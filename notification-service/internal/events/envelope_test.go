package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

func validEnvelope(t *testing.T) Envelope {
	t.Helper()
	body, err := os.ReadFile("testdata/mentor_slot_booked.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	envelope, err := Decode(body)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return envelope
}

func TestDecodeValidFixture(t *testing.T) {
	body, err := os.ReadFile("testdata/mentor_slot_booked.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	envelope, err := Decode(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if envelope.ID != 18422 || envelope.Type != "mentor_slot.booked" || envelope.Version != 1 || envelope.Producer != "lms-api" {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	if envelope.OccurredAt.Location() != time.UTC {
		t.Fatalf("occurred_at is not UTC: %v", envelope.OccurredAt.Location())
	}

	// The struct must serialize back to the canonical contract fixture.
	encoded, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(append(encoded, '\n'), body) {
		t.Fatalf("round trip differs from fixture:\n%s", encoded)
	}
}

func TestRecipientsPayload(t *testing.T) {
	envelope := validEnvelope(t)
	var data struct {
		Recipients []Recipient `json:"recipients"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if len(data.Recipients) != 2 {
		t.Fatalf("got %d recipients, want 2", len(data.Recipients))
	}
	if data.Recipients[0].Role != "mentor" || data.Recipients[1].Role != "student" {
		t.Fatalf("unexpected recipients: %+v", data.Recipients)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Envelope)
	}{
		{"zero id", func(e *Envelope) { e.ID = 0 }},
		{"negative id", func(e *Envelope) { e.ID = -1 }},
		{"empty type", func(e *Envelope) { e.Type = "" }},
		{"type without dot", func(e *Envelope) { e.Type = "mentor_slot" }},
		{"type with uppercase", func(e *Envelope) { e.Type = "Mentor_Slot.Booked" }},
		{"zero version", func(e *Envelope) { e.Version = 0 }},
		{"zero occurred_at", func(e *Envelope) { e.OccurredAt = time.Time{} }},
		{"empty producer", func(e *Envelope) { e.Producer = "" }},
		{"blank producer", func(e *Envelope) { e.Producer = "   " }},
		{"missing data", func(e *Envelope) { e.Data = nil }},
		{"null data", func(e *Envelope) { e.Data = json.RawMessage("null") }},
		{"array data", func(e *Envelope) { e.Data = json.RawMessage(`[]`) }},
		{"invalid data", func(e *Envelope) { e.Data = json.RawMessage(`{`) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			envelope := validEnvelope(t)
			tc.mutate(&envelope)
			if err := envelope.Validate(); !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("got %v, want ErrInvalidEnvelope", err)
			}
		})
	}
}

func TestDecodeRejectsBadMessages(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"not json", `not json`},
		{"truncated", `{"id": 1`},
		{"trailing garbage", `{"id":1,"type":"event.created","version":1,"occurred_at":"2026-11-13T12:00:00Z","producer":"lms-api","data":{}} {}`},
		{"id as string", `{"id":"18422","type":"event.created","version":1,"occurred_at":"2026-11-13T12:00:00Z","producer":"lms-api","data":{}}`},
		{"invalid timestamp", `{"id":18422,"type":"event.created","version":1,"occurred_at":"yesterday","producer":"lms-api","data":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode([]byte(tc.body)); !errors.Is(err, ErrInvalidEnvelope) {
				t.Fatalf("got %v, want ErrInvalidEnvelope", err)
			}
		})
	}
}

func TestDecodeAllowsUnknownFields(t *testing.T) {
	body := `{
		"id": 1,
		"type": "event.created",
		"version": 1,
		"occurred_at": "2026-11-13T12:00:00Z",
		"producer": "lms-api",
		"trace_id": "abc",
		"data": {"event_id": 9}
	}`
	if _, err := Decode([]byte(body)); err != nil {
		t.Fatalf("unknown fields should be allowed: %v", err)
	}
}
