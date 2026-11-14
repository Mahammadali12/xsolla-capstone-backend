package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

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
	ListReviews(ctx context.Context, moduleID, mentorID, teamID *int64) ([]store.MentorReviewRow, error)
	Summary(ctx context.Context, fromModule, toModule *int64) ([]service.MentorReviewSummary, error)
	Missing(ctx context.Context, moduleID int64) ([]store.ExpectedReviewer, error)
}

type MentorReviewHandlers struct {
	svc MentorReviewService
}

func NewMentorReviewHandlers(svc MentorReviewService) *MentorReviewHandlers {
	return &MentorReviewHandlers{svc: svc}
}

// @Summary     The weeks I have reviewed (student)
// @Tags        mentor-reviews
// @Produce     json
// @Security    BearerAuth
// @Success     200 {array}  dto.ReviewedWeekResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /mentor-reviews/me [get]
//
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

// @Summary     Send this week's mentor review (student)
// @Description See docs/mentor_reviews.md for the form rules and every answer.
// @Tags        mentor-reviews
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       request body dto.MentorReviewRequest true "The review"
// @Success     201 {object} dto.ReviewedWeekResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     409 {object} handlers.ErrorResponse
// @Failure     422 {object} handlers.ErrorResponse
// @Router      /mentor-reviews [post]
//
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
	case errors.Is(err, service.ErrMentorReviewRange):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
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

// ---- admin read routes (registered with RequireRole "admin": mentors never see the reviews about them) ----------

// @Summary     Mentor reviews with every answer (admin)
// @Description Filter by week, mentor and team; every filter is optional. Ordered by week, mentor, student.
// @Tags        mentor-reviews
// @Produce     json
// @Security    BearerAuth
// @Param       module_id query int false "Week (module) id"
// @Param       mentor_id query int false "Mentor user id"
// @Param       team_id   query int false "Team id"
// @Success     200 {array}  dto.MentorReviewResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /mentor-reviews [get]
func (h *MentorReviewHandlers) List(w http.ResponseWriter, r *http.Request) {
	ids, ok := queryIDs(w, r, "module_id", "mentor_id", "team_id")
	if !ok {
		return
	}
	rows, err := h.svc.ListReviews(r.Context(), ids["module_id"], ids["mentor_id"], ids["team_id"])
	if err != nil {
		writeMentorReviewError(w, "List", err)
		return
	}
	resp := make([]dto.MentorReviewResponse, len(rows))
	for i, row := range rows {
		resp[i] = dto.MentorReviewResponse{
			ID: row.ID, ModuleID: row.ModuleID, ModuleTitle: row.ModuleTitle,
			Student:    dto.ReviewPerson{ID: row.StudentID, Name: row.StudentName, Email: row.StudentEmail},
			Mentor:     dto.ReviewPerson{ID: row.MentorID, Name: row.MentorName, Email: row.MentorEmail},
			HadSession: row.HadSession, NoSessionReason: row.NoSessionReason, Topics: row.Topics, OtherTopic: row.OtherTopic,
			Overall: row.Overall, Comment: row.Comment, SubmittedAt: row.SubmittedAt,
		}
		if row.TeamID != nil && row.TeamName != nil {
			resp[i].Team = &dto.ReviewTeam{ID: *row.TeamID, Name: *row.TeamName}
		}
		if row.Ratings != nil {
			resp[i].Ratings = &dto.MentorReviewRatingsResponse{Clarity: row.Ratings.Clarity, Engagement: row.Ratings.Engagement,
				Comfort: row.Ratings.Comfort, Time: row.Ratings.Time}
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary     Mentor review numbers per week and mentor (admin)
// @Description Reviews, expected students (today's teams), sessions held, averages, topic counts and no-session reasons for the weeks from from_module to to_module (by opening date, both optional and included).
// @Tags        mentor-reviews
// @Produce     json
// @Security    BearerAuth
// @Param       from_module query int false "First week (module) id"
// @Param       to_module   query int false "Last week (module) id"
// @Success     200 {array}  dto.MentorReviewSummaryResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /mentor-reviews/summary [get]
func (h *MentorReviewHandlers) Summary(w http.ResponseWriter, r *http.Request) {
	ids, ok := queryIDs(w, r, "from_module", "to_module")
	if !ok {
		return
	}
	sums, err := h.svc.Summary(r.Context(), ids["from_module"], ids["to_module"])
	if err != nil {
		writeMentorReviewError(w, "Summary", err)
		return
	}
	resp := make([]dto.MentorReviewSummaryResponse, len(sums))
	for i, s := range sums {
		resp[i] = dto.MentorReviewSummaryResponse{
			ModuleID: s.ModuleID, ModuleTitle: s.ModuleTitle,
			Mentor:  dto.ReviewPerson{ID: s.MentorID, Name: s.MentorName, Email: s.MentorEmail},
			Reviews: s.Reviews, Expected: s.Expected, SessionsHeld: s.SessionsHeld, NoSession: s.NoSession,
			Averages: s.Averages, Topics: s.Topics, NoSessionReasons: s.NoSessionReasons,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary     Students who have not sent the week's review (admin)
// @Description Students in a team with a mentor (today's teams), enrolled in the week, without a review for it.
// @Tags        mentor-reviews
// @Produce     json
// @Security    BearerAuth
// @Param       module_id query int true "Week (module) id"
// @Success     200 {array}  dto.MissingReviewResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /mentor-reviews/missing [get]
func (h *MentorReviewHandlers) Missing(w http.ResponseWriter, r *http.Request) {
	ids, ok := queryIDs(w, r, "module_id")
	if !ok {
		return
	}
	if ids["module_id"] == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "module_id is required", nil)
		return
	}
	missing, err := h.svc.Missing(r.Context(), *ids["module_id"])
	if err != nil {
		writeMentorReviewError(w, "Missing", err)
		return
	}
	resp := make([]dto.MissingReviewResponse, len(missing))
	for i, m := range missing {
		resp[i] = dto.MissingReviewResponse{
			Student: dto.ReviewPerson{ID: m.StudentID, Name: m.StudentName, Email: m.StudentEmail},
			Team:    dto.ReviewTeam{ID: m.TeamID, Name: m.TeamName},
			Mentor:  dto.ReviewPerson{ID: m.MentorID, Name: m.MentorName, Email: m.MentorEmail},
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// queryIDs reads optional positive ids from the query string; an empty value counts as not sent. A value that is
// not a positive whole number is a 400 naming the parameter.
func queryIDs(w http.ResponseWriter, r *http.Request, names ...string) (map[string]*int64, bool) {
	out := make(map[string]*int64, len(names))
	q := r.URL.Query()
	for _, name := range names {
		raw := strings.TrimSpace(q.Get(name))
		if raw == "" {
			out[name] = nil
			continue
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", name+" must be a positive whole number", nil)
			return nil, false
		}
		out[name] = &id
	}
	return out, true
}
