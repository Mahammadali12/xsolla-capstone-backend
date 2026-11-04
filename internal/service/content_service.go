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
	store *store.Store
}

// NewContentService creates a ContentService backed by the given store.
func NewContentService(s *store.Store) *ContentService {
	return &ContentService{store: s}
}

// CreateContentItem creates a new content item.
func (s *ContentService) CreateContentItem(ctx context.Context, userID int64, role string, courseID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64) (*store.ContentItem, error) {
	// Only teacher or admin can create content items
	if role != "teacher" && role != "admin" {
		return nil, store.ErrNotCourseTeacher
	}

	// Teacher must be enrolled in/teach the course
	if role == "teacher" {
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, courseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotCourseTeacher
		}
	}

	// If parent_id is set, validate it belongs to the same course and is a section
	if parentID != nil {
		parent, err := s.store.GetContentItemByID(ctx, *parentID)
		if err != nil {
			if errors.Is(err, store.ErrContentItemNotFound) {
				return nil, store.ErrContentItemNotFound
			}
			return nil, err
		}
		if parent.CourseID != courseID {
			return nil, errors.New("parent item belongs to a different course")
		}
		if parent.Type != "section" {
			return nil, errors.New("parent item must be a section")
		}
	}

	ci, err := s.store.CreateContentItem(ctx, courseID, parentID, itemType, title, description, position, isVisible, fileID)
	if err != nil {
		log.Printf("content service: CreateContentItem store error: %v", err)
		return nil, err
	}

	// If type is assignment, create the assignment extension
	if itemType == "assignment" {
		if _, err := s.store.CreateAssignment(ctx, ci.ID, nil, 0); err != nil {
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

	enrolled, err := s.store.IsUserEnrolled(ctx, ci.CourseID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, store.ErrNotEnrolled
	}

	// If item is not visible, only teacher/admin can see it
	if !ci.IsVisible && role != "admin" {
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotEnrolled
		}
	}

	return ci, nil
}

// GetContentItemsForCourse returns the content tree for a course.
func (s *ContentService) GetContentItemsForCourse(ctx context.Context, userID int64, role string, courseID int64) ([]store.ContentItem, error) {
	if role == "admin" {
		return s.store.GetContentItemsForCourse(ctx, courseID, false)
	}

	enrolled, err := s.store.IsUserEnrolled(ctx, courseID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, courseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotEnrolled
		}
	}

	return s.store.GetContentItemsForCourse(ctx, courseID, true)
}

// UpdateContentItem updates a content item if the user is authorized.
func (s *ContentService) UpdateContentItem(ctx context.Context, userID int64, role string, itemID int64, title string, description string, position *int, isVisible *bool, parentID *int64, fileID *int64) (*store.ContentItem, error) {
	ci, err := s.store.GetContentItemByID(ctx, itemID)
	if err != nil {
		return nil, err
	}

	if role != "admin" {
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotCourseTeacher
		}
	}

	// Validate parent change
	if parentID != nil {
		if ci.ParentID == nil || *parentID != *ci.ParentID {
			if ci.Type != "section" {
				return nil, errors.New("only sections can have children")
			}
			parent, err := s.store.GetContentItemByID(ctx, *parentID)
			if err != nil {
				if errors.Is(err, store.ErrContentItemNotFound) {
					return nil, store.ErrContentItemNotFound
				}
				return nil, err
			}
			if parent.CourseID != ci.CourseID {
				return nil, errors.New("parent item belongs to a different course")
			}
			if parent.Type != "section" {
				return nil, errors.New("parent item must be a section")
			}
			// Cycle prevention: parent cannot be a descendant of this item
			if isDescendant(ctx, s.store, ci.ID, *parentID) {
				return nil, errors.New("cannot set parent to a descendant item (cycle)")
			}
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
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
		if err != nil {
			return err
		}
		if !teacher {
			return store.ErrNotCourseTeacher
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
		enrolled, err := s.store.IsUserEnrolled(ctx, ci.CourseID, userID)
		if err != nil {
			return nil, err
		}
		if !enrolled {
			teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
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
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotCourseTeacher
		}
	}

	return s.store.UpdateAssignment(ctx, contentItemID, dueAt, maxPoints)
}

// isDescendant checks if descendantID is a descendant of ancestorID (cycle prevention).
func isDescendant(ctx context.Context, store *store.Store, ancestorID int64, descendantID int64) bool {
	// Walk up from descendantID to see if we reach ancestorID
	currentID := descendantID
	for {
		ci, err := store.GetContentItemByID(ctx, currentID)
		if err != nil {
			return false
		}
		if ci.ParentID == nil {
			return false
		}
		if *ci.ParentID == ancestorID {
			return true
		}
		currentID = *ci.ParentID
	}
}
