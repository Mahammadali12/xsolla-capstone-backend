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

// reviewStore is an in-memory store for the real MentorReviewService.
type reviewStore struct {
	module    *store.Module
	enrolled  bool
	reviewed  map[int64]time.Time // module id -> submitted at, for the one student
	createErr error
	listErr   error
}

func (s *reviewStore) GetModuleByID(_ context.Context, id int64) (*store.Module, error) {
	if s.module == nil || s.module.ID != id {
		return nil, store.ErrModuleNotFound
	}
	return s.module, nil
}
func (s *reviewStore) IsUserEnrolled(context.Context, int64, int64) (bool, error) {
	return s.enrolled, nil
}
func (s *reviewStore) HasMentorReview(_ context.Context, moduleID, _ int64) (bool, error) {
	_, ok := s.reviewed[moduleID]
	return ok, nil
}
func (s *reviewStore) CreateMentorReview(_ context.Context, in store.MentorReviewInput) (*store.MentorReview, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	at := time.Date(2026, 11, 14, 10, 12, 0, 0, time.UTC)
	s.reviewed[in.ModuleID] = at
	return &store.MentorReview{ID: 1, ModuleID: in.ModuleID, StudentID: in.StudentID, MentorID: 3, SubmittedAt: at}, nil
}
func (s *reviewStore) ListReviewedModules(context.Context, int64) ([]store.ReviewedModule, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	out := []store.ReviewedModule{}
	for id, at := range s.reviewed {
		out = append(out, store.ReviewedModule{ModuleID: id, SubmittedAt: at})
	}
	return out, nil
}

// openReviewStore: module 4 closed an hour ago, so its review is open; the student is enrolled.
func openReviewStore() *reviewStore {
	now := time.Now()
	return &reviewStore{
		module:   &store.Module{ID: 4, OpensAt: now.Add(-7 * 24 * time.Hour), ClosesAt: now.Add(-time.Hour)},
		enrolled: true,
		reviewed: map[int64]time.Time{},
	}
}

var reviewSecret = []byte("mentor-review-test-secret")

func reviewServer(s *reviewStore) *http.ServeMux {
	h := NewMentorReviewHandlers(service.NewMentorReviewService(s))
	a := &middleware.Authenticator{Secret: reviewSecret}
	mux := http.NewServeMux()
	mux.Handle("GET /mentor-reviews/me", a.Middleware(http.HandlerFunc(h.ListMine)))
	mux.Handle("POST /mentor-reviews", a.Middleware(http.HandlerFunc(h.Submit)))
	return mux
}

func call(t *testing.T, mux *http.ServeMux, method, path string, roles []string, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if roles != nil {
		token, err := auth.AccessToken(10, roles, reviewSecret, time.Minute)
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

const validReview = `{"module_id": 4, "had_session": true, "topics": ["goals", "other"], "other_topic": "CV review",
  "ratings": {"clarity": 5, "engagement": 4, "comfort": 5, "time": 3}, "overall": 4, "comment": "More code reviews please"}`

const noSessionReview = `{"module_id": 4, "had_session": false, "no_session_reason": "not-scheduled", "overall": 2}`

var student = []string{"student"}

func TestPostMentorReviewCreated(t *testing.T) {
	for name, body := range map[string]string{"with session": validReview, "without session": noSessionReview} {
		t.Run(name, func(t *testing.T) {
			rec, obj := call(t, reviewServer(openReviewStore()), "POST", "/mentor-reviews", student, body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status %d, body %s", rec.Code, rec.Body)
			}
			if obj["module_id"] != float64(4) || obj["submitted_at"] != "2026-11-14T10:12:00Z" || len(obj) != 2 {
				t.Errorf("body %s, want only module_id 4 and submitted_at", rec.Body)
			}
		})
	}
}

func TestPostMentorReviewErrors(t *testing.T) {
	notOpen := func() *reviewStore {
		s := openReviewStore()
		s.module.ClosesAt = time.Now().Add(10 * 24 * time.Hour) // the last weekend before it is at least 2 days away
		return s
	}
	with := func(edit func(*reviewStore)) func() *reviewStore {
		return func() *reviewStore { s := openReviewStore(); edit(s); return s }
	}

	cases := []struct {
		name     string
		store    func() *reviewStore
		roles    []string
		body     string
		status   int
		code     string
		contains string
	}{
		{"no token", openReviewStore, nil, validReview, 401, "missing token", ""},
		{"mentor", openReviewStore, []string{"mentor"}, validReview, 403, "forbidden", ""},
		{"student who is also staff", openReviewStore, []string{"student", "lecturer"}, validReview, 403, "forbidden", ""},
		{"not JSON", openReviewStore, student, `{"module_id": 4,`, 400, "bad_request", ""},
		{"two JSON objects", openReviewStore, student, validReview + `{}`, 400, "bad_request", ""},
		{"too large", openReviewStore, student, `{"comment": "` + strings.Repeat("x", 20000) + `"}`, 413, "payload_too_large", ""},
		{"overall as text", openReviewStore, student, `{"module_id": 4, "had_session": false, "no_session_reason": "other", "overall": "2"}`, 422, "validation_failed", "overall"},
		{"rating not whole", openReviewStore, student, strings.Replace(validReview, `"time": 3`, `"time": 3.5`, 1), 422, "validation_failed", "ratings"},
		{"module_id as text", openReviewStore, student, strings.Replace(noSessionReview, `"module_id": 4`, `"module_id": "4"`, 1), 422, "validation_failed", "module_id"},
		{"broken rules", openReviewStore, student, `{"module_id": 4, "had_session": true, "overall": 9}`, 422, "validation_failed", "topic"},
		{"unknown module", openReviewStore, student, strings.Replace(validReview, `"module_id": 4`, `"module_id": 5`, 1), 404, "not_found", ""},
		{"not enrolled", with(func(s *reviewStore) { s.enrolled = false }), student, validReview, 404, "not_found", ""},
		{"already sent", with(func(s *reviewStore) { s.reviewed[4] = time.Now() }), student, validReview, 409, "already_reviewed", ""},
		{"week not open yet", notOpen, student, validReview, 422, "validation_failed", "not open yet"},
		{"no team", with(func(s *reviewStore) { s.createErr = store.ErrMentorReviewNoTeam }), student, validReview, 422, "validation_failed", "not in a team"},
		{"no mentor", with(func(s *reviewStore) { s.createErr = store.ErrMentorReviewNoMentor }), student, validReview, 422, "validation_failed", "no mentor"},
		{"sent twice at the same moment", with(func(s *reviewStore) { s.createErr = store.ErrMentorReviewExists }), student, validReview, 409, "already_reviewed", ""},
		{"database down", with(func(s *reviewStore) { s.createErr = errors.New("connection refused") }), student, validReview, 500, "server_error", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, obj := call(t, reviewServer(tc.store()), "POST", "/mentor-reviews", tc.roles, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status %d, want %d; body %s", rec.Code, tc.status, rec.Body)
			}
			if obj["error"] != tc.code || obj["status"] != float64(tc.status) {
				t.Errorf("body %s, want error %q and status %d", rec.Body, tc.code, tc.status)
			}
			if !strings.Contains(rec.Body.String(), tc.contains) {
				t.Errorf("body %s does not mention %q", rec.Body, tc.contains)
			}
		})
	}
}

func TestPostMentorReviewListsEveryBrokenRule(t *testing.T) {
	rec, obj := call(t, reviewServer(openReviewStore()), "POST", "/mentor-reviews", student, `{"had_session": true, "topics": ["goals", "gossip"], "ratings": {"clarity": 0}}`)
	if rec.Code != 422 {
		t.Fatalf("status %d", rec.Code)
	}
	var fields []string
	for _, d := range obj["details"].([]any) {
		fields = append(fields, d.(map[string]any)["field"].(string))
	}
	want := "module_id topics ratings.clarity ratings.engagement ratings.comfort ratings.time overall"
	if strings.Join(fields, " ") != want {
		t.Errorf("details fields %v, want %s", fields, want)
	}
	if obj["message"] != "module_id is required" {
		t.Errorf("message %q, want the first broken rule", obj["message"])
	}
}

func TestGetMyMentorReviews(t *testing.T) {
	s := openReviewStore()
	mux := reviewServer(s)

	rec, _ := call(t, mux, "GET", "/mentor-reviews/me", student, "")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("no reviews: status %d body %s, want 200 []", rec.Code, rec.Body)
	}

	call(t, mux, "POST", "/mentor-reviews", student, validReview)
	rec, _ = call(t, mux, "GET", "/mentor-reviews/me", student, "")
	if want := `[{"module_id":4,"submitted_at":"2026-11-14T10:12:00Z"}]`; strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("after one review: body %s, want %s", rec.Body, want)
	}

	if rec, _ := call(t, mux, "GET", "/mentor-reviews/me", []string{"admin"}, ""); rec.Code != 403 {
		t.Errorf("admin: status %d, want 403", rec.Code)
	}
	if rec, _ := call(t, mux, "GET", "/mentor-reviews/me", nil, ""); rec.Code != 401 {
		t.Errorf("no token: status %d, want 401", rec.Code)
	}

	s.listErr = errors.New("connection refused")
	if rec, _ := call(t, mux, "GET", "/mentor-reviews/me", student, ""); rec.Code != 500 {
		t.Errorf("database down: status %d, want 500", rec.Code)
	}
}
