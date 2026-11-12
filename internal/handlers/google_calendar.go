package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
)

// GoogleCalendarService is the interface the Google calendar handler depends on.
type GoogleCalendarService interface {
	ListEvents(ctx context.Context, userID int64, from, to time.Time) ([]service.GoogleCalendarEvent, error)
}

type GoogleCalendarHandlers struct {
	svc GoogleCalendarService
}

// NewGoogleCalendarHandlers creates Google calendar handlers backed by the given service.
func NewGoogleCalendarHandlers(svc GoogleCalendarService) *GoogleCalendarHandlers {
	return &GoogleCalendarHandlers{svc: svc}
}

// maxGoogleCalendarRange keeps one request from reading years of history.
const maxGoogleCalendarRange = 366 * 24 * time.Hour

// @Summary     List my Google Calendar events
// @Description All events of the signed-in user's primary Google Calendar in the given range (read-only). Requires the user to have connected Google.
// @Tags        google
// @Produce     json
// @Security    BearerAuth
// @Param       from query string false "Start of the range (RFC 3339). Default: one month ago."
// @Param       to query string false "End of the range (RFC 3339). Default: two months ahead."
// @Success     200 {array} dto.GoogleCalendarEventResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     502 {object} handlers.ErrorResponse
// @Router      /google/calendar/events [get]
func (h *GoogleCalendarHandlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())

	from := time.Now().AddDate(0, -1, 0)
	to := time.Now().AddDate(0, 2, 0)

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		parsed, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			writeError(w, 400, "bad_request", "invalid 'from' parameter", nil)
			return
		}
		from = parsed
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		parsed, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			writeError(w, 400, "bad_request", "invalid 'to' parameter", nil)
			return
		}
		to = parsed
	}
	if !to.After(from) {
		writeError(w, 400, "bad_request", "'to' must be after 'from'", nil)
		return
	}
	if to.Sub(from) > maxGoogleCalendarRange {
		writeError(w, 400, "bad_request", "the range must not be longer than 366 days", nil)
		return
	}

	events, err := h.svc.ListEvents(r.Context(), userID, from, to)
	if err != nil {
		log.Printf("google calendar handler: ListEvents error for user %d: %v", userID, err)
		switch {
		case errors.Is(err, service.ErrGoogleNotConnected):
			writeError(w, 404, "google_not_connected", "Google account not connected", nil)
		case errors.Is(err, service.ErrGoogleReconnect):
			writeError(w, 401, "google_token_expired", "Google refresh token expired or revoked", nil)
		case errors.Is(err, service.ErrGoogleCalendarUnavailable):
			writeError(w, 502, "upstream_error", "Could not reach Google Calendar", nil)
		default:
			writeError(w, 500, "server_error", "could not list Google Calendar events", nil)
		}
		return
	}

	resp := make([]dto.GoogleCalendarEventResponse, len(events))
	for i, e := range events {
		resp[i] = dto.GoogleCalendarEventResponse{
			ID:          e.ID,
			Title:       e.Title,
			Description: e.Description,
			Location:    e.Location,
			StartsAt:    e.StartsAt,
			EndsAt:      e.EndsAt,
			AllDay:      e.AllDay,
			MeetingURL:  e.MeetingURL,
			HTMLLink:    e.HTMLLink,
		}
	}
	writeJSON(w, 200, resp)
}
