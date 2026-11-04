package dto

import "time"

// CreateContentItemRequest is the payload for POST /courses/:courseId/content-items.
type CreateContentItemRequest struct {
	ParentID    *int64  `json:"parent_id"`
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Position    *int    `json:"position"`
	IsVisible   *bool   `json:"is_visible"`
	FileID      *int64  `json:"file_id"`
}

// UpdateContentItemRequest is the payload for PATCH /content-items/:id.
type UpdateContentItemRequest struct {
	Title       string  `json:"title,omitempty"`
	Description string  `json:"description,omitempty"`
	Position    *int    `json:"position,omitempty"`
	IsVisible   *bool   `json:"is_visible,omitempty"`
	ParentID    *int64  `json:"parent_id,omitempty"`
	FileID      *int64  `json:"file_id,omitempty"`
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
	Children    []ContentItemResponse `json:"children,omitempty"`
}

// AssignmentResponse is the assignment object returned in responses.
type AssignmentResponse struct {
	ContentItemID int64     `json:"content_item_id"`
	DueAt         *time.Time `json:"due_at"`
	MaxPoints     int       `json:"max_points"`
}

// ContentTreeResponse is the tree response for GET /courses/:courseId/content-items.
type ContentTreeResponse []ContentTreeNode

type ContentTreeNode struct {
	ID          int64                `json:"id"`
	Type        string               `json:"type"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Position    int                  `json:"position"`
	IsVisible   bool                 `json:"is_visible"`
	FileID      *int64               `json:"file_id"`
	DueAt       *time.Time           `json:"due_at,omitempty"`
	MaxPoints   int                  `json:"max_points,omitempty"`
	Children    []ContentTreeNode    `json:"children,omitempty"`
}
