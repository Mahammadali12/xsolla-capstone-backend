package channel

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"xsolla-notification-service/internal/events"
)

// fakeSMTP is a minimal in-process SMTP server for one message.
type fakeSMTP struct {
	ln         net.Listener
	rejectRcpt bool // RCPT TO returns 550 (permanent)
	tempFail   bool // RCPT TO returns 450 (temporary)
	startTLS   bool
	authMode   string // "", "ok", or "fail"
	cert       tls.Certificate

	mu         sync.Mutex
	messages   []string
	authTokens []string
}

func newFakeSMTP(t *testing.T, mode string, rejectRcpt bool, authMode string) (*fakeSMTP, *x509.CertPool) {
	t.Helper()
	cert, roots := testCertificate(t)
	return newFakeSMTPWithCert(t, mode, rejectRcpt, false, authMode, cert, roots)
}

func newFakeSMTPWithCert(t *testing.T, mode string, rejectRcpt bool, tempFail bool, authMode string, cert tls.Certificate, roots *x509.CertPool) (*fakeSMTP, *x509.CertPool) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if mode == "tls" {
		ln = tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	}
	f := &fakeSMTP{
		ln:         ln,
		rejectRcpt: rejectRcpt,
		tempFail:   tempFail,
		startTLS:   mode == "starttls",
		authMode:   authMode,
		cert:       cert,
	}
	go f.accept()
	t.Cleanup(func() { _ = ln.Close() })
	return f, roots
}

// testCertificate returns a self-signed certificate for 127.0.0.1 plus roots
// that trust it, so tests never disable verification.
func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	return testCertificateFor(t, true)
}

// testCertificateFor can issue a DNS-only certificate, used to prove hostname
// verification is still enabled.
func testCertificateFor(t *testing.T, withIP bool) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "smtp.example.com"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"smtp.example.com"},
	}
	if withIP {
		template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("key pair: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("append certificate")
	}
	return cert, roots
}

func (f *fakeSMTP) accept() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	write := func(line string) {
		_, _ = w.WriteString(line)
		_ = w.Flush()
	}
	write("220 fake ESMTP\r\n")

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			response := "250-fake\r\n"
			if f.startTLS {
				response += "250-STARTTLS\r\n"
			}
			if f.authMode != "" {
				response += "250-AUTH PLAIN\r\n"
			}
			write(response + "250 8BITMIME\r\n")
		case strings.HasPrefix(upper, "STARTTLS"):
			write("220 go ahead\r\n")
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12})
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			r = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			token := strings.TrimSpace(line[len("AUTH PLAIN"):])
			f.mu.Lock()
			f.authTokens = append(f.authTokens, token)
			f.mu.Unlock()
			if f.authMode == "fail" {
				write("535 5.7.8 authentication failed\r\n")
			} else {
				write("235 2.7.0 accepted\r\n")
			}
		case strings.HasPrefix(upper, "MAIL FROM"):
			write("250 OK\r\n")
		case strings.HasPrefix(upper, "RCPT TO"):
			if f.rejectRcpt {
				write("550 no such user\r\n")
			} else if f.tempFail {
				write("450 4.2.2 mailbox temporarily unavailable\r\n")
			} else {
				write("250 OK\r\n")
			}
		case line == "DATA":
			write("354 end with dot\r\n")
			var body strings.Builder
			for {
				data, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if data == ".\r\n" || data == ".\n" {
					break
				}
				if strings.HasPrefix(data, "..") {
					data = data[1:]
				}
				body.WriteString(data)
			}
			f.mu.Lock()
			f.messages = append(f.messages, body.String())
			f.mu.Unlock()
			write("250 queued\r\n")
		case line == "QUIT":
			write("221 bye\r\n")
			return
		default:
			write("250 OK\r\n")
		}
	}
}

func (f *fakeSMTP) lastMessage() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.messages) == 0 {
		return ""
	}
	return f.messages[len(f.messages)-1]
}

func (f *fakeSMTP) lastAuthToken() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.authTokens) == 0 {
		return ""
	}
	return f.authTokens[len(f.authTokens)-1]
}

func (f *fakeSMTP) hostPort(t *testing.T) (string, string) {
	t.Helper()
	host, port, err := net.SplitHostPort(f.ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	return host, port
}

func TestSMTPSenderDeliversMessage(t *testing.T) {
	fake, roots := newFakeSMTP(t, "tls", false, "")
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:    host,
		Port:    port,
		From:    "Xsolla School <no-reply@example.com>",
		TLSMode: "tls",
		Timeout: 5 * time.Second,
		rootCAs: roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = sender.Send(ctx, events.Recipient{ID: 7, Email: "student@example.com"}, Message{
		EventID: 4,
		Subject: "Mentor slot booked: 2026-11-26T10:00:00Z",
		Body:    "Hi Student,\n\nYour slot is booked.\n",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	assertMessage(t, fake.lastMessage())
}

func TestSMTPSenderDeliversWithSTARTTLS(t *testing.T) {
	fake, roots := newFakeSMTP(t, "starttls", false, "")
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:    host,
		Port:    port,
		From:    "no-reply@example.com",
		TLSMode: "starttls",
		Timeout: 5 * time.Second,
		rootCAs: roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = sender.Send(ctx, events.Recipient{ID: 7, Email: "student@example.com"}, Message{
		EventID: 4,
		Subject: "Mentor slot booked: 2026-11-26T10:00:00Z",
		Body:    "Hi Student,\n\nYour slot is booked.\n",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	assertMessage(t, fake.lastMessage())
}

func assertMessage(t *testing.T, message string) {
	t.Helper()
	for _, want := range []string{
		"To: <student@example.com>\r\n",
		"Subject: Mentor slot booked: 2026-11-26T10:00:00Z\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"\r\n\r\nHi Student,\r\n\r\nYour slot is booked.\r\n",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q does not contain %q", message, want)
		}
	}
	if regexp.MustCompile(`[^\r]\n`).MatchString(message) {
		t.Fatalf("message has a bare LF line ending: %q", message)
	}
}

func TestSMTPSenderAuthenticatesWithConfiguredCredentials(t *testing.T) {
	fake, roots := newFakeSMTP(t, "tls", false, "ok")
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:     host,
		Port:     port,
		Username: "user",
		Password: "secret",
		From:     "no-reply@example.com",
		TLSMode:  "tls",
		Timeout:  5 * time.Second,
		rootCAs:  roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "student@example.com"}, Message{Subject: "hi", Body: "body"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(fake.lastAuthToken())
	if err != nil {
		t.Fatalf("decode auth token %q: %v", fake.lastAuthToken(), err)
	}
	if string(decoded) != "\x00user\x00secret" {
		t.Fatalf("auth token = %q, want PLAIN credentials", decoded)
	}
}

func TestSMTPSenderReturnsAuthenticationFailure(t *testing.T) {
	fake, roots := newFakeSMTP(t, "tls", false, "fail")
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:     host,
		Port:     port,
		Username: "user",
		Password: "wrong",
		From:     "no-reply@example.com",
		TLSMode:  "tls",
		Timeout:  5 * time.Second,
		rootCAs:  roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "student@example.com"}, Message{Subject: "hi", Body: "body"})
	if err == nil || !strings.Contains(err.Error(), "smtp auth") {
		t.Fatalf("got %v, want an smtp auth error", err)
	}
	if fake.lastMessage() != "" {
		t.Fatal("message was submitted despite failed authentication")
	}
}

func TestSMTPSenderReturnsServerRejection(t *testing.T) {
	fake, roots := newFakeSMTP(t, "tls", true, "")
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:    host,
		Port:    port,
		From:    "no-reply@example.com",
		TLSMode: "tls",
		Timeout: 5 * time.Second,
		rootCAs: roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = sender.Send(ctx, events.Recipient{ID: 7, Email: "missing@example.com"}, Message{Subject: "hi", Body: "body"})
	if err == nil || !strings.Contains(err.Error(), "rcpt") {
		t.Fatalf("got %v, want an rcpt error", err)
	}
}

func TestSMTPSenderRejectsInvalidRecipient(t *testing.T) {
	sender, err := NewSMTPSender(SMTPConfig{
		Host:    "127.0.0.1",
		Port:    "1", // nothing listens here; a dial attempt would fail differently
		From:    "no-reply@example.com",
		TLSMode: "tls",
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "not-an-email"}, Message{Subject: "hi", Body: "body"})
	if err == nil || !strings.Contains(err.Error(), "no usable email") {
		t.Fatalf("got %v, want an invalid recipient error", err)
	}
}

func TestSMTPSenderVerifiesCertificateHostname(t *testing.T) {
	cert, roots := testCertificateFor(t, false) // DNS SAN only, no 127.0.0.1
	fake, _ := newFakeSMTPWithCert(t, "tls", false, false, "", cert, roots)
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:    host,
		Port:    port,
		From:    "no-reply@example.com",
		TLSMode: "tls",
		Timeout: 2 * time.Second,
		rootCAs: roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "student@example.com"}, Message{Subject: "hi", Body: "body"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "certificate") {
		t.Fatalf("got %v, want a certificate verification error", err)
	}
}

func TestSMTPSenderTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn) // accept but never send a greeting
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}

	sender, err := NewSMTPSender(SMTPConfig{
		Host:    host,
		Port:    port,
		From:    "no-reply@example.com",
		TLSMode: "starttls",
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	start := time.Now()
	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "student@example.com"}, Message{Subject: "hi", Body: "body"})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Fatalf("got %v, want a timeout error", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("send took %s, timeout not applied", elapsed)
	}
}

func TestNewSMTPSenderValidation(t *testing.T) {
	cases := map[string]SMTPConfig{
		"missing host": {From: "no-reply@example.com", TLSMode: "starttls", Timeout: time.Second},
		"bad from":     {Host: "smtp.example.com", From: "not an address", TLSMode: "starttls", Timeout: time.Second},
		"bad tls":      {Host: "smtp.example.com", From: "no-reply@example.com", TLSMode: "none", Timeout: time.Second},
		"bad timeout":  {Host: "smtp.example.com", From: "no-reply@example.com", TLSMode: "starttls"},
	}
	for name, cfg := range cases {
		if _, err := NewSMTPSender(cfg); err == nil {
			t.Fatalf("%s: want an error", name)
		}
	}
}

func TestBuildMessageEncodesHeaderInjection(t *testing.T) {
	from := mustAddress(t, "no-reply@example.com")
	to := mustAddress(t, "student@example.com")

	// Newlines in header values are rejected, not encoded.
	_, err := buildMessage(from, to, Message{
		Subject: "hello\r\nBcc: attacker@example.com",
		Body:    "line one\nline two",
	}, nil)
	if err == nil {
		t.Fatalf("want an error for newline in subject, got nil")
	}
	if !strings.Contains(err.Error(), "Subject contains a newline") {
		t.Fatalf("got %v, want a Subject newline error", err)
	}

	// With clean headers, body line endings are normalized to CRLF.
	msgBytes, err := buildMessage(from, to, Message{
		Subject: "hello",
		Body:    "line one\nline two",
	}, nil)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	message := string(msgBytes)
	if !strings.Contains(message, "line one\r\nline two") {
		t.Fatalf("body line endings not normalized: %q", message)
	}
}

// TestSMTPSenderClassifiesAuthFailure tests that wrong credentials produce
// a permanent error (not retried).
func TestSMTPSenderClassifiesAuthFailure(t *testing.T) {
	fake, roots := newFakeSMTP(t, "starttls", false, "fail")
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:     host,
		Port:     port,
		Username: "user@example.com",
		Password: "wrong-password",
		From:     "no-reply@example.com",
		TLSMode:  "starttls",
		Timeout:  5 * time.Second,
		rootCAs:  roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "student@example.com"}, Message{Subject: "hi", Body: "body"})
	if err == nil {
		t.Fatal("want an auth failure error, got nil")
	}
	if IsTemporary(err) {
		t.Fatalf("auth failure should be permanent, got temporary error: %v", err)
	}
}

// TestSMTPSenderClassifiesUnreachableHost tests that an unreachable SMTP
// host produces a temporary error (retried).
func TestSMTPSenderClassifiesUnreachableHost(t *testing.T) {
	sender, err := NewSMTPSender(SMTPConfig{
		Host:    "10.255.255.1",
		Port:    "25",
		From:    "no-reply@example.com",
		TLSMode: "starttls",
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "student@example.com"}, Message{Subject: "hi", Body: "body"})
	if err == nil {
		t.Fatal("want a dial error, got nil")
	}
	if !IsTemporary(err) {
		t.Fatalf("unreachable host should be temporary, got permanent error: %v", err)
	}
}

// TestSMTPSenderClassifiesInvalidRecipient tests that an invalid recipient
// address produces a permanent error.
func TestSMTPSenderClassifiesInvalidRecipient(t *testing.T) {
	fake, roots := newFakeSMTP(t, "starttls", false, "")
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:    host,
		Port:    port,
		From:    "no-reply@example.com",
		TLSMode: "starttls",
		Timeout: 5 * time.Second,
		rootCAs: roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "not-an-email"}, Message{Subject: "hi", Body: "body"})
	if err == nil {
		t.Fatal("want an invalid recipient error, got nil")
	}
	if IsTemporary(err) {
		t.Fatalf("invalid recipient should be permanent, got temporary error: %v", err)
	}
}

// TestSMTPSenderClassifiesTemporaryFailure tests that an SMTP 4xx response
// produces a temporary error (retried).
func TestSMTPSenderClassifiesTemporaryFailure(t *testing.T) {
	cert, roots := testCertificate(t)
	fake, _ := newFakeSMTPWithCert(t, "starttls", false, false, "", cert, roots)
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:    host,
		Port:    port,
		From:    "no-reply@example.com",
		TLSMode: "starttls",
		Timeout: 5 * time.Second,
		rootCAs: roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	// Enable tempFail after creating the sender so RCPT TO returns 450.
	fake.tempFail = true

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "student@example.com"}, Message{Subject: "hi", Body: "body"})
	if err == nil {
		t.Fatal("want a temporary failure error, got nil")
	}
	if !IsTemporary(err) {
		t.Fatalf("4xx response should be temporary, got permanent error: %v", err)
	}
}

// TestSMTPSenderClassifiesPermanentFailure tests that an SMTP 5xx response
// produces a permanent error (not retried).
func TestSMTPSenderClassifiesPermanentFailure(t *testing.T) {
	fake, roots := newFakeSMTP(t, "starttls", true, "")
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host:    host,
		Port:    port,
		From:    "no-reply@example.com",
		TLSMode: "starttls",
		Timeout: 5 * time.Second,
		rootCAs: roots,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "student@example.com"}, Message{Subject: "hi", Body: "body"})
	if err == nil {
		t.Fatal("want a permanent failure error, got nil")
	}
	if IsTemporary(err) {
		t.Fatalf("5xx response should be permanent, got temporary error: %v", err)
	}
}

// TestIsTemporaryWithNonSendError reports that non-sendError errors are
// treated as temporary (conservative default for unknown failures).
func TestIsTemporaryWithNonSendError(t *testing.T) {
	if IsTemporary(errors.New("some random error")) {
		t.Fatal("unknown error should not be classified as temporary")
	}
}

func TestBuildMessageMultipartAlternative(t *testing.T) {
	from := mustAddress(t, "no-reply@example.com")
	to := mustAddress(t, "student@example.com")

	raw, err := buildMessage(from, to, Message{
		Subject:  "hi",
		Body:     "plain body\n",
		HTMLBody: "<p>html body</p>",
	}, nil)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("content type: %v", err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("content type = %q, want multipart/alternative", mediaType)
	}
	if encoding := msg.Header.Get("Content-Transfer-Encoding"); encoding != "" {
		t.Fatalf("top-level encoding = %q, want none for multipart", encoding)
	}

	reader := multipart.NewReader(msg.Body, params["boundary"])
	plain := nextPart(t, reader)
	assertPartType(t, plain, "text/plain; charset=UTF-8")
	if got := readPart(t, plain); got != "plain body\r\n" {
		t.Fatalf("plain part = %q", got)
	}
	html := nextPart(t, reader)
	assertPartType(t, html, "text/html; charset=UTF-8")
	if got := readPart(t, html); got != "<p>html body</p>" {
		t.Fatalf("html part = %q", got)
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("unexpected trailing part: %v", err)
	}
}

func TestBuildMessageMultipartRelatedWithLogo(t *testing.T) {
	from := mustAddress(t, "no-reply@example.com")
	to := mustAddress(t, "student@example.com")
	logo := append(append([]byte{}, pngSignature...), []byte("fake image")...)

	raw, err := buildMessage(from, to, Message{
		Subject:  "hi",
		Body:     "plain body",
		HTMLBody: `<p><img src="cid:xsolla-lms-logo"></p>`,
	}, logo)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("content type: %v", err)
	}
	if mediaType != "multipart/related" {
		t.Fatalf("content type = %q, want multipart/related", mediaType)
	}

	outer := multipart.NewReader(msg.Body, params["boundary"])
	alternative := nextPart(t, outer)
	altType, altParams, err := mime.ParseMediaType(alternative.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("alternative content type: %v", err)
	}
	if altType != "multipart/alternative" {
		t.Fatalf("first related part = %q, want multipart/alternative", altType)
	}
	inner := multipart.NewReader(alternative, altParams["boundary"])
	assertPartType(t, nextPart(t, inner), "text/plain; charset=UTF-8")
	assertPartType(t, nextPart(t, inner), "text/html; charset=UTF-8")

	image := nextPart(t, outer)
	if got := image.Header.Get("Content-ID"); got != "<"+logoCID+">" {
		t.Fatalf("content id = %q, want %q", got, "<"+logoCID+">")
	}
	if got := image.Header.Get("Content-Type"); !strings.HasPrefix(got, "image/png") {
		t.Fatalf("logo part type = %q, want image/png", got)
	}
	encoded, err := io.ReadAll(image)
	if err != nil {
		t.Fatalf("read logo: %v", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(encoded), "\r\n", ""))
	if err != nil {
		t.Fatalf("decode logo: %v", err)
	}
	if !bytes.Equal(decoded, logo) {
		t.Fatalf("logo bytes changed: got %d bytes, want %d", len(decoded), len(logo))
	}
	if _, err := outer.NextPart(); err != io.EOF {
		t.Fatalf("unexpected trailing part: %v", err)
	}
}

func nextPart(t *testing.T, reader *multipart.Reader) *multipart.Part {
	t.Helper()
	part, err := reader.NextPart()
	if err != nil {
		t.Fatalf("next part: %v", err)
	}
	return part
}

func assertPartType(t *testing.T, part *multipart.Part, want string) {
	t.Helper()
	if got := part.Header.Get("Content-Type"); got != want {
		t.Fatalf("part type = %q, want %q", got, want)
	}
}

// readPart reads a multipart part; the reader has already decoded
// quoted-printable and removed that header.
func readPart(t *testing.T, part *multipart.Part) string {
	t.Helper()
	body, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("read part: %v", err)
	}
	return string(body)
}

func TestSMTPSenderDeliversHTMLWithLogo(t *testing.T) {
	fake, roots := newFakeSMTP(t, "tls", false, "")
	host, port := fake.hostPort(t)
	logoPath := filepath.Join(t.TempDir(), "xsolla_lms.png")
	if err := os.WriteFile(logoPath, append(append([]byte{}, pngSignature...), []byte("fake image")...), 0o600); err != nil {
		t.Fatalf("write logo: %v", err)
	}
	sender, err := NewSMTPSender(SMTPConfig{
		Host:     host,
		Port:     port,
		From:     "Xsolla School <no-reply@example.com>",
		TLSMode:  "tls",
		Timeout:  5 * time.Second,
		rootCAs:  roots,
		LogoPath: logoPath,
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "student@example.com"}, Message{
		EventID:  4,
		Subject:  "Mentor slot booked",
		Body:     "Hi Student,\n\nYour slot is booked.\n",
		HTMLBody: `<html><body><img src="cid:xsolla-lms-logo"><p>Hi Student,</p></body></html>`,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	message := fake.lastMessage()
	for _, want := range []string{
		"Content-Type: multipart/related; boundary=",
		"Content-Type: multipart/alternative; boundary=",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Type: text/html; charset=UTF-8",
		"Content-ID: <" + logoCID + ">",
		"Content-Type: image/png",
		"cid:xsolla-lms-logo",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("message does not contain %q: %q", want, message)
		}
	}
	if regexp.MustCompile(`[^\r]\n`).MatchString(message) {
		t.Fatalf("message has a bare LF line ending")
	}
}

func TestNewSMTPSenderValidatesLogo(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(good, append(append([]byte{}, pngSignature...), []byte("fake image")...), 0o600); err != nil {
		t.Fatalf("write logo: %v", err)
	}
	notPNG := filepath.Join(dir, "logo.txt")
	if err := os.WriteFile(notPNG, []byte("not a png"), 0o600); err != nil {
		t.Fatalf("write bad logo: %v", err)
	}

	base := SMTPConfig{
		Host:    "smtp.example.com",
		From:    "no-reply@example.com",
		TLSMode: "starttls",
		Timeout: time.Second,
	}
	withGood := base
	withGood.LogoPath = good
	sender, err := NewSMTPSender(withGood)
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}
	if !sender.HasLogo() {
		t.Fatal("HasLogo = false, want true")
	}

	for name, path := range map[string]string{
		"not a png": notPNG,
		"not found": filepath.Join(dir, "missing.png"),
	} {
		cfg := base
		cfg.LogoPath = path
		if _, err := NewSMTPSender(cfg); err == nil {
			t.Fatalf("%s: want an error", name)
		}
	}
}

func mustAddress(t *testing.T, value string) *mail.Address {
	t.Helper()
	address, err := mail.ParseAddress(value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return address
}
