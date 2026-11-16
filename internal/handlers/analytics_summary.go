package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// maxAnalyticsSummaryBody is far above the largest valid body (two dates and a flag).
const maxAnalyticsSummaryBody = 4 << 10

// AnalyticsSummaryService is what the AI summary handlers need.
type AnalyticsSummaryService interface {
	Student(ctx context.Context, studentID int64, from, to *time.Time, force bool) (*service.SummaryResult, error)
	Mentor(ctx context.Context, mentorID int64, from, to *time.Time, force bool) (*service.SummaryResult, error)
}

// AnalyticsSummaryHandlers serves the admin-only AI summary routes.
type AnalyticsSummaryHandlers struct {
	svc AnalyticsSummaryService
}

// NewAnalyticsSummaryHandlers creates the handlers.
func NewAnalyticsSummaryHandlers(svc AnalyticsSummaryService) *AnalyticsSummaryHandlers {
	return &AnalyticsSummaryHandlers{svc: svc}
}

// @Summary     AI summary of a student's feedback (admin)
// @Description Summarizes the written feedback the student received in the period. The feedback is read from the database and never returned; the response contains only the generated plain-text summary and its metadata.
// @Tags        analytics
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       studentId path  int                          true  "Student user id"
// @Param       request   body  dto.AnalyticsSummaryRequest  true  "Period and refresh flag"
// @Success     200 {object} dto.AnalyticsSummaryResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     502 {object} handlers.ErrorResponse
// @Failure     503 {object} handlers.ErrorResponse
// @Router      /analytics/students/{studentId}/ai-summary [post]
func (h *AnalyticsSummaryHandlers) Student(w http.ResponseWriter, r *http.Request) {
	studentID, ok := pathID(w, r, "studentId", "student")
	if !ok {
		return
	}
	h.summarize(w, r, studentID, h.svc.Student, "StudentSummary")
}

// @Summary     AI summary of a mentor's reviews (admin)
// @Description Summarizes the student-submitted reviews about the mentor in the period, comments and structured answers alike. The reviews are read from the database and never returned.
// @Tags        analytics
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       mentorId path  int                          true  "Mentor user id"
// @Param       request  body  dto.AnalyticsSummaryRequest  true  "Period and refresh flag"
// @Success     200 {object} dto.AnalyticsSummaryResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     502 {object} handlers.ErrorResponse
// @Failure     503 {object} handlers.ErrorResponse
// @Router      /analytics/mentors/{mentorId}/ai-summary [post]
func (h *AnalyticsSummaryHandlers) Mentor(w http.ResponseWriter, r *http.Request) {
	mentorID, ok := pathID(w, r, "mentorId", "mentor")
	if !ok {
		return
	}
	h.summarize(w, r, mentorID, h.svc.Mentor, "MentorSummary")
}

func (h *AnalyticsSummaryHandlers) summarize(w http.ResponseWriter, r *http.Request, id int64,
	generate func(context.Context, int64, *time.Time, *time.Time, bool) (*service.SummaryResult, error), op string,
) {
	var req dto.AnalyticsSummaryRequest
	if !decodeAnalyticsSummary(w, r, &req) {
		return
	}
	from, ok := queryDate(w, req.From, "from")
	if !ok {
		return
	}
	to, ok := queryDate(w, req.To, "to")
	if !ok {
		return
	}

	result, err := generate(r.Context(), id, from, to, req.ForceRefresh)
	if err != nil {
		writeAnalyticsSummaryError(w, op, err)
		return
	}
	var generatedAt *time.Time
	if result.GeneratedAt != nil {
		utc := result.GeneratedAt.UTC()
		generatedAt = &utc
	}
	writeJSON(w, http.StatusOK, dto.AnalyticsSummaryResponse{
		Summary:     result.Summary,
		SourceCount: result.SourceCount,
		GeneratedAt: generatedAt,
		Cached:      result.Cached,
		Period:      periodResponse(result.Period),
	})
}

// decodeAnalyticsSummary reads exactly one JSON object of reasonable size. An empty body is allowed: every field
// is optional.
func decodeAnalyticsSummary(w http.ResponseWriter, r *http.Request, req *dto.AnalyticsSummaryRequest) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAnalyticsSummaryBody))
	err := dec.Decode(req)
	if errors.Is(err, io.EOF) {
		return true
	}
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("trailing data after the JSON object")
	}
	if err == nil {
		return true
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "the request body is too large", nil)
		return false
	}
	writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
	return false
}

// writeAnalyticsSummaryError maps the service errors to the project's error envelope. Provider details never reach
// the client, and a failed generation is never replaced by an invented summary.
func writeAnalyticsSummaryError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, service.ErrAnalyticsPeriodInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", "from must not be after to", nil)
	case errors.Is(err, service.ErrAnalyticsPeriodTooLong):
		writeError(w, http.StatusBadRequest, "bad_request", "the reporting period must be at most 366 days", nil)
	case errors.Is(err, store.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user not found", nil)
	case errors.Is(err, service.ErrSummaryProviderUnavailable):
		log.Printf("analytics summary handler: %s provider unavailable: %v", op, err)
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "the AI summary service is unavailable", nil)
	case errors.Is(err, service.ErrSummaryProviderInvalid),
		errors.Is(err, service.ErrSummaryOutputInvalid),
		errors.Is(err, service.ErrSummaryInputTooLarge):
		log.Printf("analytics summary handler: %s generation failed: %v", op, err)
		writeError(w, http.StatusBadGateway, "upstream_error", "could not generate a summary", nil)
	default:
		log.Printf("analytics summary handler: %s error: %v", op, err)
		writeError(w, http.StatusInternalServerError, "server_error", "could not generate a summary", nil)
	}
}
