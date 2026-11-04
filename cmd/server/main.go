package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/db"
	"xsolla-capstone-api/internal/handlers"
	"xsolla-capstone-api/internal/middleware"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "host=127.0.0.1 port=5432 user=maqa dbname=lms sslmode=disable"
	}
	database, err := db.Open(dsn)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()
	migrationsDir := os.Getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "internal/db/migrations"
	}
	if err := db.Migrate(database, migrationsDir); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	accessSecret := []byte(os.Getenv("JWT_ACCESS_SECRET"))
	if len(accessSecret) == 0 {
		accessSecret = []byte("dev-secret-change-me")
	}
	refreshSecret := []byte(os.Getenv("JWT_REFRESH_SECRET"))
	if len(refreshSecret) == 0 {
		refreshSecret = []byte("dev-refresh-secret-change-me")
	}

	auth.AccessSecret = accessSecret
	auth.RefreshSecret = refreshSecret

	h := handlers.AuthHandlers{
		DB:           database,
		AccessSecret: accessSecret,
		RefreshSecret: refreshSecret,
		AccessTTL:    15 * time.Minute,
		RefreshTTL:   7 * 24 * time.Hour,
	}

	authenticator := &middleware.Authenticator{Secret: accessSecret}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/signup", h.Signup)
	mux.HandleFunc("POST /auth/login", h.Login)
	mux.HandleFunc("POST /auth/refresh", h.Refresh)
	mux.HandleFunc("POST /auth/logout", h.Logout)
	mux.Handle("GET /users/me", authenticator.Middleware(http.HandlerFunc(h.Me)))

	addr := os.Getenv("PORT")
	if addr == "" {
		addr = ":8080"
	} else if addr[0] != ':' {
		addr = ":" + addr
	}
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, cors(mux)); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("CORS_ORIGIN")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
