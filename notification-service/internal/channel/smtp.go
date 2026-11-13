package channel

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"xsolla-notification-service/internal/events"
)

// SMTPConfig describes any SMTP server. No provider-specific fields.
type SMTPConfig struct {
	Host     string
	Port     string
	Username string // empty means no authentication
	Password string
	From     string        // e.g. "Xsolla School <no-reply@example.com>"
	TLSMode  string        // "starttls" or "tls"
	Timeout  time.Duration // covers dial, TLS, auth, and the SMTP conversation

	// rootCAs overrides system roots for tests with a self-signed certificate.
	// Production never sets it, so verification stays on.
	rootCAs *x509.CertPool
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
	switch cfg.TLSMode {
	case "starttls", "tls":
	default:
		return nil, fmt.Errorf("SMTP_TLS_MODE must be starttls or tls, got %q", cfg.TLSMode)
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("SMTP timeout must be positive")
	}
	return &SMTPSender{cfg: cfg, from: from}, nil
}

// Send delivers one plain-text message. Errors reach deliveries.last_error.
func (s *SMTPSender) Send(ctx context.Context, recipient events.Recipient, message Message) error {
	if strings.ContainsAny(recipient.Email, "\r\n") {
		return fmt.Errorf("recipient %d email contains a newline", recipient.ID)
	}
	to, err := mail.ParseAddress(recipient.Email)
	if err != nil {
		return fmt.Errorf("recipient %d has no usable email: %w", recipient.ID, err)
	}
	body, err := buildMessage(s.from, to, message)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.cfg.Host, s.cfg.Port))
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return fmt.Errorf("smtp deadline: %w", err)
		}
	}

	if s.cfg.TLSMode == "tls" {
		tlsConn := tls.Client(conn, s.tlsConfig())
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("smtp tls handshake: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if s.cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp server does not offer STARTTLS")
		}
		if err := client.StartTLS(s.tlsConfig()); err != nil {
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

// tlsConfig never disables verification; tests supply their own roots.
func (s *SMTPSender) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName: s.cfg.Host,
		MinVersion: tls.VersionTLS12,
		RootCAs:    s.cfg.rootCAs,
	}
}

// buildMessage writes headers with encoded values, so template data cannot
// inject SMTP headers, then a quoted-printable plain-text body. Header values
// containing a newline are rejected instead of silently encoded.
func buildMessage(from, to *mail.Address, message Message) ([]byte, error) {
	for name, value := range map[string]string{
		"From name":    from.Name,
		"From address": from.Address,
		"To name":      to.Name,
		"To address":   to.Address,
		"Subject":      message.Subject,
	} {
		if strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%s contains a newline", name)
		}
	}

	var encoded bytes.Buffer
	writer := quotedprintable.NewWriter(&encoded)
	if _, err := writer.Write([]byte(normalizeCRLF(message.Body))); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}

	var b strings.Builder
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + to.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", message.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")
	b.Write(encoded.Bytes())
	return []byte(b.String()), nil
}

func normalizeCRLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\n", "\r\n")
}
