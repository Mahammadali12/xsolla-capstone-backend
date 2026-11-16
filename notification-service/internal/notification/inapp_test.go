package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"xsolla-notification-service/internal/channel"
	"xsolla-notification-service/internal/events"
	"xsolla-notification-service/internal/store"
	"xsolla-notification-service/internal/template"
)

type memInAppStore struct {
	saved []channel.InAppNotification
}

func (m *memInAppStore) SaveNotification(_ context.Context, n channel.InAppNotification) error {
	m.saved = append(m.saved, n)
	return nil
}

// in_app comes before email in the policies, so an email outage does not hold the in-app notification back.
func TestInAppIsKeptWhenEmailFails(t *testing.T) {
	event := validEvent(t)
	mem := &memInAppStore{}
	email := &fakeChannel{name: "email", err: errors.New("smtp down")}
	d := NewDispatcher(NewRegistry(map[string]Policy{
		"mentor_slot.booked": {Enabled: true, Recipients: []string{"mentor"}, Channels: []string{"in_app", "email"}, Template: "booking_confirmation"},
	}), &fakeRenderer{}, &fakeDeliveries{}, map[string]channel.Channel{"in_app": channel.NewInApp(mem), "email": email})

	if err := d.Handle(context.Background(), event); err == nil {
		t.Fatal("an email failure must still fail the event, so it is retried")
	}
	if len(mem.saved) != 1 || mem.saved[0].RecipientID != 7 || mem.saved[0].Title != "booking_confirmation" {
		t.Fatalf("in-app saved %+v, want the mentor's notification", mem.saved)
	}
}

// TestInAppPipeline runs the committed policies and templates through the real in_app channel and store.
func TestInAppPipeline(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`TRUNCATE deliveries, notifications RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}

	registry, err := LoadPolicies("../../config/policies.json")
	if err != nil {
		t.Fatal(err)
	}
	templates, err := template.LoadDir("../../config/templates")
	if err != nil {
		t.Fatal(err)
	}
	d := NewDispatcher(registry, templates, st, map[string]channel.Channel{
		"in_app": channel.NewInApp(st),
		"email":  channel.NewLogSender("email"),
	})

	occurred := time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)
	data, _ := json.Marshal(map[string]any{
		"module_id": 4, "module_title": "Week 3: Concurrency", "student_id": 42, "student_name": "Student One",
		"grade": "green", "graded_by": 7, "graded_by_name": "Mentor One",
		"recipients": []events.Recipient{{ID: 42, Role: "student", Name: "Student One", Email: "student@example.com"}},
	})
	event := events.Envelope{ID: 501, Type: "enrollment.graded", Version: 1, OccurredAt: occurred, Producer: "lms-api", Data: data}

	if err := d.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	var (
		count                            int
		producer, eventType, title, body string
		recipient                        int64
		createdAt                        time.Time
		readAt                           *time.Time
	)
	row := `SELECT count(*) OVER (), producer, event_type, recipient_id, title, body, created_at, read_at FROM notifications`
	if err := db.QueryRow(row).Scan(&count, &producer, &eventType, &recipient, &title, &body, &createdAt, &readAt); err != nil {
		t.Fatal(err)
	}
	if count != 1 || producer != "lms-api" || eventType != "enrollment.graded" || recipient != 42 || readAt != nil ||
		!createdAt.Equal(occurred) || title != "Grade posted for Week 3: Concurrency" ||
		!strings.Contains(body, "Mentor One posted your grade for Week 3: Concurrency: green.") {
		t.Fatalf("stored %d row(s): %s %s to %d %q %q at %v read %v", count, producer, eventType, recipient, title, body, createdAt, readAt)
	}

	// the same event again (a redelivery) keeps one notification, and a read one stays read
	if _, err := db.Exec(`UPDATE notifications SET read_at = now()`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE deliveries SET status = 'failed'`); err != nil { // force the channels to run again
		t.Fatal(err)
	}
	if err := d.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	var rows, unread int
	if err := db.QueryRow(`SELECT count(*), count(*) FILTER (WHERE read_at IS NULL) FROM notifications`).Scan(&rows, &unread); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || unread != 0 {
		t.Fatalf("after a redelivery: %d rows, %d unread; want 1 and 0", rows, unread)
	}
}
