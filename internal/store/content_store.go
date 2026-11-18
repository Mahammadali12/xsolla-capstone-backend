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
	Url         *string
	CreatedBy   *int64
	CreatedAt   time.Time
	DueAt       *time.Time

	// Set only for a student whose deadline follows their 1:1: TaskDueAt is the deadline the lecturer set, DueAt is
	// the student's own and DueFrom says why (service.DueFromOneOnOne or service.DueFromNoOneOnOne).
	TaskDueAt *time.Time
	DueFrom   string

	// For a file material: the uploaded file's name and type (from the files table), so the page shows what it is.
	FileName     *string
	FileMimeType *string
}

// Assignment represents an assignment extension of a content item.
type Assignment struct {
	ContentItemID int64
	DueAt         *time.Time
}

// CreateContentItem inserts a new content item and its outbox event in one transaction.
func (s *StoreImpl) CreateContentItem(ctx context.Context, moduleID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64, url *string) (*ContentItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := WithTx(tx)

	var ci ContentItem
	err = q.CreateContentItem(ctx, moduleID, parentID, itemType, title, description, position, isVisible, fileID, url).Scan(
		&ci.ID, &ci.ModuleID, &ci.ParentID, &ci.Type, &ci.Title, &ci.Description, &ci.Position, &ci.IsVisible, &ci.FileID, &ci.Url, &ci.CreatedBy, &ci.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Hidden items are drafts; students cannot see them, so they are not announced.
	if isVisible {
		var moduleTitle string
		if err := q.QueryRowContext(ctx,
			`SELECT title FROM modules WHERE id = $1 AND deleted_at IS NULL`, moduleID,
		).Scan(&moduleTitle); err != nil {
			return nil, err
		}
		recipients, err := q.enrolledStudentRecipients(ctx, moduleID)
		if err != nil {
			return nil, err
		}
		payload, err := contentItemUploadedData(ci, moduleTitle, recipients)
		if err != nil {
			return nil, err
		}
		if err := q.InsertOutboxEvent(ctx, ContentItemUploadedEventType, payload); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ci, nil
}

// GetContentItemByID returns a content item by ID if it exists and is not deleted.
func (s *StoreImpl) GetContentItemByID(ctx context.Context, id int64) (*ContentItem, error) {
	var ci ContentItem
	err := s.DB().GetContentItemByID(ctx, id).Scan(
		&ci.ID, &ci.ModuleID, &ci.ParentID, &ci.Type, &ci.Title, &ci.Description, &ci.Position, &ci.IsVisible, &ci.FileID, &ci.Url, &ci.CreatedBy, &ci.CreatedAt,
		&ci.DueAt,
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
		if err := rows.Scan(&ci.ID, &ci.ModuleID, &ci.ParentID, &ci.Type, &ci.Title, &ci.Description, &ci.Position, &ci.IsVisible, &ci.FileID, &ci.Url, &ci.CreatedBy, &ci.CreatedAt, &ci.DueAt); err != nil {
			return nil, err
		}
		items = append(items, ci)
	}
	return items, nil
}

// UpdateContentItem updates a content item.
func (s *StoreImpl) UpdateContentItem(ctx context.Context, id int64, title *string, description *string, position *int, isVisible *bool, parentID *int64, fileID *int64, dueAt *time.Time, url *string) (*ContentItem, error) {
	var ci ContentItem
	err := s.DB().UpdateContentItem(ctx, id, title, description, position, isVisible, parentID, fileID, dueAt, url).Scan(
		&ci.ID, &ci.ModuleID, &ci.ParentID, &ci.Type, &ci.Title, &ci.Description, &ci.Position, &ci.IsVisible, &ci.FileID, &ci.Url, &ci.CreatedBy, &ci.CreatedAt,
		&ci.DueAt,
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
func (s *StoreImpl) CreateAssignment(ctx context.Context, contentItemID int64, dueAt *time.Time) (*Assignment, error) {
	var a Assignment
	err := s.DB().CreateAssignment(ctx, contentItemID, dueAt).Scan(&a.ContentItemID, &a.DueAt)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetAssignmentByContentItemID returns the assignment for a content item.
func (s *StoreImpl) GetAssignmentByContentItemID(ctx context.Context, contentItemID int64) (*Assignment, error) {
	var a Assignment
	err := s.DB().GetAssignmentByContentItemID(ctx, contentItemID).Scan(&a.ContentItemID, &a.DueAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrContentItemNotFound
		}
		return nil, err
	}
	return &a, nil
}

// UpdateAssignment updates an assignment's due_at.
func (s *StoreImpl) UpdateAssignment(ctx context.Context, contentItemID int64, dueAt *time.Time) (*Assignment, error) {
	var a Assignment
	err := s.DB().UpdateAssignment(ctx, contentItemID, dueAt).Scan(&a.ContentItemID, &a.DueAt)
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
