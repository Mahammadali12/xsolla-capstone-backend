package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"xsolla-notification-service/internal/broker"
	"xsolla-notification-service/internal/channel"
	"xsolla-notification-service/internal/config"
	"xsolla-notification-service/internal/consumer"
	"xsolla-notification-service/internal/notification"
	"xsolla-notification-service/internal/store"
	"xsolla-notification-service/internal/template"
)

func main() {
	cfg := config.Load()

	openCtx, cancelOpen := context.WithTimeout(context.Background(), 10*time.Second)
	db, err := store.Open(openCtx, cfg.DatabaseURL)
	cancelOpen()
	if err != nil {
		log.Fatalf("notification worker: database: %v", err)
	}
	defer db.Close()
	log.Print("notification worker: database ready")

	registry := notification.NewRegistry(nil)
	if cfg.PoliciesPath != "" {
		var err error
		registry, err = notification.LoadPolicies(cfg.PoliciesPath)
		if err != nil {
			log.Fatalf("notification worker: policies: %v", err)
		}
	}
	log.Printf("notification worker: %d policies loaded", registry.Len())
	templates := template.NewSet()
	if cfg.TemplatesPath != "" {
		var err error
		templates, err = template.LoadDir(cfg.TemplatesPath)
		if err != nil {
			log.Fatalf("notification worker: templates: %v", err)
		}
	}
	log.Printf("notification worker: %d templates loaded", templates.Len())
	var email channel.Channel
	if cfg.SMTPHost == "" {
		log.Print("notification worker: SMTP_HOST not set, email channel logs instead of sending")
		email = channel.NewLogSender("email")
	} else {
		sender, err := channel.NewSMTPSender(channel.SMTPConfig{
			Host:     cfg.SMTPHost,
			Port:     cfg.SMTPPort,
			Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword,
			From:     cfg.SMTPFrom,
			TLS:      cfg.SMTPTLS,
		})
		if err != nil {
			log.Fatalf("notification worker: email sender: %v", err)
		}
		log.Printf("notification worker: email channel sends via %s:%s", cfg.SMTPHost, cfg.SMTPPort)
		email = sender
	}
	channels := map[string]channel.Channel{"email": email}
	dispatch := notification.NewDispatcher(registry, templates, db, channels)
	cons := consumer.New(dispatch.Handle)
	mgr := broker.NewManager(cfg.RabbitMQURL, cons.Session)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if !mgr.Connected() {
			http.Error(w, "broker not connected", http.StatusServiceUnavailable)
			return
		}
		if err := db.Ping(r.Context()); err != nil {
			log.Printf("readyz: database: %v", err)
			http.Error(w, "database not connected", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{
		Addr:              cfg.HealthAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		mgr.Run(ctx)
	}()

	go func() {
		log.Printf("notification worker listening on %s", cfg.HealthAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("notification worker: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("notification worker: shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("notification worker: shutdown: %v", err)
	}
	wg.Wait()
}
