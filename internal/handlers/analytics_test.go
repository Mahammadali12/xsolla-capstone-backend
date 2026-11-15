package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

type fakeAnalyticsService struct {
	overview *service.OverviewReport
	student  *service.StudentReport
	mentor   *service.MentorReport
	people   []store.AnalyticsPerson
	err      error

	lastModuleID *int64
	lastQ        string
	lastLimit    int
	lastOffset   int
}

func (f *fakeAnalyticsService) Overview(context.Context, *time.Time, *time.Time) (*service.OverviewReport, error) {
	return f.overview, f.err
}

func (f *fakeAnalyticsService) Student(_ context.Context, _ int64, _, _ *time.Time, moduleID *int64) (*service.StudentReport, error) {
	f.lastModuleID = moduleID
	return f.student, f.err
}

func (f *fakeAnalyticsService) Mentor(context.Context, int64, *time.Time, *time.Time) (*service.MentorReport, error) {
	return f.mentor, f.err
}

func (f *fakeAnalyticsService) SearchStudents(_ context.Context, q string, limit, offset int) ([]store.AnalyticsPerson, error) {
	f.lastQ, f.lastLimit, f.lastOffset = q, limit, offset
	return f.people, f.err
}

func (f *fakeAnalyticsService) SearchMentors(_ context.Context, q string, limit, offset int) ([]store.AnalyticsPerson, error) {
	f.lastQ, f.lastLimit, f.lastOffset = q, limit, offset
	return f.people, f.err
}

func analyticsServer(svc AnalyticsService) *http.ServeMux {
	h := NewAnalyticsHandlers(svc)
	a := &middleware.Authenticator{Secret: reviewSecret}
	mux := http.NewServeMux()
	mux.Handle("GET /analytics/overview", a.RequireRole(http.HandlerFunc(h.Overview), "admin"))
	mux.Handle("GET /analytics/students", a.RequireRole(http.HandlerFunc(h.ListStudents), "admin"))
	mux.Handle("GET /analytics/students/{studentId}", a.RequireRole(http.HandlerFunc(h.Student), "admin"))
	mux.Handle("GET /analytics/mentors", a.RequireRole(http.HandlerFunc(h.ListMentors), "admin"))
	mux.Handle("GET /analytics/mentors/{mentorId}", a.RequireRole(http.HandlerFunc(h.Mentor), "admin"))
	return mux
}

func analyticsFake() *fakeAnalyticsService {
	now := time.Date(2026, 11, 14, 17, 0, 0, 0, time.UTC)
	period := service.AnalyticsPeriod{
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, service.SchoolLocation),
		To:   time.Date(2026, 11, 14, 0, 0, 0, 0, service.SchoolLocation),
	}
	week := store.ModuleStanding{ModuleID: 4, ModuleTitle: "Week 4", Green: 3, Yellow: 1, Red: 1, Total: 5}
	accuracy := 80.0
	return &fakeAnalyticsService{
		overview: &service.OverviewReport{Period: period, GeneratedAt: now, Weeks: []store.ModuleStanding{week}},
		student: &service.StudentReport{
			Student: store.AnalyticsPerson{ID: 10, Name: "Anna"}, Period: period, GeneratedAt: now,
			Status: store.AssignmentStatus{OnTime: 4, Late: 1, Missing: 2, Eligible: 7},
			Quizzes: []service.QuizAccuracy{
				{ModuleID: 4, ModuleTitle: "Week 4", ScoreTotal: 16, PossibleTotal: 20, Accuracy: &accuracy, AttemptCount: 2},
				{ModuleID: 5, ModuleTitle: "Week 5", ScoreTotal: 0, PossibleTotal: 0, AttemptCount: 1},
			},
		},
		mentor: &service.MentorReport{
			Mentor: store.AnalyticsPerson{ID: 7, Name: "Mira"}, Period: period, GeneratedAt: now,
			Feedback: []service.FeedbackDimension{
				{Dimension: "clarity", Average: &accuracy, Count: 5},
				{Dimension: "engagement", Average: &accuracy, Count: 5},
				{Dimension: "comfort", Average: &accuracy, Count: 0},
				{Dimension: "time"},
			},
			Weeks:       []store.ModuleStanding{week},
			Attribution: service.StandingAttributionCurrentRoster,
		},
		people: []store.AnalyticsPerson{{ID: 10, Name: "Anna", Email: "anna@x.az"}},
	}
}

func TestAnalyticsRoutesAreAdminOnly(t *testing.T) {
	mux := analyticsServer(analyticsFake())
	paths := []string{
		"/analytics/overview",
		"/analytics/students",
		"/analytics/students/10",
		"/analytics/mentors",
		"/analytics/mentors/7",
	}
	for _, path := range paths {
		for _, roles := range [][]string{{"student"}, {"mentor"}, {"lecturer"}, {"mentor", "student"}} {
			if rec, _ := call(t, mux, "GET", path, roles, ""); rec.Code != http.StatusForbidden {
				t.Errorf("%s as %v: %d, want 403", path, roles, rec.Code)
			}
		}
		if rec, _ := call(t, mux, "GET", path, nil, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a token: %d, want 401", path, rec.Code)
		}
		if rec, _ := call(t, mux, "GET", path, admin, ""); rec.Code != http.StatusOK {
			t.Errorf("%s as admin: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestAnalyticsOverviewResponse(t *testing.T) {
	rec, _ := call(t, analyticsServer(analyticsFake()), "GET", "/analytics/overview?from=2026-09-01&to=2026-11-14", admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{
		`"period":{"from":"2026-09-01","to":"2026-11-14"}`,
		`"module_id":4`, `"module_title":"Week 4"`,
		`"green_count":3`, `"yellow_count":1`, `"red_count":1`, `"total_count":5`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("overview lacks %s:\n%s", want, rec.Body)
		}
	}

	empty := analyticsFake()
	empty.overview.Weeks = nil
	rec, _ = call(t, analyticsServer(empty), "GET", "/analytics/overview", admin, "")
	if !strings.Contains(rec.Body.String(), `"weeks":[]`) {
		t.Errorf("no weeks: %s, want an empty array", rec.Body)
	}
}

func TestAnalyticsStudentResponse(t *testing.T) {
	svc := analyticsFake()
	rec, _ := call(t, analyticsServer(svc), "GET", "/analytics/students/10?module_id=4", admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{
		`"student":{"id":10,"name":"Anna"}`,
		`"on_time_count":4`, `"late_count":1`, `"missing_count":2`, `"eligible_count":7`,
		`"score_total":16`, `"possible_total":20`, `"accuracy":80`, `"attempt_count":2`,
		`"accuracy":null`, // a zero-total module
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("student response lacks %s:\n%s", want, rec.Body)
		}
	}
	if svc.lastModuleID == nil || *svc.lastModuleID != 4 {
		t.Errorf("module_id not passed to the service: %v", svc.lastModuleID)
	}

	for _, path := range []string{"/analytics/students/abc", "/analytics/students/0"} {
		if rec, _ := call(t, analyticsServer(analyticsFake()), "GET", path, admin, ""); rec.Code != 400 {
			t.Errorf("%s: %d, want 400", path, rec.Code)
		}
	}
	for _, query := range []string{"from=14-11-2026", "to=2026-13-01"} {
		if rec, _ := call(t, analyticsServer(analyticsFake()), "GET", "/analytics/students/10?"+query, admin, ""); rec.Code != 400 {
			t.Errorf("%s: %d, want 400", query, rec.Code)
		}
	}
	if rec, _ := call(t, analyticsServer(analyticsFake()), "GET", "/analytics/students/10?module_id=0", admin, ""); rec.Code != 400 {
		t.Errorf("module_id=0: %d, want 400", rec.Code)
	}
}

func TestAnalyticsMentorResponse(t *testing.T) {
	rec, _ := call(t, analyticsServer(analyticsFake()), "GET", "/analytics/mentors/7", admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{
		`"mentor":{"id":7,"name":"Mira"}`,
		`"dimension":"clarity"`, `"dimension":"engagement"`, `"dimension":"comfort"`, `"dimension":"time"`,
		`"average":null,"count":0`,
		`"student_standing_by_week":[`,
		`"standing_attribution":"current_roster"`,
	} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("mentor response lacks %s:\n%s", want, rec.Body)
		}
	}
	if rec, _ := call(t, analyticsServer(analyticsFake()), "GET", "/analytics/mentors/abc", admin, ""); rec.Code != 400 {
		t.Errorf("bad mentor id: %d, want 400", rec.Code)
	}
}

func TestAnalyticsDirectory(t *testing.T) {
	svc := analyticsFake()
	rec, _ := call(t, analyticsServer(svc), "GET", "/analytics/students?q=Ann&limit=5&offset=10", admin, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `{"id":10,"name":"Anna","email":"anna@x.az"}`) {
		t.Errorf("directory response: %s", rec.Body)
	}
	if svc.lastQ != "Ann" || svc.lastLimit != 5 || svc.lastOffset != 10 {
		t.Errorf("search params = %q %d %d", svc.lastQ, svc.lastLimit, svc.lastOffset)
	}

	for _, query := range []string{"limit=0", "limit=101", "limit=x", "offset=-1", "offset=x"} {
		path := "/analytics/mentors?" + query
		if rec, _ := call(t, analyticsServer(analyticsFake()), "GET", path, admin, ""); rec.Code != 400 {
			t.Errorf("%s: %d, want 400", query, rec.Code)
		}
	}
}

func TestAnalyticsErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{service.ErrAnalyticsPeriodInvalid, 400},
		{service.ErrAnalyticsPeriodTooLong, 400},
		{store.ErrUserNotFound, 404},
		{store.ErrModuleNotFound, 404},
		{errors.New("boom"), 500},
	}
	for _, c := range cases {
		svc := analyticsFake()
		svc.err = c.err
		rec, obj := call(t, analyticsServer(svc), "GET", "/analytics/students/10", admin, "")
		if rec.Code != c.code {
			t.Errorf("%v: %d, want %d", c.err, rec.Code, c.code)
			continue
		}
		if c.code == 500 {
			if obj["error"] != "server_error" {
				t.Errorf("%v: body %v", c.err, obj)
			}
			continue
		}
		if obj["error"] == nil || obj["status"] != float64(c.code) {
			t.Errorf("%v: body %v", c.err, obj)
		}
	}
}
