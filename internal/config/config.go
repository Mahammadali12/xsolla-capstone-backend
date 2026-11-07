// Package config reads environment variables from the .env file and the
// process environment, then exposes them as a single Config struct.
// No external dependencies: os.ReadFile + os.Getenv do the job.
package config

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

// Config holds all runtime configuration for the backend.
type Config struct {
	DatabaseURL     string
	Port            string
	JWTAccessSecret []byte
	JWTRefreshSecret []byte
	CORSOrigin      string
	MigrationsDir   string

	// S3 object storage
	S3Endpoint     string
	S3Bucket       string
	S3Region       string
	S3AccessKey    string
	S3SecretKey    string
}

// defaults for optional values when not set in .env or environment.
var defaults = map[string]string{
	"PORT":             ":8080",
	"CORS_ORIGIN":      "*",
	"MIGRATIONS_DIR":   "internal/db/migrations",
	"JWT_ACCESS_SECRET": "dev-access-secret-change-me-in-production",
	"JWT_REFRESH_SECRET": "dev-refresh-secret-change-me-in-production",
	"S3_ENDPOINT":      "",
	"S3_BUCKET":        "",
	"S3_REGION":        "us-east-1",
	"S3_ACCESS_KEY":    "",
	"S3_SECRET_KEY":    "",
}

// Load reads .env (if present), applies environment variables, and returns a Config.
// Variables already set in the process environment take precedence over .env.
func Load() (*Config, error) {
	_ = loadEnvFile(".env")

	cfg := &Config{
		DatabaseURL:     getenv("DATABASE_URL", ""),
		Port:            getenv("PORT", defaults["PORT"]),
		JWTAccessSecret: []byte(getenv("JWT_ACCESS_SECRET", defaults["JWT_ACCESS_SECRET"])),
		JWTRefreshSecret: []byte(getenv("JWT_REFRESH_SECRET", defaults["JWT_REFRESH_SECRET"])),
		CORSOrigin:      getenv("CORS_ORIGIN", defaults["CORS_ORIGIN"]),
		MigrationsDir:   getenv("MIGRATIONS_DIR", defaults["MIGRATIONS_DIR"]),

		// S3 object storage
		S3Endpoint:     getenv("S3_ENDPOINT", defaults["S3_ENDPOINT"]),
		S3Bucket:       getenv("S3_BUCKET", defaults["S3_BUCKET"]),
		S3Region:       getenv("S3_REGION", defaults["S3_REGION"]),
		S3AccessKey:    getenv("S3_ACCESS_KEY", defaults["S3_ACCESS_KEY"]),
		S3SecretKey:    getenv("S3_SECRET_KEY", defaults["S3_SECRET_KEY"]),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

// loadEnvFile parses a KEY=VALUE .env file and sets missing variables via os.Setenv.
// Lines starting with # are comments; blank lines are skipped.
// It intentionally does not overwrite variables already set in the environment.
// Returns an error if the file exists but cannot be read; missing file is not an error.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("config: .env file not found at %s, using environment variables only", path)
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if _, present := os.LookupEnv(key); !present {
			_ = os.Setenv(key, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("config: failed to read .env: %w", err)
	}
	return nil
}

// getenv returns the environment value for key, or def if unset.
func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
