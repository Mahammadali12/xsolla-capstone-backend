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

// EventService is the interface that event handlers depend on.
type EventService interface {
	ListEvents(ctx context.Context, from time.Time, to time.Time, userID int64, role string) ([]service.Event, error)
	GetEventByID(ctx context.Context, userID int64, role string, id int64) (*service.Event, error)
	CreateEvent(ctx context.Context, userID int64, role string, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64) (*service.Event, error)
	UpdateEvent(ctx context.Context, userID int64, role string, id int64, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64) (*service.Event, error)
	DeleteEvent(ctx context.Context, userID int64, role string, id int64) error
}

type EventHandlers struct {
	svc EventService
}

// NewEventHandlers creates event handlers backed by the given service.
func NewEventHandlers(svc EventService) *EventHandlers {
	return &EventHandlers{svc: svc}
}

// GET /events?from=&to=
func (h *EventHandlers) ListEvents(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	var from, to time.Time
	var err error
	if fromStr != "" {
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			writeError(w, 400, "bad_request", "invalid 'from' parameter", nil)
			return
		}
	} else {
		from = time.Now().AddDate(0, -1, 0)
	}
	if toStr != "" {
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			writeError(w, 400, "bad_request", "invalid 'to' parameter", nil)
			return
		}
	} else {
		to = time.Now().AddDate(0, 2, 0)
	}

	events, err := h.svc.ListEvents(r.Context(), from, to, userID, role)
	if err != nil {
		log.Printf("event handler: ListEvents error: %v", err)
		writeError(w, 500, "server_error", "could not list events", nil)
		return
	}

	resp := make([]dto.EventResponse, len(events))
	for i, e := range events {
		resp[i] = dto.EventResponse{
			ID:          e.ID,
			Title:       e.Title,
			Kind:        e.Kind,
			StartsAt:    e.StartsAt,
			EndsAt:      e.EndsAt,
			AllDay:      e.AllDay,
			Location:    e.Location,
			Description: e.Description,
			ModuleID:    e.ModuleID,
			CreatedAt:   e.CreatedAt,
		}
	}
	writeJSON(w, 200, resp)
}

// POST /events
func (h *EventHandlers) CreateEvent(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	var req dto.CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	var startsAt, endsAt time.Time
	var err error
	if req.StartsAt != "" {
		startsAt, err = time.Parse(time.RFC3339, req.StartsAt)
		if err != nil {
			writeError(w, 400, "bad_request", "invalid starts_at format", nil)
			return
		}
	} else {
		writeError(w, 400, "bad_request", "starts_at is required", nil)
		return
	}
	if req.EndsAt != "" {
		endsAt, err = time.Parse(time.RFC3339, req.EndsAt)
		if err != nil {
			writeError(w, 400, "bad_request", "invalid ends_at format", nil)
			return
		}
	} else {
		writeError(w, 400, "bad_request", "ends_at is required", nil)
		return
	}

	var location *string
	if req.Location != "" {
		location = &req.Location
	}
	var description *string
	if req.Description != "" {
		description = &req.Description
	}
	var moduleID *int64
	if req.ModuleID != 0 {
		moduleID = &req.ModuleID
	}

	e, err := h.svc.CreateEvent(r.Context(), userID, role, req.Title, req.Kind, startsAt, endsAt, req.AllDay, location, description, moduleID)
	if err != nil {
		log.Printf("event handler: CreateEvent error: %v", err)
		switch {
		case errors.Is(err, service.ErrEventNotFound):
			writeError(w, 404, "not_found", err.Error(), nil)
		case errors.Is(err, service.ErrOnlyStaffCanCreateEvent):
			writeError(w, 403, "forbidden", "only teachers and admins can create events", nil)
		case errors.Is(err, service.ErrEndsBeforeStarts):
			writeError(w, 400, "bad_request", err.Error(), nil)
		case errors.Is(err, service.ErrModuleNotFound):
			writeError(w, 404, "not_found", "module not found", nil)
		case errors.Is(err, service.ErrNotTeacherOfModule):
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		default:
			writeError(w, 500, "server_error", "could not create event", nil)
		}
		return
	}

	writeJSON(w, 201, dto.EventResponse{
		ID:          e.ID,
		Title:       e.Title,
		Kind:        e.Kind,
		StartsAt:    e.StartsAt,
		EndsAt:      e.EndsAt,
		AllDay:      e.AllDay,
		Location:    e.Location,
		Description: e.Description,
		ModuleID:    e.ModuleID,
		CreatedAt:   e.CreatedAt,
	})
}

// PATCH /events/:id
func (h *EventHandlers) UpdateEvent(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	eventID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid event ID", nil)
		return
	}

	var req dto.UpdateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	// Fetch existing event to merge with provided values and for auth check
	existing, err := h.svc.GetEventByID(r.Context(), userID, role, eventID)
	if err != nil {
		if errors.Is(err, store.ErrEventNotFound) {
			writeError(w, 404, "not_found", "event not found", nil)
		} else if errors.Is(err, errors.New("not the teacher of this module")) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else {
			log.Printf("event handler: UpdateEvent fetch error: %v", err)
			writeError(w, 500, "server_error", "could not fetch event", nil)
		}
		return
	}

	// Parse provided times, fall back to existing values
	startsAt := existing.StartsAt
	endsAt := existing.EndsAt
	if req.StartsAt != nil && *req.StartsAt != "" {
		startsAt, err = time.Parse(time.RFC3339, *req.StartsAt)
		if err != nil {
			writeError(w, 400, "bad_request", "invalid starts_at format", nil)
			return
		}
	}
	if req.EndsAt != nil && *req.EndsAt != "" {
		endsAt, err = time.Parse(time.RFC3339, *req.EndsAt)
		if err != nil {
			writeError(w, 400, "bad_request", "invalid ends_at format", nil)
			return
		}
	}
	if endsAt.Before(startsAt) {
		writeError(w, 400, "bad_request", "ends_at must be after starts_at", nil)
		return
	}

	// Merge provided fields with existing values
	title := existing.Title
	if req.Title != nil && *req.Title != "" {
		title = *req.Title
	}
	kind := existing.Kind
	if req.Kind != nil && *req.Kind != "" {
		kind = *req.Kind
	}
	allDay := existing.AllDay
	if req.AllDay != nil {
		allDay = *req.AllDay
	}

	var location *string
	if req.Location != nil {
		location = req.Location
	} else {
		location = existing.Location
	}
	var description *string
	if req.Description != nil {
		description = req.Description
	} else {
		description = existing.Description
	}
	var moduleID *int64
	if req.ModuleID != nil {
		moduleID = req.ModuleID
	} else {
		moduleID = existing.ModuleID
	}

	e, err := h.svc.UpdateEvent(r.Context(), userID, role, eventID, title, kind, startsAt, endsAt, allDay, location, description, moduleID)
	if err != nil {
		log.Printf("event handler: UpdateEvent error: %v", err)
		switch {
		case errors.Is(err, store.ErrEventNotFound):
			writeError(w, 404, "not_found", "event not found", nil)
		case errors.Is(err, service.ErrOnlyStaffCanCreateEvent):
			writeError(w, 403, "forbidden", "only teachers and admins can update events", nil)
		case errors.Is(err, service.ErrEndsBeforeStarts):
			writeError(w, 400, "bad_request", err.Error(), nil)
		case errors.Is(err, service.ErrNotTeacherOfModule):
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		default:
			writeError(w, 500, "server_error", "could not update event", nil)
		}
		return
	}

	writeJSON(w, 200, dto.EventResponse{
		ID:          e.ID,
		Title:       e.Title,
		Kind:        e.Kind,
		StartsAt:    e.StartsAt,
		EndsAt:      e.EndsAt,
		AllDay:      e.AllDay,
		Location:    e.Location,
		Description: e.Description,
		ModuleID:    e.ModuleID,
		CreatedAt:   e.CreatedAt,
	})
}

// DELETE /events/:id
func (h *EventHandlers) DeleteEvent(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	eventID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid event ID", nil)
		return
	}

	err = h.svc.DeleteEvent(r.Context(), userID, role, eventID)
	if err != nil {
		if errors.Is(err, store.ErrEventNotFound) {
			writeError(w, 404, "not_found", "event not found", nil)
		} else if errors.Is(err, service.ErrOnlyStaffCanCreateEvent) {
			writeError(w, 403, "forbidden", "only teachers and admins can delete events", nil)
		} else if errors.Is(err, service.ErrNotTeacherOfModule) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else {
			log.Printf("event handler: DeleteEvent error: %v", err)
			writeError(w, 500, "server_error", "could not delete event", nil)
		}
		return
	}

	w.WriteHeader(204)
}
