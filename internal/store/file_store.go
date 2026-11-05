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
	CourseID int64
}

// SubmissionRef links a file to its submission.
type SubmissionRef struct {
	ID           int64
	StudentID    int64
	ContentItemID int64
}

// CreateFile inserts a new file record.
func (s *Store) CreateFile(ctx context.Context, uploadedBy int64, originalName, storageKey, mimeType string, sizeBytes int64) (*File, error) {
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
func (s *Store) GetFileByID(ctx context.Context, id int64) (*File, error) {
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
func (s *Store) GetFilesForUser(ctx context.Context, uploadedBy int64) ([]File, error) {
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
func (s *Store) DeleteFile(ctx context.Context, id int64) error {
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
func (s *Store) GetContentItemForFile(ctx context.Context, fileID int64) (*ContentItemRef, error) {
	var ref ContentItemRef
	err := s.DB().GetContentItemForFile(ctx, fileID).Scan(&ref.ID, &ref.CourseID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ref, nil
}

// GetSubmissionForFile returns the submission linked to a file, if any.
func (s *Store) GetSubmissionForFile(ctx context.Context, fileID int64) (*SubmissionRef, error) {
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
func (s *Store) FileReferencedCount(ctx context.Context, fileID int64) (int64, error) {
	var count int64
	err := s.DB().FileReferencedCount(ctx, fileID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
