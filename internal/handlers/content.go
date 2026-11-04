package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

type ContentHandlers struct {
	svc *service.ContentService
}

// NewContentHandlers creates content handlers backed by the given service.
func NewContentHandlers(svc *service.ContentService) *ContentHandlers {
	return &ContentHandlers{svc: svc}
}

// GET /courses/:courseId/content-items
func (h *ContentHandlers) ListContentItems(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	courseID, err := strconv.ParseInt(r.PathValue("courseId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid course ID", nil)
		return
	}

	items, err := h.svc.GetContentItemsForCourse(r.Context(), userID, role, courseID)
	if err != nil {
		log.Printf("content handler: ListContentItems error: %v", err)
		if errors.Is(err, store.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "not enrolled in this course", nil)
		} else {
			writeError(w, 500, "server_error", "could not list content items", nil)
		}
		return
	}

	writeJSON(w, 200, buildContentTree(items))
}

// POST /courses/:courseId/content-items
func (h *ContentHandlers) CreateContentItem(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	courseID, err := strconv.ParseInt(r.PathValue("courseId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid course ID", nil)
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

	ci, err := h.svc.CreateContentItem(r.Context(), userID, role, courseID, req.ParentID, req.Type, req.Title, req.Description, position, isVisible, req.FileID)
	if err != nil {
		log.Printf("content handler: CreateContentItem error: %v", err)
		if errors.Is(err, store.ErrNotCourseTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this course", nil)
		} else if errors.Is(err, store.ErrContentItemNotFound) {
			writeError(w, 404, "not_found", "parent item not found", nil)
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
	})
}

// GET /content-items/:id
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
			writeError(w, 403, "forbidden", "not enrolled in this course", nil)
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
	})
}

// PATCH /content-items/:id
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
		if errors.Is(err, store.ErrNotCourseTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this course", nil)
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
	})
}

// DELETE /content-items/:id
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
		if errors.Is(err, store.ErrNotCourseTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this course", nil)
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
			Children:    []dto.ContentTreeNode{},
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
				parent.Children = append(parent.Children, *node)
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
