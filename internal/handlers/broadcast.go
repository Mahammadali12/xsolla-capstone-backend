package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// BroadcastService is what the announcement routes need.
type BroadcastService interface {
	Send(ctx context.Context, adminID int64, message, link, audience string) (*store.Broadcast, error)
	List(ctx context.Context) ([]store.Broadcast, error)
}

// BroadcastHandlers serves the admin's announcements.
type BroadcastHandlers struct {
	svc BroadcastService
}

// NewBroadcastHandlers creates the announcement handlers.
func NewBroadcastHandlers(svc BroadcastService) *BroadcastHandlers {
	return &BroadcastHandlers{svc: svc}
}

// SendBroadcastRequest is the body of POST /notifications/broadcast.
type SendBroadcastRequest struct {
	Message  string `json:"message"`  // 1 to 500 characters
	Link     string `json:"link"`     // an LMS page such as /calendar, or ""
	Audience string `json:"audience"` // all, students, mentors or lecturers
}

// BroadcastResponse is one announcement.
type BroadcastResponse struct {
	ID         int64     `json:"id"`
	Message    string    `json:"message"`
	Link       string    `json:"link"`
	Audience   string    `json:"audience"`
	SentAt     time.Time `json:"sent_at"`
	SentBy     string    `json:"sent_by"`    // the admin's name ("" once their account is deleted)
	Recipients int       `json:"recipients"` // how many people it was sent to
}

func toBroadcastResponse(b store.Broadcast) BroadcastResponse {
	return BroadcastResponse{ID: b.ID, Message: b.Message, Link: b.Link, Audience: b.Audience, SentAt: b.SentAt,
		SentBy: b.SentByName, Recipients: b.Recipients}
}

// @Summary     Send an announcement
// @Description The admin sends a short message to everyone (students, mentors and lecturers) or to one group. It
// @Description reaches each of them as an in-app notification (GET /notifications, type announcement.created) and
// @Description an email. Admins are not in any audience. With nobody in the audience it is saved with 0 recipients.
// @Tags        notifications
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       request body handlers.SendBroadcastRequest true "message, link and audience"
// @Success     201 {object} handlers.BroadcastResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     422 {object} handlers.ErrorResponse "every problem, in details"
// @Router      /notifications/broadcast [post]
func (h *BroadcastHandlers) Send(w http.ResponseWriter, r *http.Request) {
	var req SendBroadcastRequest
	if !decodeReportBody(w, r, &req) {
		return
	}
	b, err := h.svc.Send(r.Context(), middleware.CtxUserID(r.Context()), req.Message, req.Link, req.Audience)
	if err != nil {
		var invalid *service.BroadcastInvalidError
		if errors.As(err, &invalid) {
			details := make([]ErrorDetail, len(invalid.Fields))
			for i, f := range invalid.Fields {
				details[i] = ErrorDetail{Field: f.Field, Message: f.Message}
			}
			writeError(w, http.StatusUnprocessableEntity, "validation_failed", invalid.Error(), details)
			return
		}
		writeError(w, http.StatusInternalServerError, "server_error", "could not send the announcement", nil)
		return
	}
	writeJSON(w, http.StatusCreated, toBroadcastResponse(*b))
}

// @Summary     List announcements
// @Description Every announcement sent, newest first (admin only).
// @Tags        notifications
// @Produce     json
// @Security    BearerAuth
// @Success     200 {array} handlers.BroadcastResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /notifications/broadcasts [get]
func (h *BroadcastHandlers) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list announcements", nil)
		return
	}
	resp := make([]BroadcastResponse, len(list))
	for i, b := range list {
		resp[i] = toBroadcastResponse(b)
	}
	writeJSON(w, http.StatusOK, resp)
}
