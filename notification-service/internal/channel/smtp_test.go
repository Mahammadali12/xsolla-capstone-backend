package channel

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/mail"
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
	rejectRcpt bool
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
	return newFakeSMTPWithCert(t, mode, rejectRcpt, authMode, cert, roots)
}

func newFakeSMTPWithCert(t *testing.T, mode string, rejectRcpt bool, authMode string, cert tls.Certificate, roots *x509.CertPool) (*fakeSMTP, *x509.CertPool) {
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
	fake, _ := newFakeSMTPWithCert(t, "tls", false, "", cert, roots)
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
	})
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
	})
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	message := string(msgBytes)
	if !strings.Contains(message, "line one\r\nline two") {
		t.Fatalf("body line endings not normalized: %q", message)
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
