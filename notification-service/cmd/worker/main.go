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
	"xsolla-notification-service/internal/config"
	"xsolla-notification-service/internal/consumer"
	"xsolla-notification-service/internal/notification"
)

func main() {
	cfg := config.Load()
	registry := notification.NewRegistry(nil)
	if cfg.PoliciesPath != "" {
		var err error
		registry, err = notification.LoadPolicies(cfg.PoliciesPath)
		if err != nil {
			log.Fatalf("notification worker: policies: %v", err)
		}
	}
	log.Printf("notification worker: %d policies loaded", registry.Len())
	dispatch := notification.NewDispatcher(registry, func(_ context.Context, delivery notification.Delivery) error {
		log.Printf("delivery: event id=%d type=%s channels=%v recipients=%d template=%s",
			delivery.Event.ID, delivery.Event.Type, delivery.Channels, len(delivery.Recipients), delivery.Template)
		return nil
	})
	cons := consumer.New(dispatch.Handle)
	mgr := broker.NewManager(cfg.RabbitMQURL, cons.Session)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !mgr.Connected() {
			http.Error(w, "broker not connected", http.StatusServiceUnavailable)
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
