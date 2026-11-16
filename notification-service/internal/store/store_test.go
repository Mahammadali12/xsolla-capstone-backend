package store

import (
	"context"
	"os"
	"testing"
)

// testStore skips unless TEST_DATABASE_URL points at a scratch database.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.db.ExecContext(ctx, `TRUNCATE deliveries, notifications RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return s
}

func TestClaimDeliveryIsIdempotent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	first, claimed, err := s.ClaimDelivery(ctx, "lms-api", 18422, "mentor_slot.booked", 7, "email")
	if err != nil || !claimed {
		t.Fatalf("first claim: id=%d claimed=%v err=%v", first, claimed, err)
	}
	second, claimed, err := s.ClaimDelivery(ctx, "lms-api", 18422, "mentor_slot.booked", 7, "email")
	if err != nil || !claimed {
		t.Fatalf("second claim: id=%d claimed=%v err=%v", second, claimed, err)
	}
	if second != first {
		t.Fatalf("ids differ: %d and %d", first, second)
	}

	var rows, attempts int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*), max(attempts) FROM deliveries`).Scan(&rows, &attempts); err != nil {
		t.Fatalf("query: %v", err)
	}
	if rows != 1 || attempts != 2 {
		t.Fatalf("rows=%d attempts=%d, want 1 and 2", rows, attempts)
	}
}

func TestClaimDeliverySkipsSent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, claimed, err := s.ClaimDelivery(ctx, "lms-api", 18422, "mentor_slot.booked", 7, "email")
	if err != nil || !claimed {
		t.Fatalf("claim: id=%d claimed=%v err=%v", id, claimed, err)
	}
	if err := s.MarkDeliverySent(ctx, id); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	if _, claimed, err := s.ClaimDelivery(ctx, "lms-api", 18422, "mentor_slot.booked", 7, "email"); err != nil || claimed {
		t.Fatalf("claimed=%v err=%v after sent, want false and nil", claimed, err)
	}
}

func TestMarkDeliverySent(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, _, err := s.ClaimDelivery(ctx, "lms-api", 18422, "mentor_slot.booked", 7, "email")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.MarkDeliverySent(ctx, id); err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	var status string
	var sentAt *string
	if err := s.db.QueryRowContext(ctx, `SELECT status, sent_at::text FROM deliveries WHERE id = $1`, id).Scan(&status, &sentAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if status != "sent" || sentAt == nil {
		t.Fatalf("status=%s sent_at=%v, want sent and a timestamp", status, sentAt)
	}
}

func TestMarkDeliveryFailed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id, _, err := s.ClaimDelivery(ctx, "lms-api", 18422, "mentor_slot.booked", 7, "email")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.MarkDeliveryFailed(ctx, id, "provider unavailable"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	var status, lastError string
	var sentAt *string
	if err := s.db.QueryRowContext(ctx, `SELECT status, last_error, sent_at::text FROM deliveries WHERE id = $1`, id).Scan(&status, &lastError, &sentAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if status != "failed" || lastError != "provider unavailable" || sentAt != nil {
		t.Fatalf("status=%s last_error=%q sent_at=%v", status, lastError, sentAt)
	}
}
