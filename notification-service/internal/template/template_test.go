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
