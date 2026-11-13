// Package config reads the notification worker's settings from environment variables.
package config

import (
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultSMTPTimeout = 30 * time.Second

// Config holds all runtime configuration for the notification worker.
type Config struct {
	HealthAddr    string
	RabbitMQURL   string
	DatabaseURL   string
	PoliciesPath  string // empty means no policies: every event is ignored
	TemplatesPath string // empty means no templates: any policy delivery fails

	// Email. An empty SMTPHost keeps the logging sender instead.
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPTLSMode  string // "starttls" or "tls"
	SMTPTimeout  time.Duration
}

// Load reads the worker configuration and validates it, so a bad SMTP setup
// fails at startup rather than on the first delivery.
func Load() (Config, error) {
	cfg := Config{
		HealthAddr:    getenv("HEALTH_ADDR", ":8081"),
		RabbitMQURL:   getenv("RABBITMQ_URL", "amqp://lms:lms@localhost:5672/"),
		DatabaseURL:   getenv("DATABASE_URL", "host=localhost port=5433 user=notify password=notify dbname=notify sslmode=disable"),
		PoliciesPath:  getenv("POLICIES_PATH", ""),
		TemplatesPath: getenv("TEMPLATES_PATH", ""),
	}

	smtp := smtpSettings{
		host:     strings.TrimSpace(os.Getenv("SMTP_HOST")),
		port:     strings.TrimSpace(os.Getenv("SMTP_PORT")),
		username: strings.TrimSpace(os.Getenv("SMTP_USERNAME")),
		password: os.Getenv("SMTP_PASSWORD"),
		from:     strings.TrimSpace(os.Getenv("SMTP_FROM")),
		tlsMode:  strings.TrimSpace(getenv("SMTP_TLS_MODE", "starttls")),
		timeout:  strings.TrimSpace(getenv("SMTP_TIMEOUT", "30s")),
	}
	if err := smtp.validate(); err != nil {
		return Config{}, err
	}

	cfg.SMTPHost = smtp.host
	cfg.SMTPPort = smtp.port
	cfg.SMTPUsername = smtp.username
	cfg.SMTPPassword = smtp.password
	cfg.SMTPFrom = smtp.from
	cfg.SMTPTLSMode = smtp.tlsMode
	cfg.SMTPTimeout = defaultSMTPTimeout
	if smtp.host != "" {
		timeout, err := time.ParseDuration(smtp.timeout)
		if err != nil {
			return Config{}, fmt.Errorf("SMTP_TIMEOUT must be a duration like 30s: %v", err)
		}
		cfg.SMTPTimeout = timeout
	}
	return cfg, nil
}

// smtpSettings is the raw environment input, validated as one unit.
type smtpSettings struct {
	host, port, username, password, from, tlsMode, timeout string
}

// validate enforces all-or-nothing SMTP configuration. An empty host disables
// SMTP, but any other SMTP setting without a host is a mistake, not a fallback.
func (s smtpSettings) validate() error {
	if s.host == "" {
		if s.port != "" || s.username != "" || s.password != "" || s.from != "" ||
			os.Getenv("SMTP_TLS_MODE") != "" || os.Getenv("SMTP_TIMEOUT") != "" {
			return fmt.Errorf("SMTP_HOST is required when other SMTP settings are set")
		}
		return nil
	}
	if s.port == "" {
		return fmt.Errorf("SMTP_PORT is required")
	}
	port, err := strconv.Atoi(s.port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("SMTP_PORT must be a number between 1 and 65535, got %q", s.port)
	}
	if s.from == "" {
		return fmt.Errorf("SMTP_FROM is required")
	}
	if _, err := mail.ParseAddress(s.from); err != nil {
		return fmt.Errorf("SMTP_FROM must be an email address: %v", err)
	}
	if s.tlsMode != "starttls" && s.tlsMode != "tls" {
		return fmt.Errorf("SMTP_TLS_MODE must be starttls or tls, got %q", s.tlsMode)
	}
	if (s.username == "") != (s.password == "") {
		return fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must be set together or left empty")
	}
	timeout, err := time.ParseDuration(s.timeout)
	if err != nil {
		return fmt.Errorf("SMTP_TIMEOUT must be a duration like 30s: %v", err)
	}
	if timeout <= 0 {
		return fmt.Errorf("SMTP_TIMEOUT must be positive, got %s", s.timeout)
	}
	return nil
}

// getenv returns the environment value for key, or def if unset or empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
