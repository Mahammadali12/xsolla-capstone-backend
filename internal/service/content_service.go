package service

import (
	"context"
	"errors"
	"log"
	"time"

	"xsolla-capstone-api/internal/auth"
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
// Only file, assignment, and link types are supported (sections are removed).
// Assignments require a non-nil due_at. Links require a non-nil url.
func (s *ContentService) CreateContentItem(ctx context.Context, userID int64, roles []string, moduleID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64, dueAt *time.Time, maxPoints int, url *string) (*store.ContentItem, error) {
	// Only lecturer or admin can create content items
	if !auth.HasRole(roles, "lecturer") && !auth.HasRole(roles, "admin") {
		return nil, store.ErrNotModuleTeacher
	}

	// Only file, assignment, and link types are supported (sections removed)
	if itemType == "section" {
		return nil, ErrUnsupportedContentType
	}

	// Teacher must be enrolled in/teach the module
	if auth.HasRole(roles, "lecturer") {
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

	// Type-specific validation
	switch itemType {
	case "assignment":
		if dueAt == nil {
			return nil, ErrAssignmentMissingDueAt
		}
	case "link":
		if url == nil || *url == "" {
			return nil, errors.New("link requires a url")
		}
	}

	ci, err := s.store.CreateContentItem(ctx, moduleID, parentID, itemType, title, description, position, isVisible, fileID, url)
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
func (s *ContentService) GetContentItemByID(ctx context.Context, userID int64, roles []string, itemID int64) (*store.ContentItem, error) {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return nil, err
	}

	if auth.HasRole(roles, "admin") {
		return ci, nil
	}

	enrolled, err := s.store.IsUserEnrolled(ctx, ci.ModuleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, store.ErrNotEnrolled
	}

	// If item is not visible, only admin can see it
	if !ci.IsVisible && !auth.HasRole(roles, "admin") {
		return nil, store.ErrNotEnrolled
	}

	return ci, nil
}

// GetContentItemsForModule returns the content tree for a module.
func (s *ContentService) GetContentItemsForModule(ctx context.Context, userID int64, roles []string, moduleID int64) ([]store.ContentItem, error) {
	if auth.HasRole(roles, "admin") {
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
// If the item is an assignment and due_at is provided, the assignment's
// deadline is updated too. If the item is a link and url is provided, the link
// url is updated too.
func (s *ContentService) UpdateContentItem(ctx context.Context, userID int64, roles []string, itemID int64, title string, description string, position *int, isVisible *bool, parentID *int64, fileID *int64, dueAt *time.Time, url *string) (*store.ContentItem, error) {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return nil, err
	}

	if !auth.HasRole(roles, "admin") {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotModuleTeacher
		}
	}

	if ci.Type == "assignment" && dueAt != nil {
		if _, err := s.store.UpdateAssignment(ctx, itemID, dueAt, 0); err != nil {
			return nil, err
		}
	}

	return s.store.UpdateContentItem(ctx, itemID, title, description, position, isVisible, parentID, fileID, dueAt, url)
}

// DeleteContentItem soft-deletes a content item if the user is authorized.
func (s *ContentService) DeleteContentItem(ctx context.Context, userID int64, roles []string, itemID int64) error {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return err
	}

	if !auth.HasRole(roles, "admin") {
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
func (s *ContentService) GetAssignmentByContentItemID(ctx context.Context, userID int64, roles []string, contentItemID int64) (*store.Assignment, error) {
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		return nil, err
	}

	if !auth.HasRole(roles, "admin") {
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
func (s *ContentService) UpdateAssignment(ctx context.Context, userID int64, roles []string, contentItemID int64, dueAt *time.Time, maxPoints int) (*store.Assignment, error) {
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		return nil, err
	}

	if !auth.HasRole(roles, "admin") {
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
