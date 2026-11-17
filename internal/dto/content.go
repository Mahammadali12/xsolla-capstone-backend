package dto

import "time"

// CreateContentItemRequest is the payload for POST /modules/:moduleId/content-items.
type CreateContentItemRequest struct {
	ParentID    *int64   `json:"parent_id"`
	Type        string   `json:"type"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Position    *int     `json:"position"`
	IsVisible   *bool    `json:"is_visible"`
	FileID      *int64   `json:"file_id"`
	DueAt       *time.Time `json:"due_at"`
	Url         *string  `json:"url,omitempty"`
}

// UpdateContentItemRequest is the payload for PATCH /content-items/:id.
type UpdateContentItemRequest struct {
	// nil = not sent: the stored value stays
	Title       *string    `json:"title,omitempty"`
	Description *string    `json:"description,omitempty"`
	Position    *int       `json:"position,omitempty"`
	IsVisible   *bool      `json:"is_visible,omitempty"`
	ParentID    *int64     `json:"parent_id,omitempty"`
	FileID      *int64     `json:"file_id,omitempty"`
	DueAt       *time.Time `json:"due_at,omitempty"`
	Url         *string    `json:"url,omitempty"`
}

// ContentItemResponse is the content item object returned in responses.
type ContentItemResponse struct {
	ID          int64       `json:"id"`
	Type        string      `json:"type"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Position    int         `json:"position"`
	IsVisible   bool        `json:"is_visible"`
	FileID      *int64      `json:"file_id"`
	ParentID    *int64      `json:"parent_id"`
	CreatedAt   time.Time   `json:"created_at"`
	DueAt       *time.Time  `json:"due_at,omitempty"`
	Url         *string     `json:"url,omitempty"`
	Children    []ContentItemResponse `json:"children,omitempty"`

	// For a student whose deadline follows their 1:1: due_at is their own deadline, task_due_at the one the
	// lecturer set, and due_from says why ("one_on_one" or "no_one_on_one"). Left out otherwise.
	TaskDueAt *time.Time `json:"task_due_at,omitempty"`
	DueFrom   string     `json:"due_from,omitempty"`
}

// AssignmentResponse is the assignment object returned in responses.
type AssignmentResponse struct {
	ContentItemID int64     `json:"content_item_id"`
	DueAt         *time.Time `json:"due_at"`
}

// ContentTreeResponse is the tree response for GET /modules/:moduleId/content-items.
type ContentTreeResponse []ContentTreeNode

type ContentTreeNode struct {
	ID          int64                `json:"id"`
	Type        string               `json:"type"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Position    int                  `json:"position"`
	IsVisible   bool                 `json:"is_visible"`
	FileID      *int64               `json:"file_id"`
	Url         *string              `json:"url,omitempty"`
	DueAt       *time.Time           `json:"due_at,omitempty"`
	Children    []*ContentTreeNode   `json:"children,omitempty"`

	// Same as on ContentItemResponse.
	TaskDueAt *time.Time `json:"task_due_at,omitempty"`
	DueFrom   string     `json:"due_from,omitempty"`
}
