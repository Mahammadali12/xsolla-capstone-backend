package service

import (
	"context"
	"errors"
	"log"
	"time"

	"xsolla-capstone-api/internal/store"
)

// ContentService coordinates content item and assignment operations.
type ContentService struct {
	store store.Store
}

// ErrUnsupportedContentType is returned when an unsupported content item type is used.
var ErrUnsupportedContentType = errors.New("unsupported content item type")

// ErrAssignmentMissingDueAt is returned when an assignment is created without a due_at.
var ErrAssignmentMissingDueAt = errors.New("assignment requires a due_at")

// NewContentService creates a ContentService backed by the given store.
func NewContentService(s store.Store) *ContentService {
	return &ContentService{store: s}
}

// CreateContentItem creates a new content item.
// Only file and assignment types are supported (sections are removed).
// Assignments require a non-nil due_at.
func (s *ContentService) CreateContentItem(ctx context.Context, userID int64, role string, moduleID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64, dueAt *time.Time, maxPoints int) (*store.ContentItem, error) {
	// Only teacher or admin can create content items
	if role != "teacher" && role != "admin" {
		return nil, store.ErrNotModuleTeacher
	}

	// Only file and assignment types are supported (sections removed)
	if itemType == "section" {
		return nil, ErrUnsupportedContentType
	}

	// Teacher must be enrolled in/teach the module
	if role == "teacher" {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotModuleTeacher
		}
	}

	// If parent_id is set, validate it belongs to the same module and is a section (not supported anymore — sections removed)
	if parentID != nil {
		return nil, errors.New("parent_id is not supported; sections have been removed")
	}

	// Assignments require a due_at
	if itemType == "assignment" && dueAt == nil {
		return nil, ErrAssignmentMissingDueAt
	}

	ci, err := s.store.CreateContentItem(ctx, moduleID, parentID, itemType, title, description, position, isVisible, fileID)
	if err != nil {
		log.Printf("content service: CreateContentItem store error: %v", err)
		return nil, err
	}

	// If type is assignment, create the assignment extension
	if itemType == "assignment" {
		if _, err := s.store.CreateAssignment(ctx, ci.ID, dueAt, maxPoints); err != nil {
			return nil, err
		}
	}

	return ci, nil
}

// GetContentItemByID returns a content item if the user is authorized to view it.
func (s *ContentService) GetContentItemByID(ctx context.Context, userID int64, role string, itemID int64) (*store.ContentItem, error) {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return nil, err
	}

	if role == "admin" {
		return ci, nil
	}

	enrolled, err := s.store.IsUserEnrolled(ctx, ci.ModuleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, store.ErrNotEnrolled
	}

	// If item is not visible, only teacher/admin can see it
	if !ci.IsVisible && role != "admin" {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotEnrolled
		}
	}

	return ci, nil
}

// GetContentItemsForModule returns the content tree for a module.
func (s *ContentService) GetContentItemsForModule(ctx context.Context, userID int64, role string, moduleID int64) ([]store.ContentItem, error) {
	if role == "admin" {
		return s.store.GetContentItemsForModule(ctx, moduleID, false)
	}

	enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotEnrolled
		}
	}

	return s.store.GetContentItemsForModule(ctx, moduleID, true)
}

// UpdateContentItem updates a content item if the user is authorized.
func (s *ContentService) UpdateContentItem(ctx context.Context, userID int64, role string, itemID int64, title string, description string, position *int, isVisible *bool, parentID *int64, fileID *int64) (*store.ContentItem, error) {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return nil, err
	}

	if role != "admin" {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotModuleTeacher
		}
	}

	return s.store.UpdateContentItem(ctx, itemID, title, description, position, isVisible, parentID, fileID)
}

// DeleteContentItem soft-deletes a content item if the user is authorized.
func (s *ContentService) DeleteContentItem(ctx context.Context, userID int64, role string, itemID int64) error {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return err
	}

	if role != "admin" {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if err != nil {
			return err
		}
		if !teacher {
			return store.ErrNotModuleTeacher
		}
	}

	return s.store.DeleteContentItem(ctx, itemID)
}

// GetAssignmentByContentItemID returns the assignment for a content item.
func (s *ContentService) GetAssignmentByContentItemID(ctx context.Context, userID int64, role string, contentItemID int64) (*store.Assignment, error) {
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		return nil, err
	}

	if role != "admin" {
		enrolled, err := s.store.IsUserEnrolled(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !enrolled {
			teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
			if err != nil {
				return nil, err
			}
			if !teacher {
				return nil, store.ErrNotEnrolled
			}
		}
	}

	return s.store.GetAssignmentByContentItemID(ctx, contentItemID)
}

// UpdateAssignment updates an assignment if the user is authorized.
func (s *ContentService) UpdateAssignment(ctx context.Context, userID int64, role string, contentItemID int64, dueAt *time.Time, maxPoints int) (*store.Assignment, error) {
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		return nil, err
	}

	if role != "admin" {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotModuleTeacher
		}
	}

	return s.store.UpdateAssignment(ctx, contentItemID, dueAt, maxPoints)
}
