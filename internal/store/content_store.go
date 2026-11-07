package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	// ErrContentItemNotFound is returned when a content item does not exist or is deleted.
	ErrContentItemNotFound = errors.New("content item not found")
	// ErrParentMustBeSection is returned when a parent item is not a section.
	ErrParentMustBeSection = errors.New("parent item must be a section")
	// ErrParentWrongModule is returned when a parent item belongs to a different module.
	ErrParentWrongModule = errors.New("parent item belongs to a different module")
)

// ContentItem represents a content item row from the database.
type ContentItem struct {
	ID          int64
	ModuleID    int64
	ParentID    *int64
	Type        string
	Title       string
	Description string
	Position    int
	IsVisible   bool
	FileID      *int64
	CreatedAt   time.Time
	DueAt       *time.Time
	MaxPoints   int
}

// Assignment represents an assignment extension of a content item.
type Assignment struct {
	ContentItemID int64
	DueAt         *time.Time
	MaxPoints     int
}

// CreateContentItem inserts a new content item.
func (s *StoreImpl) CreateContentItem(ctx context.Context, moduleID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64) (*ContentItem, error) {
	var ci ContentItem
	err := s.DB().CreateContentItem(ctx, moduleID, parentID, itemType, title, description, position, isVisible, fileID).Scan(
		&ci.ID, &ci.ModuleID, &ci.ParentID, &ci.Type, &ci.Title, &ci.Description, &ci.Position, &ci.IsVisible, &ci.FileID, &ci.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &ci, nil
}

// GetContentItemByID returns a content item by ID if it exists and is not deleted.
func (s *StoreImpl) GetContentItemByID(ctx context.Context, id int64) (*ContentItem, error) {
	var ci ContentItem
	err := s.DB().GetContentItemByID(ctx, id).Scan(
		&ci.ID, &ci.ModuleID, &ci.ParentID, &ci.Type, &ci.Title, &ci.Description, &ci.Position, &ci.IsVisible, &ci.FileID, &ci.CreatedAt,
		&ci.DueAt, &ci.MaxPoints,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrContentItemNotFound
		}
		return nil, err
	}
	return &ci, nil
}

// GetContentItemsForModule returns all content items for a module.
func (s *StoreImpl) GetContentItemsForModule(ctx context.Context, moduleID int64, visibleOnly bool) ([]ContentItem, error) {
	rows, err := s.DB().GetContentItemsForModule(ctx, moduleID, visibleOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ContentItem
	for rows.Next() {
		var ci ContentItem
		if err := rows.Scan(&ci.ID, &ci.ModuleID, &ci.ParentID, &ci.Type, &ci.Title, &ci.Description, &ci.Position, &ci.IsVisible, &ci.FileID, &ci.CreatedAt, &ci.DueAt, &ci.MaxPoints); err != nil {
			return nil, err
		}
		items = append(items, ci)
	}
	return items, nil
}

// UpdateContentItem updates a content item.
func (s *StoreImpl) UpdateContentItem(ctx context.Context, id int64, title string, description string, position *int, isVisible *bool, parentID *int64, fileID *int64, dueAt *time.Time) (*ContentItem, error) {
	var ci ContentItem
	err := s.DB().UpdateContentItem(ctx, id, title, description, position, isVisible, parentID, fileID, dueAt).Scan(
		&ci.ID, &ci.ModuleID, &ci.ParentID, &ci.Type, &ci.Title, &ci.Description, &ci.Position, &ci.IsVisible, &ci.FileID, &ci.CreatedAt,
		&ci.DueAt, &ci.MaxPoints,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrContentItemNotFound
		}
		return nil, err
	}
	return &ci, nil
}

// DeleteContentItem soft-deletes a content item.
func (s *StoreImpl) DeleteContentItem(ctx context.Context, id int64) error {
	result, err := s.DB().DeleteContentItem(ctx, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrContentItemNotFound
	}
	return nil
}

// CreateAssignment creates an assignment extension for a content item.
func (s *StoreImpl) CreateAssignment(ctx context.Context, contentItemID int64, dueAt *time.Time, maxPoints int) (*Assignment, error) {
	var a Assignment
	err := s.DB().CreateAssignment(ctx, contentItemID, dueAt, maxPoints).Scan(&a.ContentItemID, &a.DueAt, &a.MaxPoints)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAssignmentByContentItemID returns the assignment for a content item.
func (s *StoreImpl) GetAssignmentByContentItemID(ctx context.Context, contentItemID int64) (*Assignment, error) {
	var a Assignment
	err := s.DB().GetAssignmentByContentItemID(ctx, contentItemID).Scan(&a.ContentItemID, &a.DueAt, &a.MaxPoints)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrContentItemNotFound
		}
		return nil, err
	}
	return &a, nil
}

// UpdateAssignment updates an assignment's due_at and max_points.
func (s *StoreImpl) UpdateAssignment(ctx context.Context, contentItemID int64, dueAt *time.Time, maxPoints int) (*Assignment, error) {
	var a Assignment
	err := s.DB().UpdateAssignment(ctx, contentItemID, dueAt, maxPoints).Scan(&a.ContentItemID, &a.DueAt, &a.MaxPoints)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrContentItemNotFound
		}
		return nil, err
	}
	return &a, nil
}

// DeleteAssignment soft-deletes an assignment.
func (s *StoreImpl) DeleteAssignment(ctx context.Context, contentItemID int64) error {
	result, err := s.DB().DeleteAssignment(ctx, contentItemID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrContentItemNotFound
	}
	return nil
}
