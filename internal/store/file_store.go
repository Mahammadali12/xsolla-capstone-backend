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
