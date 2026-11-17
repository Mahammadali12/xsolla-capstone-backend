package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// lfhFake answers like the service and records what the handlers pass on.
type lfhFake struct {
	err       error
	gotSub    *service.LectureFeedbackSubmission
	gotFilter store.LectureFeedbackFilter
}

var lfhEnd = time.Date(2026, 11, 12, 17, 0, 0, 0, time.UTC)

func lfhLecture() store.Lecture {
	week, title := int64(3), "Week 3"
	return store.Lecture{ID: 5, Title: "Goroutines", StartsAt: lfhEnd.Add(-2 * time.Hour), EndsAt: lfhEnd, ModuleID: &week, ModuleTitle: &title}
}

func (f *lfhFake) Submit(_ context.Context, _ int64, _ []string, sub service.LectureFeedbackSubmission) (*store.SentLectureFeedback, error) {
	f.gotSub = &sub
	if f.err != nil {
		return nil, f.err
	}
	return &store.SentLectureFeedback{EventID: *sub.EventID, SubmittedAt: lfhEnd}, nil
}
func (f *lfhFake) Sent(context.Context, int64, []string) ([]store.SentLectureFeedback, error) {
	return []store.SentLectureFeedback{{EventID: 5, SubmittedAt: lfhEnd}}, f.err
}
func (f *lfhFake) Pending(context.Context, int64, []string) ([]store.Lecture, error) {
	return []store.Lecture{lfhLecture()}, f.err
}
func (f *lfhFake) List(_ context.Context, filter store.LectureFeedbackFilter) ([]store.LectureFeedbackRow, map[int64][]store.LectureFeedbackPerson, error) {
	f.gotFilter = filter
	improve := "slower please"
	return []store.LectureFeedbackRow{{ID: 1, Lecture: lfhLecture(), StudentID: 9, StudentName: "Anna", StudentEmail: "anna@x.az",
			Overall: 4, Clarity: 5, Practice: 3, Pace: "too_fast", Confidence: 2, Improve: &improve, SubmittedAt: lfhEnd}},
		map[int64][]store.LectureFeedbackPerson{3: {{ID: 20, Name: "Leyla", Email: "leyla@x.az"}}}, f.err
}
func (f *lfhFake) Summary(_ context.Context, filter store.LectureFeedbackFilter) ([]service.LectureFeedbackSummary, error) {
	f.gotFilter = filter
	avg := 4.0
	return []service.LectureFeedbackSummary{{Lecture: lfhLecture(), Lecturers: []store.LectureFeedbackPerson{}, Responses: 1, Expected: 12,
		Averages: map[string]*float64{"overall": &avg, "clarity": nil}, Pace: map[string]int{"too_fast": 1}, WithComments: 1}}, f.err
}

var lfhSecret = []byte("lecture-feedback-test-secret")

func lfhServer(f *lfhFake) *http.ServeMux {
	h := NewLectureFeedbackHandlers(f)
	a := &middleware.Authenticator{Secret: lfhSecret}
	mux := http.NewServeMux()
	mux.Handle("GET /lecture-feedback/form", a.Middleware(http.HandlerFunc(h.Form)))
	mux.Handle("GET /lecture-feedback/pending", a.Middleware(http.HandlerFunc(h.Pending)))
	mux.Handle("GET /lecture-feedback/me", a.Middleware(http.HandlerFunc(h.Mine)))
	mux.Handle("POST /lecture-feedback", a.Middleware(http.HandlerFunc(h.Submit)))
	mux.Handle("GET /lecture-feedback", a.RequireRole(http.HandlerFunc(h.List), "admin"))
	mux.Handle("GET /lecture-feedback/summary", a.RequireRole(http.HandlerFunc(h.Summary), "admin"))
	return mux
}

func lfhCall(t *testing.T, mux http.Handler, method, path string, roles []string, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if roles != nil {
		token, err := auth.AccessToken(9, roles, lfhSecret, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var obj map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &obj)
	return rec, obj
}

const lfhForm = `{"event_id": 5, "overall": 4, "clarity": 5, "practice": 3, "pace": "too_fast", "confidence": 2, "improve": "slower please"}`

func TestLectureFeedbackFormRoute(t *testing.T) {
	rec, obj := lfhCall(t, lfhServer(&lfhFake{}), "GET", "/lecture-feedback/form", []string{"lecturer"}, "")
	questions, _ := obj["questions"].([]any)
	if rec.Code != 200 || len(questions) != 7 || obj["opens_minutes_before_end"] != float64(10) || obj["open_days_after_end"] != float64(7) {
		t.Errorf("form: %d %s", rec.Code, rec.Body)
	}
	first, _ := questions[0].(map[string]any)
	if first["id"] != "overall" || first["type"] != "scale" || first["min_label"] != "Very bad" || first["required"] != true {
		t.Errorf("first question: %v", first)
	}
}

func TestLectureFeedbackStudentRoutes(t *testing.T) {
	f := &lfhFake{}
	mux := lfhServer(f)
	students := []string{"student"}

	rec, obj := lfhCall(t, mux, "POST", "/lecture-feedback", students, lfhForm)
	if rec.Code != 201 || obj["event_id"] != float64(5) || f.gotSub == nil || *f.gotSub.Pace != "too_fast" || *f.gotSub.Improve != "slower please" || f.gotSub.Liked != nil {
		t.Errorf("send: %d %s %+v", rec.Code, rec.Body, f.gotSub)
	}
	rec, _ = lfhCall(t, mux, "GET", "/lecture-feedback/pending", students, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"event_id":5`) || !strings.Contains(rec.Body.String(), `"module_title":"Week 3"`) ||
		!strings.Contains(rec.Body.String(), `"closes_at":"2026-11-19T17:00:00Z"`) {
		t.Errorf("pending: %d %s", rec.Code, rec.Body)
	}
	rec, _ = lfhCall(t, mux, "GET", "/lecture-feedback/me", students, "")
	if want := `[{"event_id":5,"submitted_at":"2026-11-12T17:00:00Z"}]`; rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("mine: %d %s", rec.Code, rec.Body)
	}

	for name, tc := range map[string]struct {
		body   string
		err    error
		status int
		code   string
	}{
		"not JSON":       {`{"event_id": 5,`, nil, 400, "bad_request"},
		"two objects":    {lfhForm + "{}", nil, 400, "bad_request"},
		"text for a 1-5": {strings.Replace(lfhForm, `"overall": 4`, `"overall": "4"`, 1), nil, 422, "validation_failed"},
		"broken rules":   {lfhForm, &service.LectureFeedbackInvalidError{Problems: []service.LectureFeedbackProblem{{Field: "pace", Message: "pace is required"}}}, 422, "validation_failed"},
		"staff":          {lfhForm, service.ErrLectureFeedbackForbidden, 403, "forbidden"},
		"unknown":        {lfhForm, service.ErrLectureFeedbackNotFound, 404, "not_found"},
		"sent before":    {lfhForm, service.ErrLectureFeedbackExists, 409, "already_sent"},
		"not open":       {lfhForm, service.ErrLectureFeedbackNotOpen, 422, "validation_failed"},
		"closed":         {lfhForm, service.ErrLectureFeedbackClosed, 422, "validation_failed"},
		"database down":  {lfhForm, errors.New("connection refused"), 500, "server_error"},
		"too large":      {`{"improve": "` + strings.Repeat("x", 20000) + `"}`, nil, 413, "payload_too_large"},
	} {
		f.err = tc.err
		rec, obj := lfhCall(t, mux, "POST", "/lecture-feedback", students, tc.body)
		if rec.Code != tc.status || obj["error"] != tc.code {
			t.Errorf("%s: %d %s, want %d %s", name, rec.Code, rec.Body, tc.status, tc.code)
		}
	}
	f.err = nil
	if rec, _ := lfhCall(t, mux, "POST", "/lecture-feedback", nil, lfhForm); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}
}

func TestLectureFeedbackAdminRoutes(t *testing.T) {
	f := &lfhFake{}
	mux := lfhServer(f)
	admin := []string{"admin"}

	for _, path := range []string{"/lecture-feedback", "/lecture-feedback/summary"} {
		for _, roles := range [][]string{{"student"}, {"mentor"}, {"lecturer"}} {
			if rec, _ := lfhCall(t, mux, "GET", path, roles, ""); rec.Code != 403 {
				t.Errorf("%s as %v: %d, want 403", path, roles, rec.Code)
			}
		}
	}

	rec, _ := lfhCall(t, mux, "GET", "/lecture-feedback?module_id=3&from=2026-11-01&to=2026-11-12", admin, "")
	body := rec.Body.String()
	for _, want := range []string{`"lecture":{"event_id":5,"title":"Goroutines"`, `"lecturers":[{"id":20,"name":"Leyla","email":"leyla@x.az"}]`,
		`"student":{"id":9,"name":"Anna","email":"anna@x.az"}`, `"pace":"too_fast"`, `"liked":null`, `"improve":"slower please"`} {
		if rec.Code != 200 || !strings.Contains(body, want) {
			t.Errorf("list lacks %s: %d %s", want, rec.Code, body)
		}
	}
	from, to := f.gotFilter.From, f.gotFilter.To
	if f.gotFilter.ModuleID == nil || *f.gotFilter.ModuleID != 3 || from == nil || to == nil ||
		from.Format(time.RFC3339) != "2026-11-01T00:00:00+04:00" || to.Format(time.RFC3339) != "2026-11-13T00:00:00+04:00" {
		t.Errorf("filter: %+v (the whole 'to' day in Baku time must be included)", f.gotFilter)
	}

	rec, _ = lfhCall(t, mux, "GET", "/lecture-feedback/summary?event_id=5", admin, "")
	for _, want := range []string{`"responses":1`, `"expected":12`, `"overall":4`, `"clarity":null`, `"with_comments":1`, `"lecturers":[]`} {
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("summary lacks %s: %d %s", want, rec.Code, rec.Body)
		}
	}

	for q, wantWord := range map[string]string{
		"event_id=abc": "event_id", "module_id=-1": "module_id", "from=2026-13-01": "from", "to=14.11.2026": "to",
		"from=2026-11-20&to=2026-11-10": "after",
	} {
		rec, obj := lfhCall(t, mux, "GET", "/lecture-feedback/summary?"+q, admin, "")
		if rec.Code != 400 || !strings.Contains(obj["message"].(string), wantWord) {
			t.Errorf("%s: %d %s, want 400 mentioning %s", q, rec.Code, rec.Body, wantWord)
		}
	}
}
