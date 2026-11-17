package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

type fakeAnalyticsSummaryService struct {
	result *service.SummaryResult
	err    error

	lastID    int64
	lastFrom  *time.Time
	lastTo    *time.Time
	lastForce bool
}

func (f *fakeAnalyticsSummaryService) Student(_ context.Context, id int64, from, to *time.Time, force bool) (*service.SummaryResult, error) {
	f.lastID, f.lastFrom, f.lastTo, f.lastForce = id, from, to, force
	return f.result, f.err
}

func (f *fakeAnalyticsSummaryService) Mentor(_ context.Context, id int64, from, to *time.Time, force bool) (*service.SummaryResult, error) {
	f.lastID, f.lastFrom, f.lastTo, f.lastForce = id, from, to, force
	return f.result, f.err
}

func analyticsSummaryServer(svc AnalyticsSummaryService) *http.ServeMux {
	h := NewAnalyticsSummaryHandlers(svc)
	a := &middleware.Authenticator{Secret: reviewSecret}
	mux := http.NewServeMux()
	mux.Handle("POST /analytics/students/{studentId}/ai-summary", a.RequireRole(http.HandlerFunc(h.Student), "admin"))
	mux.Handle("POST /analytics/mentors/{mentorId}/ai-summary", a.RequireRole(http.HandlerFunc(h.Mentor), "admin"))
	return mux
}

func summaryPeriod() service.AnalyticsPeriod {
	return service.AnalyticsPeriod{
		From: time.Date(2026, 9, 1, 0, 0, 0, 0, service.SchoolLocation),
		To:   time.Date(2026, 11, 16, 0, 0, 0, 0, service.SchoolLocation),
	}
}

func generatedSummary() *service.SummaryResult {
	at := time.Date(2026, 11, 16, 8, 30, 0, 0, time.UTC)
	return &service.SummaryResult{
		Summary: "Recurring feedback praises clear explanations.", SourceCount: 5,
		GeneratedAt: &at, Period: summaryPeriod(),
	}
}

func TestAnalyticsSummaryResponse(t *testing.T) {
	svc := &fakeAnalyticsSummaryService{result: generatedSummary()}
	for _, body := range []string{``, `{}`} {
		rec, obj := call(t, analyticsSummaryServer(svc), "POST", "/analytics/mentors/7/ai-summary", admin, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("body %q: status %d: %v", body, rec.Code, obj)
		}
	}
}

func TestAnalyticsSummaryStudentResponse(t *testing.T) {
	svc := &fakeAnalyticsSummaryService{result: generatedSummary()}
	rec, obj := call(t, analyticsSummaryServer(svc), "POST", "/analytics/students/10/ai-summary", admin,
		`{"from":"2026-09-01","to":"2026-11-16","force_refresh":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %v", rec.Code, obj)
	}
	if obj["summary"] != "Recurring feedback praises clear explanations." || obj["source_count"] != float64(5) ||
		obj["cached"] != false || obj["generated_at"] != "2026-11-16T08:30:00Z" {
		t.Fatalf("body: %v", obj)
	}
	period, _ := obj["period"].(map[string]any)
	if period["from"] != "2026-09-01" || period["to"] != "2026-11-16" {
		t.Fatalf("period: %v", obj["period"])
	}
	if svc.lastID != 10 || !svc.lastForce || svc.lastFrom == nil || svc.lastTo == nil {
		t.Fatalf("forwarded: id %d, from %v, to %v, force %v", svc.lastID, svc.lastFrom, svc.lastTo, svc.lastForce)
	}
}

func TestAnalyticsSummaryMentorNoFeedback(t *testing.T) {
	svc := &fakeAnalyticsSummaryService{result: &service.SummaryResult{
		Summary: service.MentorSummaryNoFeedback, Period: summaryPeriod(),
	}}
	rec, obj := call(t, analyticsSummaryServer(svc), "POST", "/analytics/mentors/7/ai-summary", admin, `{}`)

	if rec.Code != http.StatusOK || obj["generated_at"] != nil || obj["source_count"] != float64(0) {
		t.Fatalf("status %d, body %v", rec.Code, obj)
	}
	if svc.lastFrom != nil || svc.lastTo != nil || svc.lastForce {
		t.Fatalf("defaults: from %v, to %v, force %v", svc.lastFrom, svc.lastTo, svc.lastForce)
	}
}

func TestAnalyticsSummaryRoutesAreAdminOnly(t *testing.T) {
	svc := &fakeAnalyticsSummaryService{result: generatedSummary()}
	mux := analyticsSummaryServer(svc)
	paths := []string{"/analytics/students/10/ai-summary", "/analytics/mentors/7/ai-summary"}

	for _, path := range paths {
		for _, roles := range [][]string{{"student"}, {"mentor"}, {"lecturer"}} {
			if rec, _ := call(t, mux, "POST", path, roles, `{}`); rec.Code != http.StatusForbidden {
				t.Errorf("%s as %v: %d", path, roles, rec.Code)
			}
		}
		if rec, _ := call(t, mux, "POST", path, nil, `{}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s unauthenticated: %d", path, rec.Code)
		}
		if rec, _ := call(t, mux, "POST", path, []string{"student", "admin"}, `{}`); rec.Code != http.StatusOK {
			t.Errorf("%s multi-role admin: %d", path, rec.Code)
		}
	}
}

func TestAnalyticsSummaryBadRequests(t *testing.T) {
	svc := &fakeAnalyticsSummaryService{result: generatedSummary()}
	mux := analyticsSummaryServer(svc)

	for _, body := range []string{`{`, `{"from":"not-a-date"}`, `{"to":"16-11-2026"}`} {
		if rec, _ := call(t, mux, "POST", "/analytics/students/10/ai-summary", admin, body); rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: %d", body, rec.Code)
		}
	}
	if rec, _ := call(t, mux, "POST", "/analytics/students/abc/ai-summary", admin, `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: %d", rec.Code)
	}
}

func TestAnalyticsSummaryErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{store.ErrUserNotFound, http.StatusNotFound},
		{service.ErrAnalyticsPeriodInvalid, http.StatusBadRequest},
		{service.ErrAnalyticsPeriodTooLong, http.StatusBadRequest},
		{service.ErrSummaryProviderUnavailable, http.StatusServiceUnavailable},
		{service.ErrSummaryProviderInvalid, http.StatusBadGateway},
		{service.ErrSummaryOutputInvalid, http.StatusBadGateway},
		{service.ErrSummaryInputTooLarge, http.StatusBadGateway},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		svc := &fakeAnalyticsSummaryService{err: c.err}
		rec, _ := call(t, analyticsSummaryServer(svc), "POST", "/analytics/students/10/ai-summary", admin, `{}`)
		if rec.Code != c.want {
			t.Errorf("%v: %d, want %d", c.err, rec.Code, c.want)
		}
	}
}
