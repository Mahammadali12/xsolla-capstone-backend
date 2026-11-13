// Package config reads the notification worker's settings from environment variables.
package config

import "os"

// Config holds all runtime configuration for the notification worker.
type Config struct {
	HealthAddr    string
	RabbitMQURL   string
	DatabaseURL   string
	PoliciesPath  string // empty means no policies: every event is ignored
	TemplatesPath string // empty means no templates: any policy delivery fails
}

// Load reads the worker configuration, applying defaults for unset values.
func Load() Config {
	return Config{
		HealthAddr:    getenv("HEALTH_ADDR", ":8081"),
		RabbitMQURL:   getenv("RABBITMQ_URL", "amqp://lms:lms@localhost:5672/"),
		DatabaseURL:   getenv("DATABASE_URL", "host=localhost port=5433 user=notify password=notify dbname=notify sslmode=disable"),
		PoliciesPath:  getenv("POLICIES_PATH", ""),
		TemplatesPath: getenv("TEMPLATES_PATH", ""),
	}
}

// getenv returns the environment value for key, or def if unset or empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
