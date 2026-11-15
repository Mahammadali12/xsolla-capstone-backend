package handlers

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// AnalyticsService is the interface the analytics handlers depend on.
type AnalyticsService interface {
	Overview(ctx context.Context, from, to *time.Time) (*service.OverviewReport, error)
	Student(ctx context.Context, studentID int64, from, to *time.Time, moduleID *int64) (*service.StudentReport, error)
	Mentor(ctx context.Context, mentorID int64, from, to *time.Time) (*service.MentorReport, error)
	SearchStudents(ctx context.Context, q string, limit, offset int) ([]store.AnalyticsPerson, error)
	SearchMentors(ctx context.Context, q string, limit, offset int) ([]store.AnalyticsPerson, error)
}

// AnalyticsHandlers serves the admin-only reporting routes.
type AnalyticsHandlers struct {
	svc AnalyticsService
}

// NewAnalyticsHandlers creates the handlers.
func NewAnalyticsHandlers(svc AnalyticsService) *AnalyticsHandlers {
	return &AnalyticsHandlers{svc: svc}
}

// @Summary     Weekly student standing, school-wide (admin)
// @Description Green/yellow/red counts per module (week). Total counts only students with a recorded grade; weeks without grades have zero counts.
// @Tags        analytics
// @Produce     json
// @Security    BearerAuth
// @Param       from query string false "Start date, YYYY-MM-DD (inclusive)"
// @Param       to   query string false "End date, YYYY-MM-DD (inclusive)"
// @Success     200 {object} dto.AnalyticsOverviewResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /analytics/overview [get]
func (h *AnalyticsHandlers) Overview(w http.ResponseWriter, r *http.Request) {
	from, to, ok := queryPeriod(w, r)
	if !ok {
		return
	}
	report, err := h.svc.Overview(r.Context(), from, to)
	if err != nil {
		writeAnalyticsError(w, "Overview", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.AnalyticsOverviewResponse{
		Period:      periodResponse(report.Period),
		GeneratedAt: report.GeneratedAt,
		Weeks:       weekResponses(report.Weeks),
	})
}

// @Summary     Student selector (admin)
// @Description Active students matching q, id/name/email only.
// @Tags        analytics
// @Produce     json
// @Security    BearerAuth
// @Param       q      query string false "Name or email fragment"
// @Param       limit  query int    false "Page size, 1-100 (default 20)"
// @Param       offset query int    false "Rows to skip (default 0)"
// @Success     200 {array}  dto.AnalyticsPersonResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /analytics/students [get]
func (h *AnalyticsHandlers) ListStudents(w http.ResponseWriter, r *http.Request) {
	h.listPeople(w, r, h.svc.SearchStudents)
}

// @Summary     Mentor selector (admin)
// @Description Active mentors matching q, id/name/email only.
// @Tags        analytics
// @Produce     json
// @Security    BearerAuth
// @Param       q      query string false "Name or email fragment"
// @Param       limit  query int    false "Page size, 1-100 (default 20)"
// @Param       offset query int    false "Rows to skip (default 0)"
// @Success     200 {array}  dto.AnalyticsPersonResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /analytics/mentors [get]
func (h *AnalyticsHandlers) ListMentors(w http.ResponseWriter, r *http.Request) {
	h.listPeople(w, r, h.svc.SearchMentors)
}

func (h *AnalyticsHandlers) listPeople(w http.ResponseWriter, r *http.Request,
	search func(context.Context, string, int, int) ([]store.AnalyticsPerson, error)) {
	limit, offset, ok := queryPage(w, r)
	if !ok {
		return
	}
	people, err := search(r.Context(), r.URL.Query().Get("q"), limit, offset)
	if err != nil {
		writeAnalyticsError(w, "ListPeople", err)
		return
	}
	resp := make([]dto.AnalyticsPersonResponse, len(people))
	for i, p := range people {
		resp[i] = dto.AnalyticsPersonResponse{ID: p.ID, Name: p.Name, Email: p.Email}
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary     One student's submission status and quiz accuracy (admin)
// @Description Assignment deadlines in the period are classified by the student's first submission; quiz accuracy is sum(score)/sum(total) per module.
// @Tags        analytics
// @Produce     json
// @Security    BearerAuth
// @Param       studentId path  int    true  "Student user id"
// @Param       from      query string false "Start date, YYYY-MM-DD (inclusive)"
// @Param       to        query string false "End date, YYYY-MM-DD (inclusive)"
// @Param       module_id query int    false "Only this module (week)"
// @Success     200 {object} dto.StudentAnalyticsResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /analytics/students/{studentId} [get]
func (h *AnalyticsHandlers) Student(w http.ResponseWriter, r *http.Request) {
	studentID, ok := pathID(w, r, "studentId", "student")
	if !ok {
		return
	}
	from, to, ok := queryPeriod(w, r)
	if !ok {
		return
	}
	ids, ok := queryIDs(w, r, "module_id")
	if !ok {
		return
	}
	report, err := h.svc.Student(r.Context(), studentID, from, to, ids["module_id"])
	if err != nil {
		writeAnalyticsError(w, "Student", err)
		return
	}
	quizzes := make([]dto.QuizAccuracyResponse, len(report.Quizzes))
	for i, q := range report.Quizzes {
		quizzes[i] = dto.QuizAccuracyResponse{
			ModuleID: q.ModuleID, ModuleTitle: q.ModuleTitle, OpensAt: q.OpensAt,
			ScoreTotal: q.ScoreTotal, PossibleTotal: q.PossibleTotal,
			Accuracy: q.Accuracy, AttemptCount: q.AttemptCount,
		}
	}
	writeJSON(w, http.StatusOK, dto.StudentAnalyticsResponse{
		Student:     dto.AnalyticsPersonResponse{ID: report.Student.ID, Name: report.Student.Name},
		Period:      periodResponse(report.Period),
		GeneratedAt: report.GeneratedAt,
		AssignmentStatus: dto.AssignmentStatusResponse{
			OnTimeCount: report.Status.OnTime, LateCount: report.Status.Late,
			MissingCount: report.Status.Missing, EligibleCount: report.Status.Eligible,
		},
		QuizAccuracyByModule: quizzes,
	})
}

// @Summary     One mentor's feedback averages and students' standing (admin)
// @Description Averages are over submitted reviews in the period, per dimension, and null when nobody rated it. Weekly standing uses today's team membership (standing_attribution).
// @Tags        analytics
// @Produce     json
// @Security    BearerAuth
// @Param       mentorId path  int    true  "Mentor user id"
// @Param       from     query string false "Start date, YYYY-MM-DD (inclusive)"
// @Param       to       query string false "End date, YYYY-MM-DD (inclusive)"
// @Success     200 {object} dto.MentorAnalyticsResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /analytics/mentors/{mentorId} [get]
func (h *AnalyticsHandlers) Mentor(w http.ResponseWriter, r *http.Request) {
	mentorID, ok := pathID(w, r, "mentorId", "mentor")
	if !ok {
		return
	}
	from, to, ok := queryPeriod(w, r)
	if !ok {
		return
	}
	report, err := h.svc.Mentor(r.Context(), mentorID, from, to)
	if err != nil {
		writeAnalyticsError(w, "Mentor", err)
		return
	}
	feedback := make([]dto.FeedbackDimensionResponse, len(report.Feedback))
	for i, f := range report.Feedback {
		feedback[i] = dto.FeedbackDimensionResponse{Dimension: f.Dimension, Average: f.Average, Count: f.Count}
	}
	writeJSON(w, http.StatusOK, dto.MentorAnalyticsResponse{
		Mentor:                dto.AnalyticsPersonResponse{ID: report.Mentor.ID, Name: report.Mentor.Name},
		Period:                periodResponse(report.Period),
		GeneratedAt:           report.GeneratedAt,
		FeedbackByDimension:   feedback,
		StudentStandingByWeek: weekResponses(report.Weeks),
		StandingAttribution:   report.Attribution,
	})
}

// queryPeriod reads optional from/to dates. A missing value stays nil so the service applies its default.
func queryPeriod(w http.ResponseWriter, r *http.Request) (*time.Time, *time.Time, bool) {
	q := r.URL.Query()
	from, ok := queryDate(w, q.Get("from"), "from")
	if !ok {
		return nil, nil, false
	}
	to, ok := queryDate(w, q.Get("to"), "to")
	if !ok {
		return nil, nil, false
	}
	return from, to, true
}

func queryDate(w http.ResponseWriter, raw, name string) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	t, err := time.ParseInLocation("2006-01-02", raw, service.SchoolLocation)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", name+" must be a date in YYYY-MM-DD format", nil)
		return nil, false
	}
	return &t, true
}

// queryPage reads limit/offset, using the analytics defaults and bounds.
func queryPage(w http.ResponseWriter, r *http.Request) (limit, offset int, ok bool) {
	limit, offset = 20, 0
	q := r.URL.Query()
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 100 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 100", nil)
			return 0, 0, false
		}
		limit = v
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			writeError(w, http.StatusBadRequest, "bad_request", "offset must be zero or more", nil)
			return 0, 0, false
		}
		offset = v
	}
	return limit, offset, true
}

func pathID(w http.ResponseWriter, r *http.Request, name, label string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid "+label+" ID", nil)
		return 0, false
	}
	return id, true
}

func periodResponse(p service.AnalyticsPeriod) dto.AnalyticsPeriodResponse {
	return dto.AnalyticsPeriodResponse{From: p.From.Format("2006-01-02"), To: p.To.Format("2006-01-02")}
}

func weekResponses(weeks []store.ModuleStanding) []dto.AnalyticsWeekResponse {
	out := make([]dto.AnalyticsWeekResponse, len(weeks))
	for i, m := range weeks {
		out[i] = dto.AnalyticsWeekResponse{
			ModuleID: m.ModuleID, ModuleTitle: m.ModuleTitle, OpensAt: m.OpensAt,
			GreenCount: m.Green, YellowCount: m.Yellow, RedCount: m.Red, TotalCount: m.Total,
		}
	}
	return out
}

func writeAnalyticsError(w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, service.ErrAnalyticsPeriodInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", "from must not be after to", nil)
	case errors.Is(err, service.ErrAnalyticsPeriodTooLong):
		writeError(w, http.StatusBadRequest, "bad_request", "the reporting period must be at most 366 days", nil)
	case errors.Is(err, store.ErrModuleNotFound):
		writeError(w, http.StatusNotFound, "not_found", "module not found", nil)
	case errors.Is(err, store.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user not found", nil)
	default:
		log.Printf("analytics handler: %s error: %v", op, err)
		writeError(w, http.StatusInternalServerError, "server_error", "could not load analytics", nil)
	}
}
