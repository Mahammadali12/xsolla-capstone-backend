package template

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"xsolla-notification-service/internal/events"
)

// TestCommittedPoliciesRender guards every template referenced by the committed
// policies file against missing payload keys.
func TestCommittedPoliciesRender(t *testing.T) {
	set, err := LoadDir("../../config/templates")
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}

	body, err := os.ReadFile("../../config/policies.json")
	if err != nil {
		t.Fatalf("read policies: %v", err)
	}
	var policies map[string]struct {
		Template string `json:"template"`
	}
	if err := json.Unmarshal(body, &policies); err != nil {
		t.Fatalf("decode policies: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"module_id":        1,
		"title":            "Backend",
		"description":      "Go and Postgres",
		"opens_at":         "2026-11-20T09:00:00Z",
		"closes_at":        "2026-12-20T09:00:00Z",
		"created_by":       7,
		"content_item_id":  5,
		"module_title":     "Backend",
		"type":             "assignment",
		"event_id":         9,
		"kind":             "meeting",
		"starts_at":        "2026-11-20T10:00:00Z",
		"ends_at":          "2026-11-20T11:00:00Z",
		"all_day":          false,
		"mentor_id":        7,
		"mentor_name":      "Mentor One",
		"slots":            []map[string]any{{"id": 1, "starts_at": "2026-11-20T10:00:00Z", "ends_at": "2026-11-20T10:30:00Z"}},
		"submission_id":    3,
		"assignment_title": "Week 1",
		"student_id":       42,
		"student_name":     "Student One",
		"submitted_at":     "2026-11-20T12:00:00Z",
		"grade":            "green",
		"graded_by":        7,
		"graded_by_name":   "Mentor One",
	})
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}

	recipient := events.Recipient{ID: 42, Role: "student", Name: "Student One", Email: "student@example.com"}
	for eventType, policy := range policies {
		if policy.Template == "" {
			continue
		}
		t.Run(eventType, func(t *testing.T) {
			event := events.Envelope{
				ID:         1,
				Type:       eventType,
				Version:    1,
				OccurredAt: time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC),
				Producer:   "lms-api",
				Data:       payload,
			}
			message, err := set.Render(policy.Template, event, recipient)
			if err != nil {
				t.Fatalf("render %q: %v", policy.Template, err)
			}
			if message.Subject == "" || message.Body == "" {
				t.Fatalf("template %q rendered empty message", policy.Template)
			}
		})
	}
}
