package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// maxMentorReviewBody is far above the largest valid review (a 1000-character comment and a 120-character topic).
const maxMentorReviewBody = 16 << 10

// MentorReviewService is the interface the mentor review handlers depend on.
type MentorReviewService interface {
	ListMine(ctx context.Context, userID int64, roles []string) ([]store.ReviewedModule, error)
	Submit(ctx context.Context, userID int64, roles []string, sub service.MentorReviewSubmission) (*store.MentorReview, error)
}

type MentorReviewHandlers struct {
	svc MentorReviewService
}

func NewMentorReviewHandlers(svc MentorReviewService) *MentorReviewHandlers {
	return &MentorReviewHandlers{svc: svc}
}

// ListMine handles GET /mentor-reviews/me: the weeks the signed-in student has reviewed.
func (h *MentorReviewHandlers) ListMine(w http.ResponseWriter, r *http.Request) {
	reviewed, err := h.svc.ListMine(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()))
	if err != nil {
		writeMentorReviewError(w, "ListMine", err)
		return
	}
	resp := make([]dto.ReviewedWeekResponse, len(reviewed))
	for i, m := range reviewed {
		resp[i] = dto.ReviewedWeekResponse{ModuleID: m.ModuleID, SubmittedAt: m.SubmittedAt}
	}
	writeJSON(w, http.StatusOK, resp)
}

// Submit handles POST /mentor-reviews: the student's review of one week.
func (h *MentorReviewHandlers) Submit(w http.ResponseWriter, r *http.Request) {
	var req dto.MentorReviewRequest
	if !decodeMentorReview(w, r, &req) {
		return
	}
	review, err := h.svc.Submit(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()),
		service.MentorReviewSubmission{
			ModuleID:        req.ModuleID,
			HadSession:      req.HadSession,
			NoSessionReason: req.NoSessionReason,
			Topics:          req.Topics,
			OtherTopic:      req.OtherTopic,
			Ratings:         req.Ratings,
			Overall:         req.Overall,
			Comment:         req.Comment,
		})
	if err != nil {
		writeMentorReviewError(w, "Submit", err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.ReviewedWeekResponse{ModuleID: review.ModuleID, SubmittedAt: review.SubmittedAt})
}

// decodeMentorReview reads exactly one JSON object of reasonable size. A value of the wrong type (a string for
// overall, 4.5 for a rating) is a broken form rule (422); anything that is not JSON is a bad request (400).
func decodeMentorReview(w http.ResponseWriter, r *http.Request, req *dto.MentorReviewRequest) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMentorReviewBody))
	err := dec.Decode(req)
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("trailing data after the JSON object")
	}
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "the review is too large", nil)
	case errors.As(err, &typeErr) && typeErr.Field != "":
		msg := typeErr.Field + " has the wrong type"
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", msg, []ErrorDetail{{Field: typeErr.Field, Message: msg}})
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
	}
	return false
}

func writeMentorReviewError(w http.ResponseWriter, op string, err error) {
	var invalid *service.MentorReviewValidationError
	switch {
	case errors.As(err, &invalid):
		details := make([]ErrorDetail, len(invalid.Fields))
		for i, f := range invalid.Fields {
			details[i] = ErrorDetail{Field: f.Field, Message: f.Message}
		}
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", invalid.Error(), details)
	case errors.Is(err, service.ErrMentorReviewForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "only students send mentor reviews", nil)
	case errors.Is(err, service.ErrMentorReviewModuleNotFound):
		writeError(w, http.StatusNotFound, "not_found", "module not found", nil)
	case errors.Is(err, service.ErrMentorReviewExists):
		writeError(w, http.StatusConflict, "already_reviewed", "you have already reviewed this week", nil)
	case errors.Is(err, service.ErrMentorReviewNoTeam):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "you are not in a team yet, so there is no mentor to review", nil)
	case errors.Is(err, service.ErrMentorReviewNoMentor):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", "your team has no mentor yet, so there is nobody to review", nil)
	case errors.Is(err, service.ErrMentorReviewNotOpen):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error(), nil)
	default:
		log.Printf("mentor review handler: %s error: %v", op, err)
		writeError(w, http.StatusInternalServerError, "server_error", "could not handle the mentor review", nil)
	}
}
