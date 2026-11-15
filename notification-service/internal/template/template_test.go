package template

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xsolla-notification-service/internal/events"
)

func writeTemplate(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func newDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func testEvent(data string) events.Envelope {
	return events.Envelope{
		ID:         18422,
		Type:       "mentor_slot.booked",
		Version:    1,
		OccurredAt: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC),
		Producer:   "lms-api",
		Data:       json.RawMessage(data),
	}
}

func TestRender(t *testing.T) {
	dir := newDir(t)
	writeTemplate(t, dir, "booking.subject.tmpl", "Booked: {{.Payload.starts_at}}")
	writeTemplate(t, dir, "booking.body.tmpl", "Hi {{.Recipient.Name}}, event {{.Event.ID}} in {{.Payload.slot_id}}")

	set, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if set.Len() != 1 {
		t.Fatalf("len = %d, want 1", set.Len())
	}

	event := testEvent(`{"slot_id":123,"starts_at":"2026-11-19T10:00:00Z"}`)
	message, err := set.Render("booking", event, events.Recipient{ID: 7, Name: "Mentor One"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if message.EventID != event.ID {
		t.Fatalf("event id = %d, want %d", message.EventID, event.ID)
	}
	if message.Subject != "Booked: 2026-11-19T10:00:00Z" {
		t.Fatalf("subject = %q", message.Subject)
	}
	if message.Body != "Hi Mentor One, event 18422 in 123" {
		t.Fatalf("body = %q", message.Body)
	}
	if message.HTMLBody != "" {
		t.Fatalf("html body = %q, want empty", message.HTMLBody)
	}
}

func TestRenderHTML(t *testing.T) {
	dir := newDir(t)
	writeTemplate(t, dir, layoutFile, `<html><body>{{if .Logo}}<img src="cid:xsolla-lms-logo">{{else}}<span>Xsolla LMS</span>{{end}}{{.Content}}</body></html>`)
	writeTemplate(t, dir, "booking.subject.tmpl", "Booked: {{.Payload.starts_at}}")
	writeTemplate(t, dir, "booking.body.tmpl", "Hi {{.Recipient.Name}}, plain")
	writeTemplate(t, dir, "booking.html.tmpl", "<h1>{{.Payload.title}}</h1><p>Hi {{.Recipient.Name}}</p>")

	set, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	event := testEvent(`{"starts_at":"2026-11-19T10:00:00Z","title":"Mentor slot"}`)
	message, err := set.Render("booking", event, events.Recipient{ID: 7, Name: "Mentor One"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if message.Body != "Hi Mentor One, plain" {
		t.Fatalf("body = %q", message.Body)
	}
	if !strings.Contains(message.HTMLBody, "<h1>Mentor slot</h1>") {
		t.Fatalf("html body missing fragment: %q", message.HTMLBody)
	}
	if !strings.Contains(message.HTMLBody, "<span>Xsolla LMS</span>") {
		t.Fatalf("html body does not use the wordmark fallback: %q", message.HTMLBody)
	}

	set.UseLogo()
	message, err = set.Render("booking", event, events.Recipient{ID: 7, Name: "Mentor One"})
	if err != nil {
		t.Fatalf("render with logo: %v", err)
	}
	if !strings.Contains(message.HTMLBody, `src="cid:xsolla-lms-logo"`) {
		t.Fatalf("html body does not reference the logo: %q", message.HTMLBody)
	}
}

func TestRenderHTMLEscapesPayload(t *testing.T) {
	dir := newDir(t)
	writeTemplate(t, dir, layoutFile, `<html><body>{{.Content}}</body></html>`)
	writeTemplate(t, dir, "booking.subject.tmpl", "s")
	writeTemplate(t, dir, "booking.body.tmpl", "b")
	writeTemplate(t, dir, "booking.html.tmpl", "<p>{{.Payload.title}}</p>")

	set, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	message, err := set.Render("booking", testEvent(`{"title":"<script>alert(1)</script>"}`), events.Recipient{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(message.HTMLBody, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("payload was not escaped: %q", message.HTMLBody)
	}
}

func TestRenderHTMLMissingData(t *testing.T) {
	dir := newDir(t)
	writeTemplate(t, dir, layoutFile, `<html><body>{{.Content}}</body></html>`)
	writeTemplate(t, dir, "booking.subject.tmpl", "s")
	writeTemplate(t, dir, "booking.body.tmpl", "b")
	writeTemplate(t, dir, "booking.html.tmpl", "<p>{{.Payload.starts_at}}</p>")

	set, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := set.Render("booking", testEvent(`{"slot_id":123}`), events.Recipient{}); err == nil {
		t.Fatal("want an error for the missing starts_at")
	} else if !strings.Contains(err.Error(), "starts_at") {
		t.Fatalf("error does not name the missing key: %v", err)
	}
}

func TestRenderMissingData(t *testing.T) {
	dir := newDir(t)
	writeTemplate(t, dir, "booking.subject.tmpl", "Booked: {{.Payload.starts_at}}")
	writeTemplate(t, dir, "booking.body.tmpl", "body")

	set, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := set.Render("booking", testEvent(`{"slot_id":123}`), events.Recipient{Name: "Mentor One"}); err == nil {
		t.Fatal("want an error for the missing starts_at")
	} else if !strings.Contains(err.Error(), "starts_at") {
		t.Fatalf("error does not name the missing key: %v", err)
	}
}

func TestRenderUnknownTemplate(t *testing.T) {
	if _, err := NewSet().Render("missing", testEvent(`{}`), events.Recipient{}); err == nil {
		t.Fatal("want an error")
	}
}

func TestRenderRejectsNonObjectData(t *testing.T) {
	dir := newDir(t)
	writeTemplate(t, dir, "booking.subject.tmpl", "s")
	writeTemplate(t, dir, "booking.body.tmpl", "b")

	set, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := set.Render("booking", testEvent(`[]`), events.Recipient{}); err == nil {
		t.Fatal("want an error")
	}
}

func TestLoadDirRejectsBadLayout(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
	}{
		{"missing body", map[string]string{"booking.subject.tmpl": "s"}},
		{"missing subject", map[string]string{"booking.body.tmpl": "b"}},
		{"malformed template", map[string]string{
			"booking.subject.tmpl": "{{.Payload",
			"booking.body.tmpl":    "b",
		}},
		{"empty name", map[string]string{
			".subject.tmpl": "s",
			".body.tmpl":    "b",
		}},
		{"html without layout", map[string]string{
			"booking.subject.tmpl": "s",
			"booking.body.tmpl":    "b",
			"booking.html.tmpl":    "<p>x</p>",
		}},
		{"malformed html", map[string]string{
			layoutFile:             "<html>{{.Content}}</html>",
			"booking.subject.tmpl": "s",
			"booking.body.tmpl":    "b",
			"booking.html.tmpl":    "{{.Payload",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newDir(t)
			for name, content := range tc.files {
				writeTemplate(t, dir, name, content)
			}
			if _, err := LoadDir(dir); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestLoadDirMissingDirectory(t *testing.T) {
	if _, err := LoadDir(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("want an error")
	}
}
