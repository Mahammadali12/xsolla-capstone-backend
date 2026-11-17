package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
)

type bdUserService struct {
	UserService
	actor   int64
	role    string
	confirm bool
	err     error
}

func (f *bdUserService) DeleteAllUsers(_ context.Context, actorID int64, role string, confirm bool) (*service.DeletedUsers, error) {
	f.actor, f.role, f.confirm = actorID, role, confirm
	if f.err != nil {
		return nil, f.err
	}
	return &service.DeletedUsers{Deleted: 4, ByRole: map[string]int{"mentor": 1, "student": 3}}, nil
}

// bdServe registers the route exactly as cmd/server/main.go does.
func bdServe(t *testing.T, svc *bdUserService, role, path string) *httptest.ResponseRecorder {
	t.Helper()
	secret := []byte("bulk-delete-test-secret")
	h := NewUserHandlers(svc)
	a := &middleware.Authenticator{Secret: secret}
	mux := http.NewServeMux()
	mux.Handle("DELETE /users", a.RequireRole(http.HandlerFunc(h.DeleteAllUsers), "admin"))

	req := httptest.NewRequest("DELETE", path, nil)
	if role != "" {
		tok, err := auth.AccessToken(5, []string{role}, secret, time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestDeleteAllUsersRoute(t *testing.T) {
	svc := &bdUserService{}
	rec := bdServe(t, svc, "admin", "/users?role=student&confirm=true")
	if rec.Code != 200 || rec.Body.String() != "{\"deleted\":4,\"by_role\":{\"mentor\":1,\"student\":3}}\n" ||
		svc.actor != 5 || svc.role != "student" || !svc.confirm {
		t.Errorf("admin: %d %s (actor %d, role %q, confirm %v)", rec.Code, rec.Body, svc.actor, svc.role, svc.confirm)
	}

	for _, role := range []string{"student", "mentor", "lecturer"} {
		svc := &bdUserService{}
		if rec := bdServe(t, svc, role, "/users?confirm=true"); rec.Code != 403 || svc.actor != 0 {
			t.Errorf("%s: %d, service reached: %v", role, rec.Code, svc.actor != 0)
		}
	}
	if rec := bdServe(t, &bdUserService{}, "", "/users?confirm=true"); rec.Code != 401 {
		t.Errorf("not signed in: %d", rec.Code)
	}

	cases := []struct {
		name, path string
		err        error
		want       int
	}{
		{"no confirm", "/users", service.ErrDeleteUsersNotConfirmed, 422},
		{"bad role", "/users?role=admin&confirm=true", service.ErrDeleteUsersRole, 422},
		{"database error", "/users?confirm=true", context.DeadlineExceeded, 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := bdServe(t, &bdUserService{err: c.err}, "admin", c.path); rec.Code != c.want {
				t.Errorf("%d %s, want %d", rec.Code, rec.Body, c.want)
			}
		})
	}
	// confirm must be exactly true
	svc = &bdUserService{}
	bdServe(t, svc, "admin", "/users?confirm=yes")
	if svc.confirm {
		t.Error("confirm=yes was taken as confirmed")
	}
}
