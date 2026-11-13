// Package config reads the notification worker's settings from environment variables.
package config

import "os"

// Config holds all runtime configuration for the notification worker.
type Config struct {
	HealthAddr string
}

// Load reads the worker configuration, applying defaults for unset values.
func Load() Config {
	return Config{
		HealthAddr: getenv("HEALTH_ADDR", ":8081"),
	}
}

// getenv returns the environment value for key, or def if unset or empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
