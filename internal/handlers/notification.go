package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// NotificationService is what the in-app notification routes need.
type NotificationService interface {
	Unread(ctx context.Context, userID int64, limit int) ([]store.Notification, error)
	MarkRead(ctx context.Context, userID, id int64) error
	MarkAllRead(ctx context.Context, userID int64) (int64, error)
}

// NotificationHandlers serves each signed-in user their own in-app notifications.
type NotificationHandlers struct {
	svc NotificationService
}

// NewNotificationHandlers creates the in-app notification handlers.
func NewNotificationHandlers(svc NotificationService) *NotificationHandlers {
	return &NotificationHandlers{svc: svc}
}

// writeNotificationError answers the errors every notification route shares.
func writeNotificationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNotificationsUnavailable):
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", err.Error(), nil)
	case errors.Is(err, service.ErrNotificationNotFound):
		writeError(w, http.StatusNotFound, "not_found", "notification not found", nil)
	default:
		writeError(w, http.StatusInternalServerError, "server_error", "could not reach the notifications", nil)
	}
}

// @Summary     My unread notifications
// @Description The signed-in user's unread in-app notifications, newest first. They are made by the notification
// @Description service from the same events, policies and templates as the emails. Read ones are not listed.
// @Tags        notifications
// @Produce     json
// @Security    BearerAuth
// @Param       limit query int false "How many, 1 to 100 (default 50)"
// @Success     200 {array} dto.NotificationResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     503 {object} handlers.ErrorResponse "NOTIFY_DATABASE_URL is not set"
// @Router      /notifications [get]
func (h *NotificationHandlers) Unread(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be a number from 1 to 100", nil)
			return
		}
		limit = n
	}

	list, err := h.svc.Unread(r.Context(), middleware.CtxUserID(r.Context()), limit)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	resp := make([]dto.NotificationResponse, len(list))
	for i, n := range list {
		resp[i] = dto.NotificationResponse{
			ID:        n.ID,
			Type:      n.EventType,
			Title:     n.Title,
			Message:   n.Body,
			CreatedAt: n.CreatedAt,
			ReadAt:    n.ReadAt,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary     Mark a notification read
// @Description Marks one of the signed-in user's notifications read, so it is no longer listed. Marking it again is
// @Description fine. Someone else's notification answers 404.
// @Tags        notifications
// @Security    BearerAuth
// @Param       id path int true "Notification ID"
// @Success     204
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     503 {object} handlers.ErrorResponse
// @Router      /notifications/{id}/read [post]
func (h *NotificationHandlers) MarkRead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid notification ID", nil)
		return
	}
	if err := h.svc.MarkRead(r.Context(), middleware.CtxUserID(r.Context()), id); err != nil {
		writeNotificationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// @Summary     Mark all notifications read
// @Description Marks every unread notification of the signed-in user read.
// @Tags        notifications
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} dto.MarkAllNotificationsReadResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     503 {object} handlers.ErrorResponse
// @Router      /notifications/read-all [post]
func (h *NotificationHandlers) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	n, err := h.svc.MarkAllRead(r.Context(), middleware.CtxUserID(r.Context()))
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MarkAllNotificationsReadResponse{Marked: n})
}
