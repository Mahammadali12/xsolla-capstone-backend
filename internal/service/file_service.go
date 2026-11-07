package service

import (
	"context"
	"errors"
	"io"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// FileService coordinates file operations.
type FileService struct {
	store  store.Store
	storeS Storage
}

// Storage is the interface for object storage operations.
type Storage interface {
	// Upload stores a file in object storage.
	Upload(ctx context.Context, fileID int64, originalName, storageKey, mimeType string, sizeBytes int64, reader io.Reader) error
	// Download retrieves a file from object storage.
	Download(ctx context.Context, storageKey string) (io.ReadCloser, error)
	// Delete removes a file from object storage.
	Delete(ctx context.Context, storageKey string) error
	// PublicURL returns a public URL for accessing the file.
	PublicURL(storageKey string) string
}

// NewFileService creates a FileService backed by the given store and storage.
func NewFileService(s store.Store, st Storage) *FileService {
	return &FileService{store: s, storeS: st}
}

// ErrFileReferenced is returned when a file is still referenced by a content item or submission.
var ErrFileReferenced = errors.New("file is still referenced by a content item or submission")

// DownloadFile downloads a file from object storage.
func (s *FileService) DownloadFile(ctx context.Context, storageKey string) (io.ReadCloser, error) {
	return s.storeS.Download(ctx, storageKey)
}

// CreateFile creates a new file record.
func (s *FileService) CreateFile(ctx context.Context, uploadedBy int64, originalName, storageKey, mimeType string, sizeBytes int64, reader io.Reader) (*store.File, error) {
	// First upload to object storage
	if s.storeS != nil {
		if err := s.storeS.Upload(ctx, uploadedBy, originalName, storageKey, mimeType, sizeBytes, reader); err != nil {
			return nil, err
		}
	}

	// Then create DB record
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

// CanReadFile checks the link-rule access and returns the file if readable.
func (s *FileService) CanReadFile(ctx context.Context, userID int64, roles []string, fileID int64) (*store.File, error) {
	f, err := s.store.GetFileByID(ctx, fileID)
	if err != nil {
		return nil, err
	}

	// Owner or admin can always read
	if f.UploadedBy == userID || auth.HasRole(roles, "admin") {
		return f, nil
	}

	// Linked to a visible content item in an enrolled/teacher module
	if ci, err := s.store.GetContentItemForFile(ctx, fileID); err == nil && ci != nil {
		if enrolled, _ := s.store.IsUserEnrolled(ctx, ci.ModuleID, userID); enrolled {
			return f, nil
		}
		if auth.HasRole(roles, "lecturer") {
			if teacher, _ := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID); teacher {
				return f, nil
			}
		}
	}

	// Linked to a submission you own, or may grade (lecturer/admin enrolled in module)
	if sub, err := s.store.GetSubmissionForFile(ctx, fileID); err == nil && sub != nil {
		if sub.StudentID == userID {
			return f, nil
		}
		if auth.HasRole(roles, "lecturer") || auth.HasRole(roles, "admin") {
			if ci, err := s.store.GetContentItemByID(ctx, sub.ContentItemID); err == nil && ci != nil {
				if teacher, _ := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID); teacher {
					return f, nil
				}
			}
		}
	}

	return nil, errors.New("forbidden")
}

// DeleteFile soft-deletes a file if authorized and not referenced.
func (s *FileService) DeleteFile(ctx context.Context, userID int64, roles []string, id int64) error {
	f, err := s.store.GetFileByID(ctx, id)
	if err != nil {
		return err
	}

	if f.UploadedBy != userID && !auth.HasRole(roles, "admin") {
		return errors.New("forbidden")
	}

	count, err := s.store.FileReferencedCount(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrFileReferenced
	}

	// Delete from object storage first
	if s.storeS != nil {
		if err := s.storeS.Delete(ctx, f.StorageKey); err != nil {
			return err
		}
	}

	return s.store.DeleteFile(ctx, id)
}
