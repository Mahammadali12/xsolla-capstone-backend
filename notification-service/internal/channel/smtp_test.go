package channel

import (
	"bufio"
	"context"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"xsolla-notification-service/internal/events"
)

// fakeSMTP is a minimal in-process SMTP server, enough for one message.
type fakeSMTP struct {
	ln         net.Listener
	rejectRcpt bool

	mu       sync.Mutex
	messages []string
}

func newFakeSMTP(t *testing.T, rejectRcpt bool) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeSMTP{ln: ln, rejectRcpt: rejectRcpt}
	go f.accept()
	t.Cleanup(func() { _ = ln.Close() })
	return f
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
		switch {
		case strings.HasPrefix(strings.ToUpper(line), "EHLO"), strings.HasPrefix(strings.ToUpper(line), "HELO"):
			write("250-fake\r\n250 8BITMIME\r\n")
		case strings.HasPrefix(strings.ToUpper(line), "MAIL FROM"):
			write("250 OK\r\n")
		case strings.HasPrefix(strings.ToUpper(line), "RCPT TO"):
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

func (f *fakeSMTP) hostPort(t *testing.T) (string, string) {
	t.Helper()
	host, port, err := net.SplitHostPort(f.ln.Addr().String())
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	return host, port
}

func TestSMTPSenderDeliversMessage(t *testing.T) {
	fake := newFakeSMTP(t, false)
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host: host,
		Port: port,
		From: "Xsolla School <no-reply@example.com>",
		TLS:  "none",
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

	message := fake.lastMessage()
	for _, want := range []string{
		"From: \"Xsolla School\" <no-reply@example.com>\r\n",
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

func TestSMTPSenderReturnsServerRejection(t *testing.T) {
	fake := newFakeSMTP(t, true)
	host, port := fake.hostPort(t)
	sender, err := NewSMTPSender(SMTPConfig{
		Host: host,
		Port: port,
		From: "no-reply@example.com",
		TLS:  "none",
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
		Host: "127.0.0.1",
		Port: "1", // nothing listens here; a dial attempt would fail differently
		From: "no-reply@example.com",
		TLS:  "none",
	})
	if err != nil {
		t.Fatalf("new sender: %v", err)
	}

	err = sender.Send(context.Background(), events.Recipient{ID: 7, Email: "not-an-email"}, Message{Subject: "hi", Body: "body"})
	if err == nil || !strings.Contains(err.Error(), "no usable email") {
		t.Fatalf("got %v, want an invalid recipient error", err)
	}
}

func TestNewSMTPSenderValidation(t *testing.T) {
	cases := map[string]SMTPConfig{
		"missing host": {From: "no-reply@example.com", TLS: "starttls"},
		"bad from":     {Host: "smtp.example.com", From: "not an address", TLS: "starttls"},
		"bad tls":      {Host: "smtp.example.com", From: "no-reply@example.com", TLS: "ssl"},
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

	message := string(buildMessage(from, to, Message{
		Subject: "hello\r\nBcc: attacker@example.com",
		Body:    "line one\nline two",
	}))
	if strings.Contains(message, "\r\nBcc:") {
		t.Fatalf("subject injected a header: %q", message)
	}
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
