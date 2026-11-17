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
)

// MentorSlotService is the interface that mentor slot handlers depend on.
type MentorSlotService interface {
	ListSlots(ctx context.Context, userID int64, roles []string) ([]service.MentorSlot, error)
	CreateAvailability(ctx context.Context, userID int64, roles []string, startsAt, endsAt time.Time) ([]service.MentorSlot, error)
	RemoveSlot(ctx context.Context, userID int64, roles []string, id int64) error
	BookSlot(ctx context.Context, userID int64, roles []string, slotID int64) (*service.MentorSlot, error)
	CancelBooking(ctx context.Context, userID int64, roles []string, slotID int64) (*service.MentorSlot, error)
}

type MentorSlotHandlers struct {
	svc MentorSlotService
	avatarSupport
}

func NewMentorSlotHandlers(svc MentorSlotService) *MentorSlotHandlers {
	return &MentorSlotHandlers{svc: svc}
}

// toResponse converts service slots to DTO responses.
func toResponse(slots []service.MentorSlot) []dto.MentorSlotResponse {
	resp := make([]dto.MentorSlotResponse, len(slots))
	for i, s := range slots {
		attendees := make([]dto.Attendee, len(s.Attendees))
		for j, a := range s.Attendees {
			attendees[j] = dto.Attendee{ID: a.ID, Name: a.Name, Email: a.Email}
		}
		resp[i] = dto.MentorSlotResponse{
			ID:           strconv.FormatInt(s.ID, 10),
			MentorID:     s.MentorID,
			MentorName:   s.MentorName,
			StartsAt:     s.StartsAt,
			EndsAt:       s.EndsAt,
			Booked:       s.Booked,
			BookedBy:     s.BookedBy,
			BookedByName: s.BookedByName,
			MeetingURL:   s.MeetingURL,
			Attendees:    attendees,
		}
	}
	return resp
}

// writeSlots writes the slot list as JSON, with the attendees' pictures.
func (h *MentorSlotHandlers) writeSlots(w http.ResponseWriter, r *http.Request, slots []service.MentorSlot) {
	resp := toResponse(slots)
	lists := make([][]dto.Attendee, len(resp))
	for i := range resp {
		lists[i] = resp[i].Attendees
	}
	h.fillAttendees(r, lists...)
	writeJSON(w, 200, resp)
}

// GET /mentor-slots
func (h *MentorSlotHandlers) ListSlots(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	slots, err := h.svc.ListSlots(r.Context(), userID, roles)
	if err != nil {
		log.Printf("mentor slot handler: ListSlots error: %v", err)
		writeError(w, 500, "server_error", "could not list slots", nil)
		return
	}
	h.writeSlots(w, r, slots)
}

// POST /mentor-slots  body: {starts_at, ends_at} (RFC3339 timestamps with an explicit timezone)
func (h *MentorSlotHandlers) CreateAvailability(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	var req dto.CreateAvailabilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	if req.StartsAt == nil || req.EndsAt == nil {
		writeError(w, 400, "bad_request", "starts_at and ends_at are required", nil)
		return
	}

	// Create the slots.
	if _, err := h.svc.CreateAvailability(r.Context(), userID, roles, *req.StartsAt, *req.EndsAt); err != nil {
		log.Printf("mentor slot handler: CreateAvailability error: %v", err)
		switch {
		case errors.Is(err, service.ErrOnlyMentorCanManageSlots):
			writeError(w, 403, "forbidden", "only mentors can add 1:1 times", nil)
		default:
			writeError(w, 400, "bad_request", err.Error(), nil)
		}
		return
	}

	// Return the caller's full updated slot list.
	slots, err := h.svc.ListSlots(r.Context(), userID, roles)
	if err != nil {
		log.Printf("mentor slot handler: ListSlots after create error: %v", err)
		writeError(w, 500, "server_error", "could not list slots", nil)
		return
	}
	h.writeSlots(w, r, slots)
}

// DELETE /mentor-slots/:id
func (h *MentorSlotHandlers) RemoveSlot(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid slot ID", nil)
		return
	}

	if err := h.svc.RemoveSlot(r.Context(), userID, roles, id); err != nil {
		log.Printf("mentor slot handler: RemoveSlot error: %v", err)
		switch {
		case errors.Is(err, service.ErrOnlyMentorCanManageSlots):
			writeError(w, 403, "forbidden", "only mentors can remove 1:1 times", nil)
		case errors.Is(err, service.ErrMentorSlotNotFound):
			writeError(w, 404, "not_found", "slot not found or already booked", nil)
		default:
			writeError(w, 500, "server_error", "could not remove slot", nil)
		}
		return
	}

	slots, err := h.svc.ListSlots(r.Context(), userID, roles)
	if err != nil {
		log.Printf("mentor slot handler: ListSlots after remove error: %v", err)
		writeError(w, 500, "server_error", "could not list slots", nil)
		return
	}
	h.writeSlots(w, r, slots)
}

// POST /mentor-slots/:id/book
func (h *MentorSlotHandlers) BookSlot(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid slot ID", nil)
		return
	}

	if _, err := h.svc.BookSlot(r.Context(), userID, roles, id); err != nil {
		log.Printf("mentor slot handler: BookSlot error: %v", err)
		switch {
		case errors.Is(err, service.ErrOnlyStudentsCanBook):
			writeError(w, 403, "forbidden", "only students can book a 1:1", nil)
		case errors.Is(err, service.ErrStudentHasBooking):
			writeError(w, 409, "conflict", "you already have an upcoming 1:1 booking", nil)
		case errors.Is(err, service.ErrSlotNotBookable):
			writeError(w, 409, "conflict", "slot is not bookable", nil)
		case errors.Is(err, service.ErrMentorSlotNotFound):
			writeError(w, 404, "not_found", "slot not found", nil)
		default:
			writeError(w, 500, "server_error", "could not book slot", nil)
		}
		return
	}

	slots, err := h.svc.ListSlots(r.Context(), userID, roles)
	if err != nil {
		log.Printf("mentor slot handler: ListSlots after book error: %v", err)
		writeError(w, 500, "server_error", "could not list slots", nil)
		return
	}
	h.writeSlots(w, r, slots)
}

// DELETE /mentor-slots/:id/book
func (h *MentorSlotHandlers) CancelBooking(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid slot ID", nil)
		return
	}

	if _, err := h.svc.CancelBooking(r.Context(), userID, roles, id); err != nil {
		log.Printf("mentor slot handler: CancelBooking error: %v", err)
		switch {
		case errors.Is(err, service.ErrMentorSlotNotFound):
			writeError(w, 404, "not_found", "slot or booking not found", nil)
		default:
			writeError(w, 500, "server_error", "could not cancel booking", nil)
		}
		return
	}

	slots, err := h.svc.ListSlots(r.Context(), userID, roles)
	if err != nil {
		log.Printf("mentor slot handler: ListSlots after cancel error: %v", err)
		writeError(w, 500, "server_error", "could not list slots", nil)
		return
	}
	h.writeSlots(w, r, slots)
}
