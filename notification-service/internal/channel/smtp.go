package channel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"xsolla-notification-service/internal/events"
)

const (
	dialTimeout = 10 * time.Second
	sendTimeout = 30 * time.Second
)

// SMTPConfig describes any SMTP server. No provider-specific fields.
type SMTPConfig struct {
	Host     string
	Port     string
	Username string // empty means no authentication
	Password string
	From     string // e.g. "Xsolla School <no-reply@example.com>"
	TLS      string // "starttls" or "none"
}

// SMTPSender delivers email through a configured SMTP server.
type SMTPSender struct {
	cfg  SMTPConfig
	from *mail.Address
}

// NewSMTPSender validates the configuration; From and TLS must be usable.
func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	if cfg.Host == "" {
		return nil, errors.New("SMTP host is required")
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("SMTP_FROM: %w", err)
	}
	switch cfg.TLS {
	case "starttls", "none":
	default:
		return nil, fmt.Errorf("SMTP_TLS must be starttls or none, got %q", cfg.TLS)
	}
	return &SMTPSender{cfg: cfg, from: from}, nil
}

// Send delivers one plain-text message. Errors reach deliveries.last_error.
func (s *SMTPSender) Send(ctx context.Context, recipient events.Recipient, message Message) error {
	to, err := mail.ParseAddress(recipient.Email)
	if err != nil {
		return fmt.Errorf("recipient %d has no usable email: %w", recipient.ID, err)
	}
	body := buildMessage(s.from, to, message)
	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.cfg.Host, s.cfg.Port))
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(sendTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("smtp deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if s.cfg.TLS == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp server does not offer STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(s.from.Address); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	data, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := data.Write(body); err != nil {
		data.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := data.Close(); err != nil {
		return fmt.Errorf("smtp finish: %w", err)
	}
	// The message is accepted; a dropped QUIT must not mark it failed.
	if err := client.Quit(); err != nil {
		log.Printf("channel email: smtp quit after send: %v", err)
	}
	return nil
}

// buildMessage writes headers with encoded values, so template data cannot
// inject SMTP headers, then a CRLF-normalized plain-text body.
func buildMessage(from, to *mail.Address, message Message) []byte {
	var b strings.Builder
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + to.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", message.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(normalizeCRLF(message.Body))
	return []byte(b.String())
}

func normalizeCRLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\n", "\r\n")
}
