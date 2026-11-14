package handlers

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// adminReviewServer registers the read routes the way main.go does: admins only.
func adminReviewServer(s *reviewStore) *http.ServeMux {
	h := NewMentorReviewHandlers(service.NewMentorReviewService(s))
	a := &middleware.Authenticator{Secret: reviewSecret}
	mux := http.NewServeMux()
	mux.Handle("GET /mentor-reviews", a.RequireRole(http.HandlerFunc(h.List), "admin"))
	mux.Handle("GET /mentor-reviews/summary", a.RequireRole(http.HandlerFunc(h.Summary), "admin"))
	mux.Handle("GET /mentor-reviews/missing", a.RequireRole(http.HandlerFunc(h.Missing), "admin"))
	return mux
}

var admin = []string{"admin"}

func adminRows() []store.MentorReviewRow {
	reason, other, comment, team := "not-scheduled", "CV review", "More code reviews please", "Team A"
	teamID := int64(3)
	at := time.Date(2026, 11, 14, 10, 12, 0, 0, time.UTC)
	return []store.MentorReviewRow{
		{ID: 1, ModuleID: 4, ModuleTitle: "Week 4", StudentID: 10, StudentName: "Anna", StudentEmail: "anna@x.az",
			MentorID: 7, MentorName: "Mira", MentorEmail: "mira@x.az", TeamID: &teamID, TeamName: &team,
			HadSession: true, Topics: []string{"goals", "other"}, OtherTopic: &other,
			Ratings: &store.MentorReviewRatings{Clarity: 5, Engagement: 4, Comfort: 5, Time: 3}, Overall: 4, Comment: &comment, SubmittedAt: at},
		{ID: 2, ModuleID: 4, ModuleTitle: "Week 4", StudentID: 11, StudentName: "Bek", StudentEmail: "bek@x.az",
			MentorID: 7, MentorName: "Mira", MentorEmail: "mira@x.az",
			HadSession: false, NoSessionReason: &reason, Topics: []string{}, Overall: 2, SubmittedAt: at},
	}
}

func TestAdminReviewRoutesAreAdminOnly(t *testing.T) {
	mux := adminReviewServer(openReviewStore())
	for _, path := range []string{"/mentor-reviews", "/mentor-reviews/summary", "/mentor-reviews/missing?module_id=4"} {
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

func TestAdminReviewList(t *testing.T) {
	s := openReviewStore()
	s.rows = adminRows()
	rec, _ := call(t, adminReviewServer(s), "GET", "/mentor-reviews?module_id=4&mentor_id=7&team_id=", admin, "")
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"student":{"id":10,"name":"Anna","email":"anna@x.az"}`,
		`"mentor":{"id":7,"name":"Mira","email":"mira@x.az"}`,
		`"team":{"id":3,"name":"Team A"}`,
		`"ratings":{"clarity":5,"engagement":4,"comfort":5,"time":3}`,
		`"other_topic":"CV review"`,
		// the review without a session: no team left, no ratings, empty topics, a reason
		`"team":null`, `"ratings":null`, `"topics":[]`, `"no_session_reason":"not-scheduled"`, `"comment":null`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("response lacks %s:\n%s", want, body)
		}
	}

	s.rows = nil
	if rec, _ := call(t, adminReviewServer(s), "GET", "/mentor-reviews", admin, ""); strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("no reviews: %s, want []", rec.Body)
	}
	for _, q := range []string{"module_id=abc", "mentor_id=0", "team_id=-2", "module_id=4.5"} {
		rec, obj := call(t, adminReviewServer(s), "GET", "/mentor-reviews?"+q, admin, "")
		if rec.Code != 400 || !strings.Contains(obj["message"].(string), strings.SplitN(q, "=", 2)[0]) {
			t.Errorf("%s: %d %s, want 400 naming the parameter", q, rec.Code, rec.Body)
		}
	}
}

func TestAdminReviewSummary(t *testing.T) {
	s := openReviewStore()
	s.rows = adminRows()
	s.expected = []store.ExpectedReviewer{{ModuleID: 4, ModuleTitle: "Week 4", MentorID: 7, MentorName: "Mira", MentorEmail: "mira@x.az"}}
	rec, _ := call(t, adminReviewServer(s), "GET", "/mentor-reviews/summary?from_module=4&to_module=4", admin, "")
	if rec.Code != 200 {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`"module_id":4`, `"mentor":{"id":7,"name":"Mira","email":"mira@x.az"}`,
		`"reviews":2`, `"expected":1`, `"sessions_held":1`, `"no_session":1`,
		`"overall":3`, `"clarity":5`, `"goals":1`, `"homework":0`, `"not-scheduled":1`, `"mentor-unavailable":0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("summary lacks %s:\n%s", want, body)
		}
	}

	if rec, _ := call(t, adminReviewServer(s), "GET", "/mentor-reviews/summary?from_module=99", admin, ""); rec.Code != 404 {
		t.Errorf("unknown from_module: %d, want 404", rec.Code)
	}
	if rec, _ := call(t, adminReviewServer(s), "GET", "/mentor-reviews/summary?to_module=x", admin, ""); rec.Code != 400 {
		t.Errorf("to_module=x: %d, want 400", rec.Code)
	}
}

func TestAdminReviewMissing(t *testing.T) {
	s := openReviewStore()
	s.expected = []store.ExpectedReviewer{
		{ModuleID: 4, StudentID: 10, StudentName: "Anna", TeamID: 3, TeamName: "Team A", MentorID: 7, MentorName: "Mira", Reviewed: true},
		{ModuleID: 4, StudentID: 11, StudentName: "Bek", StudentEmail: "bek@x.az", TeamID: 3, TeamName: "Team A", MentorID: 7, MentorName: "Mira", MentorEmail: "mira@x.az"},
	}
	rec, _ := call(t, adminReviewServer(s), "GET", "/mentor-reviews/missing?module_id=4", admin, "")
	want := `[{"student":{"id":11,"name":"Bek","email":"bek@x.az"},"team":{"id":3,"name":"Team A"},"mentor":{"id":7,"name":"Mira","email":"mira@x.az"}}]`
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("missing: %d %s\nwant %s", rec.Code, rec.Body, want)
	}
	for q, status := range map[string]int{"": 400, "?module_id=": 400, "?module_id=x": 400, "?module_id=99": 404} {
		if rec, _ := call(t, adminReviewServer(s), "GET", "/mentor-reviews/missing"+q, admin, ""); rec.Code != status {
			t.Errorf("missing%s: %d, want %d", q, rec.Code, status)
		}
	}
}
