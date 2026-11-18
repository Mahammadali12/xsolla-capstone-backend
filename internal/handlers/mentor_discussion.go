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

// DiscussionService is what the talk handlers depend on.
type DiscussionService interface {
	List(ctx context.Context, userID int64, roles []string) ([]store.Discussion, error)
	Create(ctx context.Context, mentorID int64, roles []string, title, note string, startsAt, endsAt time.Time, studentIDs []int64, wholeTeam bool) ([]store.Discussion, error)
	Remove(ctx context.Context, userID int64, roles []string, id int64) error
}

type DiscussionHandlers struct {
	svc DiscussionService
}

func NewDiscussionHandlers(svc DiscussionService) *DiscussionHandlers {
	return &DiscussionHandlers{svc: svc}
}

func discussionResponses(list []store.Discussion) []dto.DiscussionResponse {
	out := make([]dto.DiscussionResponse, len(list))
	for i, d := range list {
		out[i] = dto.DiscussionResponse{
			ID:          strconv.FormatInt(d.ID, 10),
			BatchID:     strconv.FormatInt(d.BatchID, 10),
			MentorID:    d.MentorID,
			MentorName:  d.MentorName,
			StudentID:   d.StudentID,
			StudentName: d.StudentName,
			Title:       d.Title,
			Note:        d.Note,
			StartsAt:    d.StartsAt,
			EndsAt:      d.EndsAt,
		}
	}
	return out
}

func (h *DiscussionHandlers) writeList(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.List(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()))
	if err != nil {
		log.Printf("discussion handler: list error: %v", err)
		writeError(w, 500, "server_error", "could not list talks", nil)
		return
	}
	writeJSON(w, 200, discussionResponses(list))
}

// GET /mentor-discussions
func (h *DiscussionHandlers) List(w http.ResponseWriter, r *http.Request) {
	h.writeList(w, r)
}

// POST /mentor-discussions  body: {title, note?, starts_at, ends_at, student_ids? | whole_team?}; answers the caller's list
func (h *DiscussionHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateDiscussionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	if req.StartsAt == nil || req.EndsAt == nil {
		writeError(w, 400, "bad_request", "starts_at and ends_at are required", nil)
		return
	}
	_, err := h.svc.Create(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()),
		req.Title, req.Note, *req.StartsAt, *req.EndsAt, req.StudentIDs, req.WholeTeam)
	if err != nil {
		if errors.Is(err, service.ErrOnlyMentorCanSetDiscussions) {
			writeError(w, 403, "forbidden", "only mentors can set up a talk", nil)
		} else {
			writeError(w, 400, "bad_request", err.Error(), nil)
		}
		return
	}
	h.writeList(w, r)
}

// DELETE /mentor-discussions/{id}  removes the talk for every student invited to it; answers the caller's list
func (h *DiscussionHandlers) Remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid talk ID", nil)
		return
	}
	err = h.svc.Remove(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()), id)
	switch {
	case err == nil:
		h.writeList(w, r)
	case errors.Is(err, service.ErrOnlyMentorCanSetDiscussions):
		writeError(w, 403, "forbidden", "only mentors can remove a talk", nil)
	case errors.Is(err, service.ErrDiscussionNotFound):
		writeError(w, 404, "not_found", "talk not found", nil)
	default:
		log.Printf("discussion handler: remove error: %v", err)
		writeError(w, 500, "server_error", "could not remove the talk", nil)
	}
}
