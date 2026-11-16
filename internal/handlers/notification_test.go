package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

type ntfFakeService struct {
	err    error
	userID int64
	limit  int
	readID int64
}

func (f *ntfFakeService) Unread(_ context.Context, userID int64, limit int) ([]store.Notification, error) {
	f.userID, f.limit = userID, limit
	if f.err != nil {
		return nil, f.err
	}
	return []store.Notification{{ID: 5, EventType: "enrollment.graded", Title: "Grade posted for Week 3", Body: "Hi Anna",
		CreatedAt: time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC)}}, nil
}

func (f *ntfFakeService) MarkRead(_ context.Context, userID, id int64) error {
	f.userID, f.readID = userID, id
	return f.err
}

func (f *ntfFakeService) MarkAllRead(_ context.Context, userID int64) (int64, error) {
	f.userID = userID
	return 2, f.err
}

func ntfServe(t *testing.T, svc *ntfFakeService, method, path string, signedIn bool) *httptest.ResponseRecorder {
	t.Helper()
	secret := []byte("notification-test-secret")
	h := NewNotificationHandlers(svc)
	a := &middleware.Authenticator{Secret: secret}
	mux := http.NewServeMux()
	mux.Handle("GET /notifications", a.Middleware(http.HandlerFunc(h.Unread)))
	mux.Handle("POST /notifications/read-all", a.Middleware(http.HandlerFunc(h.MarkAllRead)))
	mux.Handle("POST /notifications/{id}/read", a.Middleware(http.HandlerFunc(h.MarkRead)))

	req := httptest.NewRequest(method, path, nil)
	if signedIn {
		tok, err := auth.AccessToken(31, []string{"student"}, secret, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestNotificationRoutes(t *testing.T) {
	svc := &ntfFakeService{}
	rec := ntfServe(t, svc, "GET", "/notifications?limit=20", true)
	var list []map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || svc.userID != 31 || svc.limit != 20 || len(list) != 1 ||
		list[0]["type"] != "enrollment.graded" || list[0]["title"] != "Grade posted for Week 3" ||
		list[0]["message"] != "Hi Anna" || list[0]["read_at"] != nil || list[0]["created_at"] != "2026-11-16T08:00:00Z" {
		t.Errorf("GET /notifications: %d %s (user %d, limit %d)", rec.Code, rec.Body, svc.userID, svc.limit)
	}

	if rec := ntfServe(t, svc, "POST", "/notifications/41/read", true); rec.Code != 204 || svc.readID != 41 || svc.userID != 31 {
		t.Errorf("POST /notifications/41/read: %d (id %d, user %d)", rec.Code, svc.readID, svc.userID)
	}
	rec = ntfServe(t, svc, "POST", "/notifications/read-all", true)
	if rec.Code != 200 || rec.Body.String() != "{\"marked\":2}\n" {
		t.Errorf("POST /notifications/read-all: %d %s", rec.Code, rec.Body)
	}
}

func TestNotificationRouteErrors(t *testing.T) {
	cases := []struct {
		name, method, path string
		signedIn           bool
		err                error
		want               int
	}{
		{"not signed in", "GET", "/notifications", false, nil, 401},
		{"limit not a number", "GET", "/notifications?limit=ten", true, nil, 400},
		{"limit too big", "GET", "/notifications?limit=101", true, nil, 400},
		{"bad id", "POST", "/notifications/abc/read", true, nil, 400},
		{"someone else's", "POST", "/notifications/9/read", true, service.ErrNotificationNotFound, 404},
		{"no notification database", "GET", "/notifications", true, service.ErrNotificationsUnavailable, 503},
		{"no notification database (read-all)", "POST", "/notifications/read-all", true, service.ErrNotificationsUnavailable, 503},
		{"database error", "POST", "/notifications/9/read", true, context.DeadlineExceeded, 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := ntfServe(t, &ntfFakeService{err: c.err}, c.method, c.path, c.signedIn); rec.Code != c.want {
				t.Errorf("%s %s: %d %s, want %d", c.method, c.path, rec.Code, rec.Body, c.want)
			}
		})
	}
}
