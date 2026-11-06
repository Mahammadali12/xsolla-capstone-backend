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
	ID           int64
	StudentID    int64
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
	ID          int64
	Title       string
	Kind        string
	StartsAt    time.Time
	EndsAt      time.Time
	AllDay      bool
	Location    *string
	Description *string
	ModuleID    *int64
	CreatedAt   time.Time
}

// ErrEventNotFound is returned when an event does not exist or is deleted.
var ErrEventNotFound = errors.New("event not found")

// CreateEvent inserts a new event record.
func (s *StoreImpl) CreateEvent(ctx context.Context, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64) (*Event, error) {
	var e Event
	err := s.DB().CreateEvent(ctx, title, kind, startsAt, endsAt, allDay, location, description, moduleID).Scan(
		&e.ID, &e.Title, &e.Kind, &e.StartsAt, &e.EndsAt, &e.AllDay, &e.Location, &e.Description, &e.ModuleID, &e.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// GetEventByID returns an event by ID if it exists and is not deleted.
func (s *StoreImpl) GetEventByID(ctx context.Context, id int64) (*Event, error) {
	var e Event
	var location *string
	var description *string
	var moduleID *int64
	err := s.DB().GetEventByID(ctx, id).Scan(
		&e.ID, &e.Title, &e.Kind, &e.StartsAt, &e.EndsAt, &e.AllDay, &location, &description, &moduleID, &e.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}
	e.Location = location
	e.Description = description
	e.ModuleID = moduleID
	return &e, nil
}

// ListEventsByRange returns events in the given date range, filtered by user access.
func (s *StoreImpl) ListEventsByRange(ctx context.Context, from time.Time, to time.Time, userID int64, role string) ([]Event, error) {
	rows, err := s.DB().ListEventsByRange(ctx, from, to, userID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		var location *string
		var description *string
		var moduleID *int64
		if err := rows.Scan(&e.ID, &e.Title, &e.Kind, &e.StartsAt, &e.EndsAt, &e.AllDay, &location, &description, &moduleID, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Location = location
		e.Description = description
		e.ModuleID = moduleID
		events = append(events, e)
	}
	return events, nil
}

// UpdateEvent updates an event record.
func (s *StoreImpl) UpdateEvent(ctx context.Context, id int64, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64, userID int64, isAdmin bool) (*Event, error) {
	var e Event
	var loc *string
	var desc *string
	var modID *int64
	err := s.DB().UpdateEvent(ctx, id, title, kind, startsAt, endsAt, allDay, location, description, moduleID, userID, isAdmin).Scan(
		&e.ID, &e.Title, &e.Kind, &e.StartsAt, &e.EndsAt, &e.AllDay, &loc, &desc, &modID, &e.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}
	e.Location = loc
	e.Description = desc
	e.ModuleID = modID
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
