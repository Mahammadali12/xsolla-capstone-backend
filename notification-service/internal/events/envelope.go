// Package events defines the JSON contract between the LMS producer and the
// notification service.
package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ErrInvalidEnvelope is returned when a message does not satisfy the event contract.
var ErrInvalidEnvelope = errors.New("invalid event envelope")

// typePattern is the domain.event naming convention, e.g. mentor_slot.booked.
var typePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// Envelope is the versioned message the LMS publishes and the notification
// service consumes. The routing key is Type.
type Envelope struct {
	// ID is the LMS outbox row id, not a domain entity id. It is never reused,
	// so a mentor slot can be booked, cancelled, and booked again with three IDs.
	ID int64 `json:"id"`
	// Type is the domain event name, e.g. mentor_slot.booked.
	Type string `json:"type"`
	// Version is the event schema version.
	Version int `json:"version"`
	// OccurredAt is when the business operation happened, in UTC.
	OccurredAt time.Time `json:"occurred_at"`
	// Producer identifies the service that emitted the event.
	Producer string `json:"producer"`
	// Data is the event-specific payload. It must be a JSON object.
	Data json.RawMessage `json:"data"`
}

// Recipient is the minimal contact snapshot carried in data.recipients.
// Payloads must not include more personal data than delivery needs.
type Recipient struct {
	ID    int64  `json:"id"`
	Role  string `json:"role"` // student, mentor, lecturer, or admin
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Decode parses and validates an event envelope. OccurredAt is normalized to UTC.
func Decode(body []byte) (Envelope, error) {
	var envelope Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, err
	}
	envelope.OccurredAt = envelope.OccurredAt.UTC()
	return envelope, nil
}

// Validate checks the required envelope fields and the data object.
func (e Envelope) Validate() error {
	if e.ID <= 0 {
		return fmt.Errorf("%w: id must be positive", ErrInvalidEnvelope)
	}
	if len(e.Type) > 128 || !typePattern.MatchString(e.Type) {
		return fmt.Errorf("%w: type must look like domain.event", ErrInvalidEnvelope)
	}
	if e.Version < 1 {
		return fmt.Errorf("%w: version must be at least 1", ErrInvalidEnvelope)
	}
	if e.OccurredAt.IsZero() {
		return fmt.Errorf("%w: occurred_at is required", ErrInvalidEnvelope)
	}
	if strings.TrimSpace(e.Producer) == "" || len(e.Producer) > 64 {
		return fmt.Errorf("%w: producer is required", ErrInvalidEnvelope)
	}
	data := bytes.TrimSpace(e.Data)
	if len(data) == 0 || data[0] != '{' || !json.Valid(data) {
		return fmt.Errorf("%w: data must be a JSON object", ErrInvalidEnvelope)
	}
	return nil
}
