package store

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMentorSlotBookedData(t *testing.T) {
	student := int64(42)
	slot := MentorSlot{
		ID:       123,
		MentorID: 7,
		StartsAt: time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 11, 19, 10, 30, 0, 0, time.UTC),
		BookedBy: &student,
	}

	payload, err := mentorSlotBookedData(slot, "Mentor One", "mentor@example.com", "Student One", "student@example.com")
	if err != nil {
		t.Fatalf("payload: %v", err)
	}

	var got mentorSlotBookedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SlotID != 123 || !got.StartsAt.Equal(slot.StartsAt) || !got.EndsAt.Equal(slot.EndsAt) {
		t.Fatalf("unexpected slot data: %+v", got)
	}
	if got.StartsAt.Location() != time.UTC || got.EndsAt.Location() != time.UTC {
		t.Fatalf("timestamps are not UTC: %v, %v", got.StartsAt, got.EndsAt)
	}
	if len(got.Recipients) != 2 {
		t.Fatalf("got %d recipients, want 2", len(got.Recipients))
	}
	mentor, studentRecipient := got.Recipients[0], got.Recipients[1]
	if mentor.ID != 7 || mentor.Role != "mentor" || mentor.Name != "Mentor One" || mentor.Email != "mentor@example.com" {
		t.Fatalf("unexpected mentor: %+v", mentor)
	}
	if studentRecipient.ID != 42 || studentRecipient.Role != "student" || studentRecipient.Name != "Student One" || studentRecipient.Email != "student@example.com" {
		t.Fatalf("unexpected student: %+v", studentRecipient)
	}
}

func TestMentorSlotBookedDataRequiresStudent(t *testing.T) {
	slot := MentorSlot{ID: 123, MentorID: 7}
	if _, err := mentorSlotBookedData(slot, "Mentor One", "mentor@example.com", "Student One", "student@example.com"); err == nil {
		t.Fatal("want an error for a slot without a student")
	}
}
