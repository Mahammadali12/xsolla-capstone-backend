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
	"time"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// maxLectureFeedbackBody is far above the largest valid form (two 1000-character answers).
const maxLectureFeedbackBody = 16 << 10

// LectureFeedbackService is what the lecture feedback handlers need.
type LectureFeedbackService interface {
	Submit(ctx context.Context, userID int64, roles []string, sub service.LectureFeedbackSubmission) (*store.SentLectureFeedback, error)
	Sent(ctx context.Context, userID int64, roles []string) ([]store.SentLectureFeedback, error)
	Pending(ctx context.Context, userID int64, roles []string) ([]store.Lecture, error)
	List(ctx context.Context, f store.LectureFeedbackFilter) ([]store.LectureFeedbackRow, map[int64][]store.LectureFeedbackPerson, error)
	Summary(ctx context.Context, f store.LectureFeedbackFilter) ([]service.LectureFeedbackSummary, error)
}

type LectureFeedbackHandlers struct {
	svc LectureFeedbackService
}

func NewLectureFeedbackHandlers(svc LectureFeedbackService) *LectureFeedbackHandlers {
	return &LectureFeedbackHandlers{svc: svc}
}

// @Summary     The lecture feedback form
// @Description The questions in order; their ids are the fields of POST /lecture-feedback.
// @Tags        lecture-feedback
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} map[string]interface{}
// @Router      /lecture-feedback/form [get]
func (h *LectureFeedbackHandlers) Form(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"questions":                service.LectureFeedbackForm,
		"opens_minutes_before_end": int(service.LectureFeedbackOpensBeforeEnd / time.Minute),
		"open_days_after_end":      int(service.LectureFeedbackOpenFor / (24 * time.Hour)),
	})
}

// @Summary     Lectures I can give feedback on now (student)
// @Description Lectures whose form is open (10 minutes before the end until 7 days after) and not yet sent.
// @Tags        lecture-feedback
// @Produce     json
// @Security    BearerAuth
// @Success     200 {array}  dto.PendingLectureResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /lecture-feedback/pending [get]
func (h *LectureFeedbackHandlers) Pending(w http.ResponseWriter, r *http.Request) {
	lectures, err := h.svc.Pending(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()))
	if err != nil {
		writeLectureFeedbackError(w, "Pending", err)
		return
	}
	resp := make([]dto.PendingLectureResponse, len(lectures))
	for i, l := range lectures {
		_, closes := service.LectureFeedbackWindow(l)
		resp[i] = dto.PendingLectureResponse{LectureRef: lectureRef(l), ClosesAt: closes}
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary     Lectures I have given feedback on (student)
// @Tags        lecture-feedback
// @Produce     json
// @Security    BearerAuth
// @Success     200 {array}  dto.SentLectureFeedbackResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /lecture-feedback/me [get]
func (h *LectureFeedbackHandlers) Mine(w http.ResponseWriter, r *http.Request) {
	sent, err := h.svc.Sent(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()))
	if err != nil {
		writeLectureFeedbackError(w, "Mine", err)
		return
	}
	resp := make([]dto.SentLectureFeedbackResponse, len(sent))
	for i, s := range sent {
		resp[i] = dto.SentLectureFeedbackResponse{EventID: s.EventID, SubmittedAt: s.SubmittedAt}
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary     Send feedback on a lecture (student)
// @Description See docs/lecture_feedback.md for the questions, the rules and every answer.
// @Tags        lecture-feedback
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       request body dto.LectureFeedbackRequest true "The form"
// @Success     201 {object} dto.SentLectureFeedbackResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     409 {object} handlers.ErrorResponse
// @Failure     422 {object} handlers.ErrorResponse
// @Router      /lecture-feedback [post]
func (h *LectureFeedbackHandlers) Submit(w http.ResponseWriter, r *http.Request) {
	var req dto.LectureFeedbackRequest
	if !decodeLectureFeedback(w, r, &req) {
		return
	}
	sent, err := h.svc.Submit(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()),
		service.LectureFeedbackSubmission{EventID: req.EventID, Overall: req.Overall, Clarity: req.Clarity, Practice: req.Practice,
			Confidence: req.Confidence, Pace: req.Pace, Liked: req.Liked, Improve: req.Improve})
	if err != nil {
		writeLectureFeedbackError(w, "Submit", err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.SentLectureFeedbackResponse{EventID: sent.EventID, SubmittedAt: sent.SubmittedAt})
}

// @Summary     Lecture feedback with every answer (admin)
// @Description Filters are optional: event_id, module_id, and from/to as dates (YYYY-MM-DD, Baku time, both included) on the lecture's start.
// @Tags        lecture-feedback
// @Produce     json
// @Security    BearerAuth
// @Param       event_id  query int    false "Lecture (event) id"
// @Param       module_id query int    false "Week (module) id"
// @Param       from      query string false "First day, YYYY-MM-DD"
// @Param       to        query string false "Last day, YYYY-MM-DD"
// @Success     200 {array}  dto.LectureFeedbackResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /lecture-feedback [get]
func (h *LectureFeedbackHandlers) List(w http.ResponseWriter, r *http.Request) {
	f, ok := lectureFeedbackFilter(w, r)
	if !ok {
		return
	}
	rows, lecturers, err := h.svc.List(r.Context(), f)
	if err != nil {
		writeLectureFeedbackError(w, "List", err)
		return
	}
	resp := make([]dto.LectureFeedbackResponse, len(rows))
	for i, row := range rows {
		resp[i] = dto.LectureFeedbackResponse{
			ID: row.ID, Lecture: lectureRef(row.Lecture), Lecturers: lecturerList(row.Lecture, lecturers),
			Student: dto.LectureFeedbackPersonResponse{ID: row.StudentID, Name: row.StudentName, Email: row.StudentEmail},
			Overall: row.Overall, Clarity: row.Clarity, Practice: row.Practice, Pace: row.Pace, Confidence: row.Confidence,
			Liked: row.Liked, Improve: row.Improve, SubmittedAt: row.SubmittedAt,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary     Lecture feedback numbers per lecture (admin)
// @Description Every lecture in the filter, also those without feedback: responses, students expected, averages, pace counts.
// @Tags        lecture-feedback
// @Produce     json
// @Security    BearerAuth
// @Param       event_id  query int    false "Lecture (event) id"
// @Param       module_id query int    false "Week (module) id"
// @Param       from      query string false "First day, YYYY-MM-DD"
// @Param       to        query string false "Last day, YYYY-MM-DD"
// @Success     200 {array}  dto.LectureFeedbackSummaryResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /lecture-feedback/summary [get]
func (h *LectureFeedbackHandlers) Summary(w http.ResponseWriter, r *http.Request) {
	f, ok := lectureFeedbackFilter(w, r)
	if !ok {
		return
	}
	sums, err := h.svc.Summary(r.Context(), f)
	if err != nil {
		writeLectureFeedbackError(w, "Summary", err)
		return
	}
	resp := make([]dto.LectureFeedbackSummaryResponse, len(sums))
	for i, s := range sums {
		people := make([]dto.LectureFeedbackPersonResponse, len(s.Lecturers))
		for j, p := range s.Lecturers {
			people[j] = dto.LectureFeedbackPersonResponse{ID: p.ID, Name: p.Name, Email: p.Email}
		}
		resp[i] = dto.LectureFeedbackSummaryResponse{Lecture: lectureRef(s.Lecture), Lecturers: people,
			Responses: s.Responses, Expected: s.Expected, Averages: s.Averages, Pace: s.Pace, WithComments: s.WithComments}
	}
	writeJSON(w, http.StatusOK, resp)
}

func lectureRef(l store.Lecture) dto.LectureRef {
	return dto.LectureRef{EventID: l.ID, Title: l.Title, StartsAt: l.StartsAt, EndsAt: l.EndsAt, ModuleID: l.ModuleID, ModuleTitle: l.ModuleTitle}
}

func lecturerList(l store.Lecture, byModule map[int64][]store.LectureFeedbackPerson) []dto.LectureFeedbackPersonResponse {
	out := []dto.LectureFeedbackPersonResponse{}
	if l.ModuleID == nil {
		return out
	}
	for _, p := range byModule[*l.ModuleID] {
		out = append(out, dto.LectureFeedbackPersonResponse{ID: p.ID, Name: p.Name, Email: p.Email})
	}
	return out
}

// lectureFeedbackFilter reads event_id, module_id (positive ids) and from/to (YYYY-MM-DD, Baku time, both days
// included) from the query. Empty values do not filter; anything else broken is a 400 naming the parameter.
func lectureFeedbackFilter(w http.ResponseWriter, r *http.Request) (store.LectureFeedbackFilter, bool) {
	var f store.LectureFeedbackFilter
	q := r.URL.Query()
	for name, dst := range map[string]**int64{"event_id": &f.EventID, "module_id": &f.ModuleID} {
		raw := strings.TrimSpace(q.Get(name))
		if raw == "" {
			continue
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "bad_request", name+" must be a positive whole number", nil)
			return f, false
		}
		*dst = &id
	}
	day := func(name string) (*time.Time, bool) {
		raw := strings.TrimSpace(q.Get(name))
		if raw == "" {
			return nil, true
		}
		t, err := time.ParseInLocation("2006-01-02", raw, service.LectureFeedbackZone())
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", name+" must be a date like 2026-11-14", nil)
			return nil, false
		}
		return &t, true
	}
	from, ok := day("from")
	if !ok {
		return f, false
	}
	to, ok := day("to")
	if !ok {
		return f, false
	}
	if from != nil && to != nil && from.After(*to) {
		writeError(w, http.StatusBadRequest, "bad_request", "from must not be after to", nil)
		return f, false
	}
	f.From = from
	if to != nil {
		end := to.AddDate(0, 0, 1) // the whole "to" day is included
		f.To = &end
	}
	return f, true
}

// decodeLectureFeedback reads one JSON object of reasonable size. A value of the wrong type is a broken form rule
// (422 naming the field); anything that is not JSON is a 400.
func decodeLectureFeedback(w http.ResponseWriter, r *http.Request, req *dto.LectureFeedbackRequest) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLectureFeedbackBody))
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
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "the form is too large", nil)
	case errors.As(err, &typeErr) && typeErr.Field != "":
		msg := typeErr.Field + " has the wrong type"
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", msg, []ErrorDetail{{Field: typeErr.Field, Message: msg}})
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
	}
	return false
}

func writeLectureFeedbackError(w http.ResponseWriter, op string, err error) {
	var invalid *service.LectureFeedbackInvalidError
	switch {
	case errors.As(err, &invalid):
		details := make([]ErrorDetail, len(invalid.Problems))
		for i, p := range invalid.Problems {
			details[i] = ErrorDetail{Field: p.Field, Message: p.Message}
		}
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", invalid.Error(), details)
	case errors.Is(err, service.ErrLectureFeedbackForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "only students give lecture feedback", nil)
	case errors.Is(err, service.ErrLectureFeedbackNotFound):
		writeError(w, http.StatusNotFound, "not_found", "lecture not found", nil)
	case errors.Is(err, service.ErrLectureFeedbackExists):
		writeError(w, http.StatusConflict, "already_sent", "you have already given feedback on this lecture", nil)
	case errors.Is(err, service.ErrLectureFeedbackNotOpen), errors.Is(err, service.ErrLectureFeedbackClosed):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error(), nil)
	default:
		log.Printf("lecture feedback handler: %s error: %v", op, err)
		writeError(w, http.StatusInternalServerError, "server_error", "could not handle the lecture feedback", nil)
	}
}
