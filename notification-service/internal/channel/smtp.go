package channel

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"time"

	"xsolla-notification-service/internal/events"
)

// logoCID is the Content-ID the HTML layout references as cid:xsolla-lms-logo.
const logoCID = "xsolla-lms-logo"

// pngSignature identifies a PNG file; the layout always sends image/png.
var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// SMTPConfig describes any SMTP server. No provider-specific fields.
type SMTPConfig struct {
	Host     string
	Port     string
	Username string // empty means no authentication
	Password string
	From     string        // e.g. "Xsolla School <no-reply@example.com>"
	TLSMode  string        // "starttls" or "tls"
	Timeout  time.Duration // covers dial, TLS, auth, and the SMTP conversation
	LogoPath string        // optional PNG attached inline as cid:xsolla-lms-logo

	// rootCAs overrides system roots for tests with a self-signed certificate.
	// Production never sets it, so verification stays on.
	rootCAs *x509.CertPool
}

// sendError wraps an SMTP send failure and classifies it as temporary or
// permanent. Temporary failures (network errors, 4xx responses) may be retried;
// permanent failures (auth failures, invalid recipients, 5xx responses) should
// not be retried indefinitely.
type sendError struct {
	err       error
	temporary bool
}

func (e *sendError) Error() string { return e.err.Error() }
func (e *sendError) Unwrap() error { return e.err }

// temporarySendError wraps err as a retryable failure.
func TemporarySendError(err error) error { return &sendError{err: err, temporary: true} }

// permanentSendError wraps err as a non-retryable failure.
func PermanentSendError(err error) error { return &sendError{err: err, temporary: false} }

// IsTemporary reports whether err is a retryable send failure.
func IsTemporary(err error) bool {
	var se *sendError
	if errors.As(err, &se) {
		return se.temporary
	}
	return false
}

// smtpStatusCode extracts the 3-digit SMTP status code from an error message,
// such as "smtp: 450 4.2.2 mailbox temporarily unavailable" or
// "smtp rcpt to: 550 no such user". Returns 0 if no code is found.
func smtpStatusCode(err error) int {
	msg := err.Error()
	// Scan for a 3-digit code at the start of a word.
	for i := 0; i < len(msg)-2; i++ {
		if msg[i] < '0' || msg[i] > '9' {
			continue
		}
		if i+2 < len(msg) && msg[i+1] >= '0' && msg[i+1] <= '9' && msg[i+2] >= '0' && msg[i+2] <= '9' {
			// Check it's not part of a longer number
			if i > 0 && msg[i-1] >= '0' && msg[i-1] <= '9' {
				continue
			}
			if i+3 < len(msg) && msg[i+3] >= '0' && msg[i+3] <= '9' {
				continue
			}
			code := 0
			for j := i; j < i+3; j++ {
				code = code*10 + int(msg[j]-'0')
			}
			return code
		}
	}
	return 0
}

// temporaryIf4xx classifies an SMTP command error: 4xx codes are temporary,
// 5xx codes are permanent, unknown codes default to temporary.
func temporaryIf4xx(err error) error {
	code := smtpStatusCode(err)
	if code >= 400 && code < 500 {
		return TemporarySendError(err)
	}
	if code >= 500 {
		return PermanentSendError(err)
	}
	return TemporarySendError(err)
}

// SMTPSender delivers email through a configured SMTP server.
type SMTPSender struct {
	cfg  SMTPConfig
	from *mail.Address
	logo []byte
}

// NewSMTPSender validates the configuration; From, TLS, and the optional logo
// must be usable.
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
	var logo []byte
	if cfg.LogoPath != "" {
		logo, err = os.ReadFile(cfg.LogoPath)
		if err != nil {
			return nil, fmt.Errorf("email logo: %w", err)
		}
		if !bytes.HasPrefix(logo, pngSignature) {
			return nil, fmt.Errorf("email logo %s: not a PNG file", cfg.LogoPath)
		}
	}
	return &SMTPSender{cfg: cfg, from: from, logo: logo}, nil
}

// HasLogo reports whether HTML messages carry the inline logo.
func (s *SMTPSender) HasLogo() bool {
	return len(s.logo) > 0
}

// Send delivers one message, plain text or multipart when HTML is present.
// Errors reach deliveries.last_error.
func (s *SMTPSender) Send(ctx context.Context, recipient events.Recipient, message Message) error {
	if strings.ContainsAny(recipient.Email, "\r\n") {
		return PermanentSendError(fmt.Errorf("recipient %d email contains a newline", recipient.ID))
	}
	to, err := mail.ParseAddress(recipient.Email)
	if err != nil {
		return PermanentSendError(fmt.Errorf("recipient %d has no usable email: %w", recipient.ID, err))
	}
	body, err := buildMessage(s.from, to, message, s.logo)
	if err != nil {
		return PermanentSendError(err)
	}

	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.cfg.Host, s.cfg.Port))
	if err != nil {
		return TemporarySendError(fmt.Errorf("smtp dial: %w", err))
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
			return TemporarySendError(fmt.Errorf("smtp tls handshake: %w", err))
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return TemporarySendError(fmt.Errorf("smtp client: %w", err))
	}
	defer client.Close()

	if s.cfg.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return PermanentSendError(errors.New("smtp server does not offer STARTTLS"))
		}
		if err := client.StartTLS(s.tlsConfig()); err != nil {
			return TemporarySendError(fmt.Errorf("smtp starttls: %w", err))
		}
	}
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return PermanentSendError(fmt.Errorf("smtp auth: %w", err))
		}
	}
	if err := client.Mail(s.from.Address); err != nil {
		return temporaryIf4xx(fmt.Errorf("smtp mail from: %w", err))
	}
	if err := client.Rcpt(to.Address); err != nil {
		return temporaryIf4xx(fmt.Errorf("smtp rcpt to: %w", err))
	}
	data, err := client.Data()
	if err != nil {
		return temporaryIf4xx(fmt.Errorf("smtp data: %w", err))
	}
	if _, err := data.Write(body); err != nil {
		data.Close()
		return TemporarySendError(fmt.Errorf("smtp write: %w", err))
	}
	if err := data.Close(); err != nil {
		return TemporarySendError(fmt.Errorf("smtp finish: %w", err))
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
// inject SMTP headers, then the body selected by the message shape. Header
// values containing a newline are rejected instead of silently encoded.
func buildMessage(from, to *mail.Address, message Message, logo []byte) ([]byte, error) {
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

	contentType, encoding, body, err := buildBody(message, logo)
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	b.WriteString("From: " + from.String() + "\r\n")
	b.WriteString("To: " + to.String() + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", message.Subject) + "\r\n")
	b.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: " + contentType + "\r\n")
	if encoding != "" {
		b.WriteString("Content-Transfer-Encoding: " + encoding + "\r\n")
	}
	b.WriteString("\r\n")
	b.Write(body)
	return []byte(b.String()), nil
}

// buildBody picks the simplest encoding that carries the message: plain text,
// an HTML alternative, or an HTML alternative with the inline logo. Multipart
// entities carry no top-level Content-Transfer-Encoding.
func buildBody(message Message, logo []byte) (contentType, encoding string, body []byte, err error) {
	if message.HTMLBody == "" {
		encoded, err := encodeQuotedPrintable(message.Body)
		if err != nil {
			return "", "", nil, err
		}
		return "text/plain; charset=UTF-8", "quoted-printable", encoded, nil
	}
	if len(logo) == 0 {
		contentType, body, err = buildAlternative(message)
		return contentType, "", body, err
	}
	contentType, body, err = buildRelated(message, logo)
	return contentType, "", body, err
}

func encodeQuotedPrintable(text string) ([]byte, error) {
	var encoded bytes.Buffer
	writer := quotedprintable.NewWriter(&encoded)
	if _, err := writer.Write([]byte(normalizeCRLF(text))); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	return encoded.Bytes(), nil
}

// buildAlternative returns a multipart/alternative entity with the plain text
// first and HTML last, so clients pick the last part they can render.
func buildAlternative(message Message) (string, []byte, error) {
	var buf bytes.Buffer
	boundary, err := writeAlternative(&buf, message)
	if err != nil {
		return "", nil, err
	}
	return "multipart/alternative; boundary=\"" + boundary + "\"", buf.Bytes(), nil
}

func writeAlternative(buf *bytes.Buffer, message Message) (string, error) {
	w := multipart.NewWriter(buf)
	for _, part := range []struct {
		contentType string
		text        string
	}{
		{"text/plain; charset=UTF-8", message.Body},
		{"text/html; charset=UTF-8", message.HTMLBody},
	} {
		header := textproto.MIMEHeader{}
		header.Set("Content-Type", part.contentType)
		header.Set("Content-Transfer-Encoding", "quoted-printable")
		p, err := w.CreatePart(header)
		if err != nil {
			return "", fmt.Errorf("encode multipart: %w", err)
		}
		encoded, err := encodeQuotedPrintable(part.text)
		if err != nil {
			return "", err
		}
		if _, err := p.Write(encoded); err != nil {
			return "", fmt.Errorf("encode multipart: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("encode multipart: %w", err)
	}
	return w.Boundary(), nil
}

// buildRelated nests the alternative inside multipart/related and appends the
// logo, which the HTML references as cid:xsolla-lms-logo.
func buildRelated(message Message, logo []byte) (string, []byte, error) {
	var inner bytes.Buffer
	altBoundary, err := writeAlternative(&inner, message)
	if err != nil {
		return "", nil, err
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", "multipart/alternative; boundary=\""+altBoundary+"\"")
	part, err := w.CreatePart(header)
	if err != nil {
		return "", nil, fmt.Errorf("encode related: %w", err)
	}
	if _, err := part.Write(inner.Bytes()); err != nil {
		return "", nil, fmt.Errorf("encode related: %w", err)
	}
	if err := writeLogoPart(w, logo); err != nil {
		return "", nil, err
	}
	if err := w.Close(); err != nil {
		return "", nil, fmt.Errorf("encode related: %w", err)
	}
	return "multipart/related; boundary=\"" + w.Boundary() + "\"", buf.Bytes(), nil
}

func writeLogoPart(w *multipart.Writer, logo []byte) error {
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", `image/png; name="xsolla_lms.png"`)
	header.Set("Content-Transfer-Encoding", "base64")
	// Set directly so the header keeps its conventional casing.
	header["Content-ID"] = []string{"<" + logoCID + ">"}
	header.Set("Content-Disposition", `inline; filename="xsolla_lms.png"`)
	part, err := w.CreatePart(header)
	if err != nil {
		return fmt.Errorf("encode logo: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(logo)
	for len(encoded) > 76 {
		if _, err := io.WriteString(part, encoded[:76]+"\r\n"); err != nil {
			return fmt.Errorf("encode logo: %w", err)
		}
		encoded = encoded[76:]
	}
	if _, err := io.WriteString(part, encoded+"\r\n"); err != nil {
		return fmt.Errorf("encode logo: %w", err)
	}
	return nil
}

func normalizeCRLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\n", "\r\n")
}
