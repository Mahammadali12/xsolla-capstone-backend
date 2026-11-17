package handlers

import (
	"context"
	"encoding/json"
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

type rptFakeService struct {
	sentBy  int64
	filter  store.ReportFilter
	err     error
	updated int64
}

func (f *rptFakeService) Send(_ context.Context, reporterID int64, subject, message string) (*store.Report, error) {
	f.sentBy = reporterID
	return &store.Report{ID: 3, Subject: subject, Message: message, Status: "open", CreatedAt: time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)}, f.err
}

func (f *rptFakeService) List(_ context.Context, filter store.ReportFilter) ([]store.Report, error) {
	f.filter = filter
	id := int64(7)
	return []store.Report{{ID: 3, ReporterID: &id, Reporter: &store.UserBrief{ID: 7, Name: "Murad", Email: "murad@example.com"},
		Roles: []string{"mentor"}, Subject: "Student X", Message: "Rude", Status: "open"}}, f.err
}

func (f *rptFakeService) Get(context.Context, int64) (*store.Report, error) {
	return &store.Report{ID: 3, Status: "open"}, f.err
}

func (f *rptFakeService) Counts(context.Context) (int, int, error) { return 2, 5, f.err }

func (f *rptFakeService) Update(_ context.Context, _ int64, id int64, _, _ *string) (*store.Report, error) {
	f.updated = id
	return &store.Report{ID: id, Status: "resolved"}, f.err
}

func (f *rptFakeService) Delete(context.Context, int64) error { return f.err }

// rptServe registers the routes exactly as cmd/server/main.go does.
func rptServe(t *testing.T, svc *rptFakeService, role, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	secret := []byte("report-test-secret")
	h := NewReportHandlers(svc)
	a := &middleware.Authenticator{Secret: secret}
	mux := http.NewServeMux()
	mux.Handle("POST /reports", a.Middleware(http.HandlerFunc(h.Send)))
	mux.Handle("GET /reports", a.RequireRole(http.HandlerFunc(h.List), "admin"))
	mux.Handle("GET /reports/summary", a.RequireRole(http.HandlerFunc(h.Summary), "admin"))
	mux.Handle("GET /reports/{id}", a.RequireRole(http.HandlerFunc(h.Get), "admin"))
	mux.Handle("PATCH /reports/{id}", a.RequireRole(http.HandlerFunc(h.Update), "admin"))
	mux.Handle("DELETE /reports/{id}", a.RequireRole(http.HandlerFunc(h.Delete), "admin"))

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if role != "" {
		tok, err := auth.AccessToken(31, []string{role}, secret, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAnyoneCanSendAReport(t *testing.T) {
	for _, role := range []string{"student", "mentor", "lecturer", "admin"} {
		svc := &rptFakeService{}
		rec := rptServe(t, svc, role, "POST", "/reports", `{"subject":"Student X","message":"Rude in class"}`)
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != 201 || svc.sentBy != 31 || body["id"] != float64(3) || body["subject"] != "Student X" || body["status"] != nil {
			t.Errorf("%s: %d %s", role, rec.Code, rec.Body)
		}
	}
	if rec := rptServe(t, &rptFakeService{}, "", "POST", "/reports", `{"subject":"a","message":"b"}`); rec.Code != 401 {
		t.Errorf("not signed in: %d", rec.Code)
	}
}

func TestOnlyAdminsReadReports(t *testing.T) {
	routes := [][2]string{{"GET", "/reports"}, {"GET", "/reports/summary"}, {"GET", "/reports/3"}, {"PATCH", "/reports/3"}, {"DELETE", "/reports/3"}}
	for _, role := range []string{"student", "mentor", "lecturer"} {
		for _, r := range routes {
			if rec := rptServe(t, &rptFakeService{}, role, r[0], r[1], `{"status":"resolved"}`); rec.Code != 403 {
				t.Errorf("%s %s %s: %d, want 403", role, r[0], r[1], rec.Code)
			}
		}
	}

	svc := &rptFakeService{}
	rec := rptServe(t, svc, "admin", "GET", "/reports?status=open&role=mentor&reporter_id=7&q=rude", "")
	var list []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	reporter, _ := list[0]["reporter"].(map[string]any)
	if rec.Code != 200 || svc.filter != (store.ReportFilter{Status: "open", Role: "mentor", ReporterID: 7, Query: "rude"}) ||
		reporter["name"] != "Murad" || list[0]["resolved_by"] != nil {
		t.Errorf("list: %d %s, filter %+v", rec.Code, rec.Body, svc.filter)
	}
	if rec := rptServe(t, svc, "admin", "GET", "/reports/summary", ""); rec.Code != 200 || rec.Body.String() != "{\"open\":2,\"resolved\":5,\"total\":7}\n" {
		t.Errorf("summary: %d %s", rec.Code, rec.Body)
	}
	if rec := rptServe(t, svc, "admin", "PATCH", "/reports/9", `{"status":"resolved","admin_note":"done"}`); rec.Code != 200 || svc.updated != 9 {
		t.Errorf("update: %d %s", rec.Code, rec.Body)
	}
	if rec := rptServe(t, svc, "admin", "DELETE", "/reports/9", ""); rec.Code != 204 {
		t.Errorf("delete: %d", rec.Code)
	}
}

func TestReportRouteErrors(t *testing.T) {
	invalid := &service.ReportInvalidError{Fields: []service.ReportFieldError{{Field: "subject", Message: "subject is required"}}}
	cases := []struct {
		name, role, method, path, body string
		err                            error
		want                           int
	}{
		{"not JSON", "student", "POST", "/reports", `subject=a`, nil, 400},
		{"two JSON objects", "student", "POST", "/reports", `{"subject":"a","message":"b"}{}`, nil, 400},
		{"message is a number", "student", "POST", "/reports", `{"subject":"a","message":5}`, nil, 422},
		{"too large", "student", "POST", "/reports", `{"subject":"a","message":"` + strings.Repeat("a", 70000) + `"}`, nil, 413},
		{"empty form", "student", "POST", "/reports", `{}`, invalid, 422},
		{"bad id", "admin", "GET", "/reports/abc", "", nil, 400},
		{"bad reporter_id", "admin", "GET", "/reports?reporter_id=x", "", nil, 400},
		{"unknown report", "admin", "GET", "/reports/99", "", service.ErrReportNotFound, 404},
		{"unknown report (delete)", "admin", "DELETE", "/reports/99", "", service.ErrReportNotFound, 404},
		{"database error", "admin", "GET", "/reports", "", context.DeadlineExceeded, 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := rptServe(t, &rptFakeService{err: c.err}, c.role, c.method, c.path, c.body)
			if rec.Code != c.want {
				t.Errorf("%d %s, want %d", rec.Code, rec.Body, c.want)
			}
			if c.err == invalid && !strings.Contains(rec.Body.String(), `"field":"subject"`) {
				t.Errorf("details missing: %s", rec.Body)
			}
		})
	}
}
