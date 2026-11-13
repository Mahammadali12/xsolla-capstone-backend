package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"xsolla-capstone-api/internal/db"
)

// testDatabase skips unless TEST_DATABASE_URL points at a scratch LMS database.
func testDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	database, err := db.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(database, filepath.Join("..", "db", "migrations")); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func insertTestUser(t *testing.T, database *sql.DB, email, name string) int64 {
	t.Helper()
	var id int64
	if err := database.QueryRow(
		`INSERT INTO users (email, name, password_hash) VALUES ($1, $2, 'test') RETURNING id`,
		email, name,
	).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		if _, err := database.Exec(`DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Logf("cleanup user %d: %v", id, err)
		}
	})
	return id
}

func insertTestSlot(t *testing.T, database *sql.DB, mentorID int64) int64 {
	t.Helper()
	startsAt := time.Now().Add(72 * time.Hour).Truncate(time.Minute)
	var id int64
	if err := database.QueryRow(
		`INSERT INTO mentor_slots (mentor_id, starts_at, ends_at) VALUES ($1, $2, $3) RETURNING id`,
		mentorID, startsAt, startsAt.Add(30*time.Minute),
	).Scan(&id); err != nil {
		t.Fatalf("insert slot: %v", err)
	}
	t.Cleanup(func() {
		if _, err := database.Exec(`DELETE FROM mentor_slots WHERE id = $1`, id); err != nil {
			t.Logf("cleanup slot %d: %v", id, err)
		}
	})
	return id
}

func TestBookMentorSlotWritesOutboxEvent(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	suffix := time.Now().UnixNano()
	mentorID := insertTestUser(t, database, fmt.Sprintf("outbox-mentor-%d@example.com", suffix), "Mentor One")
	studentID := insertTestUser(t, database, fmt.Sprintf("outbox-student-%d@example.com", suffix), "Student One")
	slotID := insertTestSlot(t, database, mentorID)

	slot, err := st.BookMentorSlot(context.Background(), studentID, slotID)
	if err != nil {
		t.Fatalf("book: %v", err)
	}
	if slot.BookedBy == nil || *slot.BookedBy != studentID {
		t.Fatalf("slot not booked: %+v", slot)
	}

	slotArg := strconv.FormatInt(slotID, 10)
	t.Cleanup(func() {
		if _, err := database.Exec(`DELETE FROM outbox_events WHERE payload->>'slot_id' = $1`, slotArg); err != nil {
			t.Logf("cleanup outbox: %v", err)
		}
	})

	var eventType string
	var payload []byte
	if err := database.QueryRow(
		`SELECT event_type, payload FROM outbox_events WHERE payload->>'slot_id' = $1 ORDER BY id DESC LIMIT 1`,
		slotArg,
	).Scan(&eventType, &payload); err != nil {
		t.Fatalf("outbox row: %v", err)
	}
	if eventType != MentorSlotBookedEventType {
		t.Fatalf("event type = %q, want %q", eventType, MentorSlotBookedEventType)
	}

	var data mentorSlotBookedPayload
	if err := json.Unmarshal(payload, &data); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if data.SlotID != slotID || len(data.Recipients) != 2 {
		t.Fatalf("unexpected payload: %+v", data)
	}
	if data.Recipients[0].ID != mentorID || data.Recipients[0].Role != "mentor" {
		t.Fatalf("unexpected mentor recipient: %+v", data.Recipients[0])
	}
	if data.Recipients[1].ID != studentID || data.Recipients[1].Role != "student" {
		t.Fatalf("unexpected student recipient: %+v", data.Recipients[1])
	}
}

func TestBookMentorSlotRollsBackWhenOutboxFails(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	suffix := time.Now().UnixNano()
	mentorID := insertTestUser(t, database, fmt.Sprintf("rollback-mentor-%d@example.com", suffix), "Mentor One")
	studentID := insertTestUser(t, database, fmt.Sprintf("rollback-student-%d@example.com", suffix), "Student One")
	slotID := insertTestSlot(t, database, mentorID)

	if _, err := database.Exec(`ALTER TABLE outbox_events RENAME TO outbox_events_test_backup`); err != nil {
		t.Fatalf("move outbox table: %v", err)
	}
	defer func() {
		if _, err := database.Exec(`ALTER TABLE outbox_events_test_backup RENAME TO outbox_events`); err != nil {
			t.Errorf("restore outbox table: %v", err)
		}
	}()

	if _, err := st.BookMentorSlot(context.Background(), studentID, slotID); err == nil {
		t.Fatal("want an error when the outbox insert fails")
	}

	var bookedBy *int64
	if err := database.QueryRow(`SELECT booked_by FROM mentor_slots WHERE id = $1`, slotID).Scan(&bookedBy); err != nil {
		t.Fatalf("read slot: %v", err)
	}
	if bookedBy != nil {
		t.Fatalf("booking was not rolled back: booked_by=%d", *bookedBy)
	}
}
