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

func TestModuleCreatedData(t *testing.T) {
	m := Module{
		ID:          1,
		Title:       "Backend",
		Description: "Go and Postgres",
		OpensAt:     time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC),
		ClosesAt:    time.Date(2026, 12, 20, 9, 0, 0, 0, time.UTC),
		CreatedBy:   7,
	}
	recipients := []recipientSnapshot{{ID: 42, Role: "student", Name: "Student One", Email: "student@example.com"}}

	payload, err := moduleCreatedData(m, recipients)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}

	var got moduleCreatedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ModuleID != m.ID || got.Title != m.Title || got.CreatedBy != m.CreatedBy {
		t.Fatalf("unexpected module data: %+v", got)
	}
	if got.OpensAt.Location() != time.UTC || got.ClosesAt.Location() != time.UTC {
		t.Fatalf("timestamps are not UTC: %v, %v", got.OpensAt, got.ClosesAt)
	}
	if len(got.Recipients) != 1 || got.Recipients[0].Role != "student" {
		t.Fatalf("unexpected recipients: %+v", got.Recipients)
	}
}

func TestContentItemUploadedData(t *testing.T) {
	ci := ContentItem{ID: 5, ModuleID: 1, Type: "assignment", Title: "Week 1", Description: "Build it"}
	recipients := []recipientSnapshot{{ID: 42, Role: "student", Name: "Student One", Email: "student@example.com"}}

	payload, err := contentItemUploadedData(ci, "Backend", recipients)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}

	var got contentItemUploadedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ContentItemID != ci.ID || got.ModuleTitle != "Backend" || got.Type != "assignment" {
		t.Fatalf("unexpected content item data: %+v", got)
	}
	if len(got.Recipients) != 1 || got.Recipients[0].ID != 42 {
		t.Fatalf("unexpected recipients: %+v", got.Recipients)
	}
}

func TestEventCreatedData(t *testing.T) {
	moduleID := int64(1)
	e := Event{
		ID:       9,
		Title:    "Kickoff",
		Kind:     "meeting",
		StartsAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC),
		ModuleID: &moduleID,
	}
	recipients := []recipientSnapshot{
		{ID: 7, Role: "lecturer", Name: "Lecturer One", Email: "lecturer@example.com"},
		{ID: 42, Role: "student", Name: "Student One", Email: "student@example.com"},
	}

	payload, err := eventCreatedData(e, recipients)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}

	var got eventCreatedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.EventID != e.ID || got.Title != e.Title || got.Kind != e.Kind {
		t.Fatalf("unexpected event data: %+v", got)
	}
	if len(got.Recipients) != 2 || got.Recipients[1].Role != "student" {
		t.Fatalf("unexpected recipients: %+v", got.Recipients)
	}
}

func TestMentorAvailabilityCreatedData(t *testing.T) {
	slots := []MentorSlot{
		{ID: 1, StartsAt: time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 11, 20, 10, 30, 0, 0, time.UTC)},
		{ID: 2, StartsAt: time.Date(2026, 11, 20, 10, 30, 0, 0, time.UTC), EndsAt: time.Date(2026, 11, 20, 11, 0, 0, 0, time.UTC)},
	}
	recipients := []recipientSnapshot{{ID: 42, Role: "student", Name: "Student One", Email: "student@example.com"}}

	payload, err := mentorAvailabilityCreatedData(7, "Mentor One", slots[0].StartsAt, slots[1].EndsAt, slots, recipients)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}

	var got mentorAvailabilityCreatedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.MentorID != 7 || got.MentorName != "Mentor One" || len(got.Slots) != 2 {
		t.Fatalf("unexpected availability data: %+v", got)
	}
	if got.Slots[0].ID != 1 || got.Slots[1].ID != 2 {
		t.Fatalf("unexpected slots: %+v", got.Slots)
	}
}

func TestSubmissionCreatedData(t *testing.T) {
	sub := Submission{ID: 3, ContentItemID: 5, StudentID: 42, SubmittedAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	recipients := []recipientSnapshot{{ID: 7, Role: "mentor", Name: "Mentor One", Email: "mentor@example.com"}}

	payload, err := submissionCreatedData(sub, "Week 1", 1, "Backend", "Student One", recipients)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}

	var got submissionCreatedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SubmissionID != sub.ID || got.AssignmentTitle != "Week 1" || got.ModuleTitle != "Backend" {
		t.Fatalf("unexpected submission data: %+v", got)
	}
	if len(got.Recipients) != 1 || got.Recipients[0].Role != "mentor" {
		t.Fatalf("unexpected recipients: %+v", got.Recipients)
	}
}

func TestEnrollmentGradedData(t *testing.T) {
	recipients := []recipientSnapshot{{ID: 42, Role: "student", Name: "Student One", Email: "student@example.com"}}

	payload, err := enrollmentGradedData(1, "Backend", 42, "Student One", "green", 7, "Mentor One", recipients)
	if err != nil {
		t.Fatalf("payload: %v", err)
	}

	var got enrollmentGradedPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Grade != "green" || got.GradedByName != "Mentor One" || got.StudentID != 42 {
		t.Fatalf("unexpected grade data: %+v", got)
	}
	if len(got.Recipients) != 1 || got.Recipients[0].Role != "student" {
		t.Fatalf("unexpected recipients: %+v", got.Recipients)
	}
}

func TestPrimaryRole(t *testing.T) {
	if role := primaryRole([]string{"admin", "student"}); role != "student" {
		t.Fatalf("primaryRole = %q, want student", role)
	}
	if role := primaryRole(nil); role != "student" {
		t.Fatalf("primaryRole = %q, want student", role)
	}
}
