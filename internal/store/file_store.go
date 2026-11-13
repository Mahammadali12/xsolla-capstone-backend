package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	// ErrFileNotFound is returned when a file does not exist or is deleted.
	ErrFileNotFound = errors.New("file not found")
)

// File represents a file row from the database.
type File struct {
	ID           int64
	UploadedBy   int64
	OriginalName string
	StorageKey   string
	MimeType     string
	SizeBytes    int64
	CreatedAt    time.Time
}

// ContentItemRef links a file to its content item.
type ContentItemRef struct {
	ID       int64
	ModuleID int64
}

// SubmissionRef links a file to its submission.
type SubmissionRef struct {
	ID            int64
	StudentID     int64
	ContentItemID int64
}

// CreateFile inserts a new file record.
func (s *StoreImpl) CreateFile(ctx context.Context, uploadedBy int64, originalName, storageKey, mimeType string, sizeBytes int64) (*File, error) {
	var f File
	err := s.DB().CreateFile(ctx, uploadedBy, originalName, storageKey, mimeType, sizeBytes).Scan(
		&f.ID, &f.UploadedBy, &f.OriginalName, &f.StorageKey, &f.MimeType, &f.SizeBytes, &f.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// GetFileByID returns a file by ID if it exists and is not deleted.
func (s *StoreImpl) GetFileByID(ctx context.Context, id int64) (*File, error) {
	var f File
	err := s.DB().GetFileByID(ctx, id).Scan(
		&f.ID, &f.UploadedBy, &f.OriginalName, &f.StorageKey, &f.MimeType, &f.SizeBytes, &f.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFileNotFound
		}
		return nil, err
	}
	return &f, nil
}

// GetFilesForUser returns all files uploaded by a user.
func (s *StoreImpl) GetFilesForUser(ctx context.Context, uploadedBy int64) ([]File, error) {
	rows, err := s.DB().GetFilesForUser(ctx, uploadedBy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.UploadedBy, &f.OriginalName, &f.StorageKey, &f.MimeType, &f.SizeBytes, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

// DeleteFile soft-deletes a file record.
func (s *StoreImpl) DeleteFile(ctx context.Context, id int64) error {
	result, err := s.DB().DeleteFile(ctx, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrFileNotFound
	}
	return nil
}

// GetContentItemForFile returns the content item linked to a file, if any.
func (s *StoreImpl) GetContentItemForFile(ctx context.Context, fileID int64) (*ContentItemRef, error) {
	var ref ContentItemRef
	err := s.DB().GetContentItemForFile(ctx, fileID).Scan(&ref.ID, &ref.ModuleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ref, nil
}

// GetSubmissionForFile returns the submission linked to a file, if any.
func (s *StoreImpl) GetSubmissionForFile(ctx context.Context, fileID int64) (*SubmissionRef, error) {
	var ref SubmissionRef
	err := s.DB().GetSubmissionForFile(ctx, fileID).Scan(&ref.ID, &ref.StudentID, &ref.ContentItemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ref, nil
}

// FileReferencedCount returns how many content items/submissions reference this file.
func (s *StoreImpl) FileReferencedCount(ctx context.Context, fileID int64) (int64, error) {
	var count int64
	err := s.DB().FileReferencedCount(ctx, fileID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// Event represents an event row from the database.
type Event struct {
	ID            int64
	Title         string
	Kind          string
	StartsAt      time.Time
	EndsAt        time.Time
	AllDay        bool
	Location      *string
	Description   *string
	MeetingURL    *string
	ModuleID      *int64
	GoogleEventID *string
	CreatedAt     time.Time
}

// ErrEventNotFound is returned when an event does not exist or is deleted.
var ErrEventNotFound = errors.New("event not found")

// ErrMentorSlotNotFound is returned when a mentor slot does not exist or is deleted.
var ErrMentorSlotNotFound = errors.New("mentor slot not found")

// ErrStudentHasBooking is returned when a student already has an upcoming booking.
var ErrStudentHasBooking = errors.New("student already has an upcoming booking")

// ErrSlotNotBookable is returned when a slot is already booked or has started.
var ErrSlotNotBookable = errors.New("slot is not bookable")

// MentorSlot is a single bookable 1:1 time offered by a mentor.
// booked_by IS NULL means the slot is free; set means it is booked by that student.
type MentorSlot struct {
	ID            int64
	MentorID      int64
	MentorName    string
	StartsAt      time.Time
	EndsAt        time.Time
	BookedBy      *int64
	BookedByName  *string
	GoogleEventID *string
	MeetingURL    *string
	CreatedAt     time.Time
}

// CreateEvent inserts a new event record.
func (s *StoreImpl) CreateEvent(ctx context.Context, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, meetingURL *string, moduleID *int64) (*Event, error) {
	var e Event
	err := s.DB().CreateEvent(ctx, title, kind, startsAt, endsAt, allDay, location, description, meetingURL, moduleID).Scan(
		&e.ID, &e.Title, &e.Kind, &e.StartsAt, &e.EndsAt, &e.AllDay, &e.Location, &e.Description, &e.MeetingURL, &e.ModuleID, &e.GoogleEventID, &e.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// UpdateEventGoogle links an event to its Google Calendar event and stores the Meet link Google returned.
func (s *StoreImpl) UpdateEventGoogle(ctx context.Context, eventID int64, googleEventID *string, meetingURL *string) error {
	_, err := s.DB().ExecContext(ctx,
		`UPDATE events SET google_event_id = COALESCE($1, google_event_id), meeting_url = COALESCE($2, meeting_url) WHERE id = $3`,
		googleEventID, meetingURL, eventID,
	)
	return err
}

// GetEventByID returns an event by ID if it exists and is not deleted.
func (s *StoreImpl) GetEventByID(ctx context.Context, id int64) (*Event, error) {
	var e Event
	var location *string
	var description *string
	var meetingURL *string
	var moduleID *int64
	var googleEventID *string
	err := s.DB().GetEventByID(ctx, id).Scan(
		&e.ID, &e.Title, &e.Kind, &e.StartsAt, &e.EndsAt, &e.AllDay, &location, &description, &meetingURL, &moduleID, &googleEventID, &e.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}
	e.Location = location
	e.Description = description
	e.MeetingURL = meetingURL
	e.ModuleID = moduleID
	e.GoogleEventID = googleEventID
	return &e, nil
}

// ListEventsByRange returns every non-deleted event in the given date range,
// whatever the caller's role (the route already authenticated them).
func (s *StoreImpl) ListEventsByRange(ctx context.Context, from time.Time, to time.Time) ([]Event, error) {
	rows, err := s.DB().ListEventsByRange(ctx, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var location *string
		var description *string
		var meetingURL *string
		var moduleID *int64
		var googleEventID *string
		if err := rows.Scan(&e.ID, &e.Title, &e.Kind, &e.StartsAt, &e.EndsAt, &e.AllDay, &location, &description, &meetingURL, &moduleID, &googleEventID, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Location = location
		e.Description = description
		e.MeetingURL = meetingURL
		e.ModuleID = moduleID
		e.GoogleEventID = googleEventID
		events = append(events, e)
	}
	return events, nil
}

// UpdateEvent updates an event record.
func (s *StoreImpl) UpdateEvent(ctx context.Context, id int64, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, meetingURL *string, moduleID *int64, userID int64, isAdmin bool) (*Event, error) {
	var e Event
	var loc *string
	var desc *string
	var meet *string
	var modID *int64
	var googleEventID *string
	err := s.DB().UpdateEvent(ctx, id, title, kind, startsAt, endsAt, allDay, location, description, meetingURL, moduleID, userID, isAdmin).Scan(
		&e.ID, &e.Title, &e.Kind, &e.StartsAt, &e.EndsAt, &e.AllDay, &loc, &desc, &meet, &modID, &googleEventID, &e.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}
	e.Location = loc
	e.Description = desc
	e.MeetingURL = meet
	e.ModuleID = modID
	e.GoogleEventID = googleEventID
	return &e, nil
}

// DeleteEvent soft-deletes an event record.
func (s *StoreImpl) DeleteEvent(ctx context.Context, id int64, userID int64, isAdmin bool) error {
	result, err := s.DB().DeleteEvent(ctx, id, userID, isAdmin)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrEventNotFound
	}
	return nil
}

// CreateMentorSlot inserts a single bookable 1:1 slot.
func (s *StoreImpl) CreateMentorSlot(ctx context.Context, mentorID int64, startsAt, endsAt time.Time) (*MentorSlot, error) {
	var id, mentorIDCol int64
	var starts, ends, created time.Time
	var bookedBy *int64
	err := s.DB().CreateMentorSlot(ctx, mentorID, startsAt, endsAt).Scan(
		&id, &mentorIDCol, &starts, &ends, &bookedBy, &created,
	)
	if err != nil {
		return nil, err
	}
	return &MentorSlot{
		ID:        id,
		MentorID:  mentorIDCol,
		StartsAt:  starts,
		EndsAt:    ends,
		BookedBy:  bookedBy,
		CreatedAt: created,
	}, nil
}

// MentorSlotsOverlap returns the count of non-deleted slots for a mentor that overlap a given range.
func (s *StoreImpl) MentorSlotsOverlap(ctx context.Context, mentorID int64, startsAt, endsAt time.Time) (int, error) {
	var count int
	err := s.DB().MentorSlotsOverlap(ctx, mentorID, startsAt, endsAt).Scan(&count)
	return count, err
}

// ListMentorSlots returns slots visible to the caller.
func (s *StoreImpl) ListMentorSlots(ctx context.Context, userID int64, roles []string) ([]MentorSlot, error) {
	rows, err := s.DB().ListMentorSlots(ctx, userID, roles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slots []MentorSlot
	for rows.Next() {
		var slot MentorSlot
		var bookedBy *int64
		var bookedByName *string
		var meetingURL *string
		var mentorName string
		if err := rows.Scan(&slot.ID, &slot.MentorID, &mentorName, &slot.StartsAt, &slot.EndsAt, &bookedBy, &bookedByName, &meetingURL, &slot.CreatedAt); err != nil {
			return nil, err
		}
		slot.BookedBy = bookedBy
		slot.BookedByName = bookedByName
		slot.MeetingURL = meetingURL
		slot.MentorName = mentorName
		slots = append(slots, slot)
	}
	return slots, nil
}

// GetMentorSlotByID returns a non-deleted slot by ID.
func (s *StoreImpl) GetMentorSlotByID(ctx context.Context, id int64) (*MentorSlot, error) {
	var slotID, mentorIDCol int64
	var starts, ends, created time.Time
	var bookedBy *int64
	var googleEventID *string
	var meetingURL *string
	err := s.DB().GetMentorSlotByID(ctx, id).Scan(
		&slotID, &mentorIDCol, &starts, &ends, &bookedBy, &googleEventID, &meetingURL, &created,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMentorSlotNotFound
		}
		return nil, err
	}
	return &MentorSlot{
		ID:            slotID,
		MentorID:      mentorIDCol,
		StartsAt:      starts,
		EndsAt:        ends,
		BookedBy:      bookedBy,
		GoogleEventID: googleEventID,
		MeetingURL:    meetingURL,
		CreatedAt:     created,
	}, nil
}

// UpdateMentorSlotGoogle links a booking to its Google Calendar event and Meet link.
func (s *StoreImpl) UpdateMentorSlotGoogle(ctx context.Context, slotID int64, googleEventID *string, meetingURL *string) error {
	_, err := s.DB().ExecContext(ctx,
		`UPDATE mentor_slots SET google_event_id = COALESCE($2, google_event_id), meeting_url = COALESCE($3, meeting_url)
		 WHERE id = $1 AND deleted_at IS NULL`,
		slotID, googleEventID, meetingURL,
	)
	return err
}

// DeleteMentorSlot soft-deletes a slot if the caller is authorized and it is free.
func (s *StoreImpl) DeleteMentorSlot(ctx context.Context, userID int64, roles []string, id int64) error {
	result, err := s.DB().DeleteMentorSlot(ctx, userID, roles, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrMentorSlotNotFound
	}
	return nil
}

// DeleteMentorSlotsByMentor soft-deletes every slot a mentor offers and returns the deleted rows.
func (s *StoreImpl) DeleteMentorSlotsByMentor(ctx context.Context, mentorID int64) ([]MentorSlot, error) {
	rows, err := s.DB().DeleteMentorSlotsByMentor(ctx, mentorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slots []MentorSlot
	for rows.Next() {
		var slot MentorSlot
		if err := rows.Scan(&slot.ID, &slot.GoogleEventID); err != nil {
			return nil, err
		}
		slot.MentorID = mentorID
		slots = append(slots, slot)
	}
	return slots, rows.Err()
}

// BookMentorSlot atomically books a free, not-yet-started slot for a student.
func (s *StoreImpl) BookMentorSlot(ctx context.Context, studentID int64, slotID int64) (*MentorSlot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := WithTx(tx)

	// Serialize concurrent bookings by the same student.
	if err := q.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, studentID).Scan(&studentID); err != nil {
		return nil, err
	}

	// Student may have at most one upcoming booking.
	existing, err := q.BookMentorSlotPrecheck(ctx, studentID)
	if err != nil {
		return nil, err
	}
	if existing > 0 {
		return nil, ErrStudentHasBooking
	}

	// Claim the slot atomically within the transaction.
	row, err := q.BookMentorSlot(ctx, slotID, studentID)
	if err != nil {
		return nil, err
	}

	var id, mentorIDCol int64
	var starts, ends, created time.Time
	var bookedBy *int64
	if err := row.Scan(&id, &mentorIDCol, &starts, &ends, &bookedBy, &created); err != nil {
		return nil, err
	}

	slot := MentorSlot{
		ID:        id,
		MentorID:  mentorIDCol,
		StartsAt:  starts,
		EndsAt:    ends,
		BookedBy:  bookedBy,
		CreatedAt: created,
	}

	// The event joins the booking in this transaction: if either fails, both roll back.
	mentorName, mentorEmail, err := q.userNameEmail(ctx, mentorIDCol)
	if err != nil {
		return nil, err
	}
	studentName, studentEmail, err := q.userNameEmail(ctx, studentID)
	if err != nil {
		return nil, err
	}
	payload, err := mentorSlotBookedData(slot, mentorName, mentorEmail, studentName, studentEmail)
	if err != nil {
		return nil, err
	}
	if err := q.InsertOutboxEvent(ctx, MentorSlotBookedEventType, payload); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &slot, nil
}

// CancelMentorSlotBooking clears a booking if the caller is authorized and the slot hasn't started.
func (s *StoreImpl) CancelMentorSlotBooking(ctx context.Context, userID int64, roles []string, slotID int64) (*MentorSlot, error) {
	row, err := s.DB().CancelMentorSlotBooking(ctx, userID, roles, slotID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMentorSlotNotFound
		}
		return nil, err
	}
	var id, mentorIDCol int64
	var starts, ends, created time.Time
	var bookedBy *int64
	if err := row.Scan(&id, &mentorIDCol, &starts, &ends, &bookedBy, &created); err != nil {
		return nil, err
	}
	return &MentorSlot{
		ID:        id,
		MentorID:  mentorIDCol,
		StartsAt:  starts,
		EndsAt:    ends,
		BookedBy:  bookedBy,
		CreatedAt: created,
	}, nil
}
