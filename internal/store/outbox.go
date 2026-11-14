package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// MentorSlotBookedEventType is the domain event emitted when a student books a slot.
const MentorSlotBookedEventType = "mentor_slot.booked"

// MentorSlotCanceledEventType is the domain event emitted when a booking is cancelled.
const MentorSlotCanceledEventType = "mentor_slot.canceled"

const (
	// ModuleCreatedEventType is emitted when a module is created.
	ModuleCreatedEventType = "module.created"
	// ContentItemUploadedEventType is emitted when a content item is added to a module.
	ContentItemUploadedEventType = "content_item.uploaded"
	// EventCreatedEventType is emitted when a calendar event is created.
	EventCreatedEventType = "event.created"
	// MentorAvailabilityCreatedEventType is emitted when a mentor opens a new slot window.
	MentorAvailabilityCreatedEventType = "mentor_availability.created"
	// SubmissionCreatedEventType is emitted when a student submits an assignment.
	SubmissionCreatedEventType = "submission.created"
	// EnrollmentGradedEventType is emitted when a mentor posts a student's module grade.
	EnrollmentGradedEventType = "enrollment.graded"
)

// recipientSnapshot is the minimal contact snapshot the notification service
// needs. Do not add more personal data than delivery requires.
type recipientSnapshot struct {
	ID    int64  `json:"id"`
	Role  string `json:"role"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// mentorSlotBookedPayload is the event's data object.
type mentorSlotBookedPayload struct {
	SlotID     int64               `json:"slot_id"`
	StartsAt   time.Time           `json:"starts_at"`
	EndsAt     time.Time           `json:"ends_at"`
	Recipients []recipientSnapshot `json:"recipients"`
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
		Recipients: []recipientSnapshot{
			{ID: slot.MentorID, Role: "mentor", Name: mentorName, Email: mentorEmail},
			{ID: *slot.BookedBy, Role: "student", Name: studentName, Email: studentEmail},
		},
	})
}

// mentorSlotCanceledPayload is the event's data object for a cancellation.
type mentorSlotCanceledPayload struct {
	SlotID     int64               `json:"slot_id"`
	StartsAt   time.Time           `json:"starts_at"`
	EndsAt     time.Time           `json:"ends_at"`
	Recipients []recipientSnapshot `json:"recipients"`
}

// mentorSlotCanceledData builds the event payload for a cancelled booking.
// The student ID is captured before the booking is cleared, since the slot
// row no longer references them after cancellation.
func mentorSlotCanceledData(slotID int64, startsAt, endsAt time.Time, mentorID, studentID int64, mentorName, mentorEmail, studentName, studentEmail string) ([]byte, error) {
	return json.Marshal(mentorSlotCanceledPayload{
		SlotID:   slotID,
		StartsAt: startsAt.UTC(),
		EndsAt:   endsAt.UTC(),
		Recipients: []recipientSnapshot{
			{ID: mentorID, Role: "mentor", Name: mentorName, Email: mentorEmail},
			{ID: studentID, Role: "student", Name: studentName, Email: studentEmail},
		},
	})
}

// moduleCreatedPayload is the event's data object for a new module.
type moduleCreatedPayload struct {
	ModuleID    int64               `json:"module_id"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	OpensAt     time.Time           `json:"opens_at"`
	ClosesAt    time.Time           `json:"closes_at"`
	CreatedBy   int64               `json:"created_by"`
	Recipients  []recipientSnapshot `json:"recipients"`
}

// moduleCreatedData builds the event payload for a created module.
func moduleCreatedData(m Module, recipients []recipientSnapshot) ([]byte, error) {
	return json.Marshal(moduleCreatedPayload{
		ModuleID:    m.ID,
		Title:       m.Title,
		Description: m.Description,
		OpensAt:     m.OpensAt.UTC(),
		ClosesAt:    m.ClosesAt.UTC(),
		CreatedBy:   m.CreatedBy,
		Recipients:  recipients,
	})
}

// contentItemUploadedPayload is the event's data object for a new content item.
type contentItemUploadedPayload struct {
	ContentItemID int64               `json:"content_item_id"`
	ModuleID      int64               `json:"module_id"`
	ModuleTitle   string              `json:"module_title"`
	Type          string              `json:"type"`
	Title         string              `json:"title"`
	Description   string              `json:"description"`
	Recipients    []recipientSnapshot `json:"recipients"`
}

// contentItemUploadedData builds the event payload for a created content item.
func contentItemUploadedData(ci ContentItem, moduleTitle string, recipients []recipientSnapshot) ([]byte, error) {
	return json.Marshal(contentItemUploadedPayload{
		ContentItemID: ci.ID,
		ModuleID:      ci.ModuleID,
		ModuleTitle:   moduleTitle,
		Type:          ci.Type,
		Title:         ci.Title,
		Description:   ci.Description,
		Recipients:    recipients,
	})
}

// eventCreatedPayload is the event's data object for a new calendar event.
type eventCreatedPayload struct {
	EventID    int64               `json:"event_id"`
	Title      string              `json:"title"`
	Kind       string              `json:"kind"`
	StartsAt   time.Time           `json:"starts_at"`
	EndsAt     time.Time           `json:"ends_at"`
	AllDay     bool                `json:"all_day"`
	ModuleID   *int64              `json:"module_id"`
	Recipients []recipientSnapshot `json:"recipients"`
}

// eventCreatedData builds the event payload for a created calendar event.
func eventCreatedData(e Event, recipients []recipientSnapshot) ([]byte, error) {
	return json.Marshal(eventCreatedPayload{
		EventID:    e.ID,
		Title:      e.Title,
		Kind:       e.Kind,
		StartsAt:   e.StartsAt.UTC(),
		EndsAt:     e.EndsAt.UTC(),
		AllDay:     e.AllDay,
		ModuleID:   e.ModuleID,
		Recipients: recipients,
	})
}

// availabilitySlot is one 30-minute slot inside a created availability window.
type availabilitySlot struct {
	ID       int64     `json:"id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

// mentorAvailabilityCreatedPayload is the event's data object for a new slot window.
type mentorAvailabilityCreatedPayload struct {
	MentorID   int64               `json:"mentor_id"`
	MentorName string              `json:"mentor_name"`
	StartsAt   time.Time           `json:"starts_at"`
	EndsAt     time.Time           `json:"ends_at"`
	Slots      []availabilitySlot  `json:"slots"`
	Recipients []recipientSnapshot `json:"recipients"`
}

// mentorAvailabilityCreatedData builds the event payload for a created slot window.
func mentorAvailabilityCreatedData(mentorID int64, mentorName string, startsAt, endsAt time.Time, slots []MentorSlot, recipients []recipientSnapshot) ([]byte, error) {
	payloadSlots := make([]availabilitySlot, len(slots))
	for i, slot := range slots {
		payloadSlots[i] = availabilitySlot{ID: slot.ID, StartsAt: slot.StartsAt.UTC(), EndsAt: slot.EndsAt.UTC()}
	}
	return json.Marshal(mentorAvailabilityCreatedPayload{
		MentorID:   mentorID,
		MentorName: mentorName,
		StartsAt:   startsAt.UTC(),
		EndsAt:     endsAt.UTC(),
		Slots:      payloadSlots,
		Recipients: recipients,
	})
}

// submissionCreatedPayload is the event's data object for a student submission.
type submissionCreatedPayload struct {
	SubmissionID    int64               `json:"submission_id"`
	ContentItemID   int64               `json:"content_item_id"`
	AssignmentTitle string              `json:"assignment_title"`
	ModuleID        int64               `json:"module_id"`
	ModuleTitle     string              `json:"module_title"`
	StudentID       int64               `json:"student_id"`
	StudentName     string              `json:"student_name"`
	SubmittedAt     time.Time           `json:"submitted_at"`
	Recipients      []recipientSnapshot `json:"recipients"`
}

// submissionCreatedData builds the event payload for a created submission.
func submissionCreatedData(sub Submission, assignmentTitle string, moduleID int64, moduleTitle, studentName string, recipients []recipientSnapshot) ([]byte, error) {
	return json.Marshal(submissionCreatedPayload{
		SubmissionID:    sub.ID,
		ContentItemID:   sub.ContentItemID,
		AssignmentTitle: assignmentTitle,
		ModuleID:        moduleID,
		ModuleTitle:     moduleTitle,
		StudentID:       sub.StudentID,
		StudentName:     studentName,
		SubmittedAt:     sub.SubmittedAt.UTC(),
		Recipients:      recipients,
	})
}

// enrollmentGradedPayload is the event's data object for a posted module grade.
type enrollmentGradedPayload struct {
	ModuleID     int64               `json:"module_id"`
	ModuleTitle  string              `json:"module_title"`
	StudentID    int64               `json:"student_id"`
	StudentName  string              `json:"student_name"`
	Grade        string              `json:"grade"`
	GradedBy     int64               `json:"graded_by"`
	GradedByName string              `json:"graded_by_name"`
	Recipients   []recipientSnapshot `json:"recipients"`
}

// enrollmentGradedData builds the event payload for a posted enrollment grade.
func enrollmentGradedData(moduleID int64, moduleTitle string, studentID int64, studentName, grade string, gradedBy int64, gradedByName string, recipients []recipientSnapshot) ([]byte, error) {
	return json.Marshal(enrollmentGradedPayload{
		ModuleID:     moduleID,
		ModuleTitle:  moduleTitle,
		StudentID:    studentID,
		StudentName:  studentName,
		Grade:        grade,
		GradedBy:     gradedBy,
		GradedByName: gradedByName,
		Recipients:   recipients,
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
