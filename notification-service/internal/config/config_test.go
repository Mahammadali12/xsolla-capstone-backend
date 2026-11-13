package config

import (
	"strings"
	"testing"
	"time"
)

func setSMTPEnv(t *testing.T, values map[string]string) {
	t.Helper()
	for _, key := range []string{
		"SMTP_HOST", "SMTP_PORT", "SMTP_USERNAME", "SMTP_PASSWORD",
		"SMTP_FROM", "SMTP_TLS_MODE", "SMTP_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
}

func TestLoadSMTPDisabled(t *testing.T) {
	setSMTPEnv(t, nil)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.SMTPHost != "" {
		t.Fatalf("SMTPHost = %q, want empty", cfg.SMTPHost)
	}
}

func TestLoadSMTPValid(t *testing.T) {
	setSMTPEnv(t, map[string]string{
		"SMTP_HOST":     "smtp.example.com",
		"SMTP_PORT":     "587",
		"SMTP_USERNAME": "user",
		"SMTP_PASSWORD": "secret",
		"SMTP_FROM":     "School <no-reply@example.com>",
		"SMTP_TLS_MODE": "starttls",
		"SMTP_TIMEOUT":  "10s",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.SMTPHost != "smtp.example.com" || cfg.SMTPPort != "587" {
		t.Fatalf("unexpected host/port: %+v", cfg)
	}
	if cfg.SMTPUsername != "user" || cfg.SMTPPassword != "secret" {
		t.Fatalf("unexpected credentials: %+v", cfg)
	}
	if cfg.SMTPFrom != "School <no-reply@example.com>" || cfg.SMTPTLSMode != "starttls" {
		t.Fatalf("unexpected from/tls: %+v", cfg)
	}
	if cfg.SMTPTimeout != 10*time.Second {
		t.Fatalf("SMTPTimeout = %s, want 10s", cfg.SMTPTimeout)
	}
}

func TestLoadSMTPDefaults(t *testing.T) {
	setSMTPEnv(t, map[string]string{
		"SMTP_HOST": "smtp.example.com",
		"SMTP_PORT": "465",
		"SMTP_FROM": "no-reply@example.com",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.SMTPTLSMode != "starttls" {
		t.Fatalf("SMTPTLSMode = %q, want starttls", cfg.SMTPTLSMode)
	}
	if cfg.SMTPTimeout != 30*time.Second {
		t.Fatalf("SMTPTimeout = %s, want 30s", cfg.SMTPTimeout)
	}
}

func TestLoadSMTPRejectsConfigurations(t *testing.T) {
	valid := map[string]string{
		"SMTP_HOST": "smtp.example.com",
		"SMTP_PORT": "587",
		"SMTP_FROM": "no-reply@example.com",
	}
	cases := map[string]struct {
		values map[string]string
		want   string
	}{
		"missing port":          {values: map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_FROM": "no-reply@example.com"}, want: "SMTP_PORT is required"},
		"non-numeric port":      {values: withValue(valid, "SMTP_PORT", "smtp"), want: "SMTP_PORT must be a number"},
		"port too low":          {values: withValue(valid, "SMTP_PORT", "0"), want: "SMTP_PORT must be a number"},
		"port too high":         {values: withValue(valid, "SMTP_PORT", "70000"), want: "SMTP_PORT must be a number"},
		"missing from":          {values: map[string]string{"SMTP_HOST": "smtp.example.com", "SMTP_PORT": "587"}, want: "SMTP_FROM is required"},
		"bad from":              {values: withValue(valid, "SMTP_FROM", "not an address"), want: "SMTP_FROM must be an email address"},
		"bad tls mode":          {values: withValue(valid, "SMTP_TLS_MODE", "none"), want: "SMTP_TLS_MODE must be starttls or tls"},
		"username without pw":   {values: withValue(valid, "SMTP_USERNAME", "user"), want: "SMTP_USERNAME and SMTP_PASSWORD must be set together"},
		"password without user": {values: withValue(valid, "SMTP_PASSWORD", "secret"), want: "SMTP_USERNAME and SMTP_PASSWORD must be set together"},
		"bad timeout":           {values: withValue(valid, "SMTP_TIMEOUT", "soon"), want: "SMTP_TIMEOUT must be a duration"},
		"zero timeout":          {values: withValue(valid, "SMTP_TIMEOUT", "0s"), want: "SMTP_TIMEOUT must be positive"},
		"port without host":     {values: map[string]string{"SMTP_PORT": "587"}, want: "SMTP_HOST is required"},
		"from without host":     {values: map[string]string{"SMTP_FROM": "no-reply@example.com"}, want: "SMTP_HOST is required"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			setSMTPEnv(t, tc.values)
			_, err := Load()
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func withValue(base map[string]string, key, value string) map[string]string {
	values := make(map[string]string, len(base)+1)
	for k, v := range base {
		values[k] = v
	}
	values[key] = value
	return values
}
