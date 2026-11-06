package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// ContentService is the interface that content handlers depend on.
type ContentService interface {
	CreateContentItem(ctx context.Context, userID int64, role string, moduleID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64, dueAt *time.Time, maxPoints int) (*store.ContentItem, error)
	GetContentItemByID(ctx context.Context, userID int64, role string, itemID int64) (*store.ContentItem, error)
	GetContentItemsForModule(ctx context.Context, userID int64, role string, moduleID int64) ([]store.ContentItem, error)
	UpdateContentItem(ctx context.Context, userID int64, role string, itemID int64, title string, description string, position *int, isVisible *bool, parentID *int64, fileID *int64) (*store.ContentItem, error)
	DeleteContentItem(ctx context.Context, userID int64, role string, itemID int64) error
}

type ContentHandlers struct {
	svc ContentService
}

// NewContentHandlers creates content handlers backed by the given service.
func NewContentHandlers(svc ContentService) *ContentHandlers {
	return &ContentHandlers{svc: svc}
}

// @Summary     List content items
// @Description Get the content tree for a module (files, assignments)
// @Tags        content-items
// @Produce     json
// @Security    BearerAuth
// @Param       moduleId path int true "Module ID"
// @Success     200 {array} dto.ContentTreeNode
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     500 {object} handlers.ErrorResponse
// @Router      /modules/{moduleId}/content-items [get]
func (h *ContentHandlers) ListContentItems(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	items, err := h.svc.GetContentItemsForModule(r.Context(), userID, role, moduleID)
	if err != nil {
		log.Printf("content handler: ListContentItems error: %v", err)
		if errors.Is(err, store.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "not enrolled in this module", nil)
		} else {
			writeError(w, 500, "server_error", "could not list content items", nil)
		}
		return
	}

	writeJSON(w, 200, buildContentTree(items))
}

// @Summary     Create content item
// @Description Create a file or assignment (teacher or admin only)
// @Tags        content-items
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       moduleId path int true "Module ID"
// @Param       request body dto.CreateContentItemRequest true "Content item details"
// @Success     201 {object} dto.ContentItemResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     500 {object} handlers.ErrorResponse
// @Router      /modules/{moduleId}/content-items [post]
func (h *ContentHandlers) CreateContentItem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	var req dto.CreateContentItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	if req.Type == "" || req.Title == "" {
		writeError(w, 400, "bad_request", "type and title are required", nil)
		return
	}

	position := 0
	if req.Position != nil {
		position = *req.Position
	}
	isVisible := true
	if req.IsVisible != nil {
		isVisible = *req.IsVisible
	}

	var dueAt *time.Time
	if req.DueAt != nil {
		dueAt = req.DueAt
	}

	ci, err := h.svc.CreateContentItem(r.Context(), userID, role, moduleID, req.ParentID, req.Type, req.Title, req.Description, position, isVisible, req.FileID, dueAt, req.MaxPoints)
	if err != nil {
		log.Printf("content handler: CreateContentItem error: %v", err)
		if errors.Is(err, service.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else if errors.Is(err, service.ErrUnsupportedContentType) {
			writeError(w, 400, "bad_request", err.Error(), nil)
		} else if errors.Is(err, service.ErrAssignmentMissingDueAt) {
			writeError(w, 400, "bad_request", err.Error(), nil)
		} else if errors.Is(err, store.ErrContentItemNotFound) || errors.Is(err, service.ErrParentWrongModule) {
			writeError(w, 404, "not_found", "parent item not found", nil)
		} else if errors.Is(err, service.ErrParentMustBeSection) {
			writeError(w, 400, "bad_request", "parent item must be a section", nil)
		} else {
			writeError(w, 500, "server_error", "could not create content item", nil)
		}
		return
	}

	writeJSON(w, 201, dto.ContentItemResponse{
		ID:          ci.ID,
		Type:        ci.Type,
		Title:       ci.Title,
		Description: ci.Description,
		Position:    ci.Position,
		IsVisible:   ci.IsVisible,
		FileID:      ci.FileID,
		ParentID:    ci.ParentID,
		CreatedAt:   ci.CreatedAt,
		DueAt:       ci.DueAt,
		MaxPoints:   ci.MaxPoints,
	})
}

// @Summary     Get content item
// @Description Get a single content item by ID
// @Tags        content-items
// @Produce     json
// @Security    BearerAuth
// @Param       id path int true "Content item ID"
// @Success     200 {object} dto.ContentItemResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /content-items/{id} [get]
func (h *ContentHandlers) GetContentItem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	itemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid content item ID", nil)
		return
	}

	ci, err := h.svc.GetContentItemByID(r.Context(), userID, role, itemID)
	if err != nil {
		log.Printf("content handler: GetContentItem error: %v", err)
		if errors.Is(err, store.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "not enrolled in this module", nil)
		} else if errors.Is(err, store.ErrContentItemNotFound) {
			writeError(w, 404, "not_found", "content item not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not get content item", nil)
		}
		return
	}

	writeJSON(w, 200, dto.ContentItemResponse{
		ID:          ci.ID,
		Type:        ci.Type,
		Title:       ci.Title,
		Description: ci.Description,
		Position:    ci.Position,
		IsVisible:   ci.IsVisible,
		FileID:      ci.FileID,
		ParentID:    ci.ParentID,
		CreatedAt:   ci.CreatedAt,
		DueAt:       ci.DueAt,
		MaxPoints:   ci.MaxPoints,
	})
}

// @Summary     Update content item
// @Description Update content item fields (teacher or admin only)
// @Tags        content-items
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id path int true "Content item ID"
// @Param       request body dto.UpdateContentItemRequest true "Content item updates"
// @Success     200 {object} dto.ContentItemResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /content-items/{id} [patch]
func (h *ContentHandlers) UpdateContentItem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	itemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid content item ID", nil)
		return
	}

	var req dto.UpdateContentItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	position := req.Position
	isVisible := req.IsVisible

	ci, err := h.svc.UpdateContentItem(r.Context(), userID, role, itemID, req.Title, req.Description, position, isVisible, req.ParentID, req.FileID)
	if err != nil {
		log.Printf("content handler: UpdateContentItem error: %v", err)
		if errors.Is(err, store.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else if errors.Is(err, store.ErrContentItemNotFound) {
			writeError(w, 404, "not_found", "content item not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not update content item", nil)
		}
		return
	}

	writeJSON(w, 200, dto.ContentItemResponse{
		ID:          ci.ID,
		Type:        ci.Type,
		Title:       ci.Title,
		Description: ci.Description,
		Position:    ci.Position,
		IsVisible:   ci.IsVisible,
		FileID:      ci.FileID,
		ParentID:    ci.ParentID,
		CreatedAt:   ci.CreatedAt,
		DueAt:       ci.DueAt,
		MaxPoints:   ci.MaxPoints,
	})
}

// @Summary     Delete content item
// @Description Soft-delete a content item (teacher or admin only)
// @Tags        content-items
// @Security    BearerAuth
// @Param       id path int true "Content item ID"
// @Success     204 {string} string "No content"
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /content-items/{id} [delete]
func (h *ContentHandlers) DeleteContentItem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	itemID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid content item ID", nil)
		return
	}

	if err := h.svc.DeleteContentItem(r.Context(), userID, role, itemID); err != nil {
		log.Printf("content handler: DeleteContentItem error: %v", err)
		if errors.Is(err, store.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else if errors.Is(err, store.ErrContentItemNotFound) {
			writeError(w, 404, "not_found", "content item not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not delete content item", nil)
		}
		return
	}

	w.WriteHeader(204)
}

// buildContentTree builds a nested tree from a flat list of content items.
func buildContentTree(items []store.ContentItem) []dto.ContentTreeNode {
	itemMap := make(map[int64]*dto.ContentTreeNode)
	var roots []*dto.ContentTreeNode

	for _, ci := range items {
		node := dto.ContentTreeNode{
			ID:          ci.ID,
			Type:        ci.Type,
			Title:       ci.Title,
			Description: ci.Description,
			Position:    ci.Position,
			IsVisible:   ci.IsVisible,
			FileID:      ci.FileID,
			DueAt:       ci.DueAt,
			MaxPoints:   ci.MaxPoints,
			Children:    []*dto.ContentTreeNode{},
		}
		itemMap[ci.ID] = &node
	}

	for _, ci := range items {
		node := itemMap[ci.ID]
		if ci.ParentID == nil {
			roots = append(roots, node)
		} else {
			parent := itemMap[*ci.ParentID]
			if parent != nil {
				parent.Children = append(parent.Children, node)
			} else {
				// Parent not in list (e.g. hidden), treat as root
				roots = append(roots, node)
			}
		}
	}

	// Convert pointer slice to value slice for JSON serialization.
	result := make([]dto.ContentTreeNode, len(roots))
	for i, node := range roots {
		result[i] = *node
	}
	return result
}
