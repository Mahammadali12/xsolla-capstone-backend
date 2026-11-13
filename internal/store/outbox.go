package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// MentorSlotBookedEventType is the domain event emitted when a student books a slot.
const MentorSlotBookedEventType = "mentor_slot.booked"

// bookingRecipient is the minimal contact snapshot the notification service
// needs. Do not add more personal data than delivery requires.
type bookingRecipient struct {
	ID    int64  `json:"id"`
	Role  string `json:"role"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// mentorSlotBookedPayload is the event's data object.
type mentorSlotBookedPayload struct {
	SlotID     int64              `json:"slot_id"`
	StartsAt   time.Time          `json:"starts_at"`
	EndsAt     time.Time          `json:"ends_at"`
	Recipients []bookingRecipient `json:"recipients"`
}

// OutboxEvent is one unpublished domain event row.
type OutboxEvent struct {
	ID        int64
	Type      string
	Payload   []byte
	CreatedAt time.Time
}

// mentorSlotBookedData builds the event payload for a confirmed booking.
func mentorSlotBookedData(slot MentorSlot, mentorName, mentorEmail, studentName, studentEmail string) ([]byte, error) {
	if slot.BookedBy == nil {
		return nil, errors.New("mentor slot has no student")
	}
	return json.Marshal(mentorSlotBookedPayload{
		SlotID:   slot.ID,
		StartsAt: slot.StartsAt.UTC(),
		EndsAt:   slot.EndsAt.UTC(),
		Recipients: []bookingRecipient{
			{ID: slot.MentorID, Role: "mentor", Name: mentorName, Email: mentorEmail},
			{ID: *slot.BookedBy, Role: "student", Name: studentName, Email: studentEmail},
		},
	})
}

// InsertOutboxEvent records an event in the caller's transaction.
func (q *Query) InsertOutboxEvent(ctx context.Context, eventType string, payload []byte) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO outbox_events (event_type, payload) VALUES ($1, $2)`,
		eventType, payload,
	)
	return err
}

// userNameEmail reads the fields a recipient snapshot needs.
func (q *Query) userNameEmail(ctx context.Context, id int64) (string, string, error) {
	var name, email string
	err := q.QueryRowContext(ctx, `SELECT name, email FROM users WHERE id = $1`, id).Scan(&name, &email)
	return name, email, err
}

// PendingOutboxEvents returns the oldest unpublished events.
func (s *StoreImpl) PendingOutboxEvents(ctx context.Context, limit int) ([]OutboxEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, event_type, payload, created_at
		 FROM outbox_events
		 WHERE published_at IS NULL
		 ORDER BY id
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []OutboxEvent
	for rows.Next() {
		var event OutboxEvent
		if err := rows.Scan(&event.ID, &event.Type, &event.Payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// MarkOutboxEventPublished records a confirmed publication.
func (s *StoreImpl) MarkOutboxEventPublished(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE outbox_events SET published_at = NOW(), last_error = NULL WHERE id = $1`, id)
	return err
}

// MarkOutboxEventFailed records a publication failure; the row stays unpublished.
func (s *StoreImpl) MarkOutboxEventFailed(ctx context.Context, id int64, cause string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE outbox_events SET attempts = attempts + 1, last_error = $2 WHERE id = $1`, id, cause)
	return err
}
