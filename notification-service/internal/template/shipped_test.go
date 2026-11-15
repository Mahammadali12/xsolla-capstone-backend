package template

import (
	"strings"
	"testing"

	"xsolla-notification-service/internal/events"
)

func TestShippedTemplatesRenderCheck(t *testing.T) {
	set, err := LoadDir("../../config/templates")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cases := map[string]string{
		"booking_confirmation":        `{"starts_at":"2026-11-19T10:00:00Z","ends_at":"2026-11-19T10:30:00Z"}`,
		"booking_cancellation":        `{"starts_at":"2026-11-19T10:00:00Z","ends_at":"2026-11-19T10:30:00Z"}`,
		"module_created":              `{"title":"Math","opens_at":"2026-11-19T10:00:00Z","closes_at":"2026-12-19T10:00:00Z","description":"Basics"}`,
		"content_item_uploaded":       `{"type":"video","title":"Intro","module_title":"Math","description":"Watch this"}`,
		"event_created":               `{"title":"Lecture","kind":"lecture","starts_at":"2026-11-19T10:00:00Z","ends_at":"2026-11-19T11:00:00Z","all_day":false}`,
		"mentor_availability_created": `{"mentor_name":"Mentor One","slots":[{"starts_at":"2026-11-19T10:00:00Z","ends_at":"2026-11-19T10:30:00Z"}]}`,
		"submission_created":          `{"student_name":"Student One","assignment_title":"Essay","module_title":"Math","submitted_at":"2026-11-19T10:00:00Z"}`,
		"enrollment_graded":           `{"graded_by_name":"Mentor One","module_title":"Math","grade":"A"}`,
	}
	for name, payload := range cases {
		message, err := set.Render(name, events.Envelope{ID: 1, Type: "x.y", Data: []byte(payload)}, events.Recipient{ID: 2, Name: "Test", Email: "test@example.com"})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(message.HTMLBody, "<!DOCTYPE html>") {
			t.Fatalf("%s: missing layout: %q", name, message.HTMLBody)
		}
	}
}
