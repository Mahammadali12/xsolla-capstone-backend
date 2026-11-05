package service

import (
	"context"

	"xsolla-capstone-api/internal/store"
)

// FileService coordinates file operations.
type FileService struct {
	store *store.Store
}

// NewFileService creates a FileService backed by the given store.
func NewFileService(s *store.Store) *FileService {
	return &FileService{store: s}
}

// CreateFile creates a new file record.
func (s *FileService) CreateFile(ctx context.Context, uploadedBy int64, originalName, storageKey, mimeType string, sizeBytes int64) (*store.File, error) {
	return s.store.CreateFile(ctx, uploadedBy, originalName, storageKey, mimeType, sizeBytes)
}

// GetFileByID returns a file by ID.
func (s *FileService) GetFileByID(ctx context.Context, id int64) (*store.File, error) {
	return s.store.GetFileByID(ctx, id)
}

// GetFilesForUser returns all files uploaded by a user.
func (s *FileService) GetFilesForUser(ctx context.Context, uploadedBy int64) ([]store.File, error) {
	return s.store.GetFilesForUser(ctx, uploadedBy)
}

// DeleteFile soft-deletes a file record.
func (s *FileService) DeleteFile(ctx context.Context, id int64) error {
	return s.store.DeleteFile(ctx, id)
}
