package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRevokeTokenAt(t *testing.T) {
	var gotMethod, gotContentType, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotToken = r.PostFormValue("token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := revokeTokenAt(context.Background(), srv.URL, "refresh-token"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("content type = %q", gotContentType)
	}
	if gotToken != "refresh-token" {
		t.Fatalf("token = %q, want refresh-token", gotToken)
	}
}

func TestRevokeTokenAtStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		wantErr bool
	}{
		{name: "ok", status: http.StatusOK},
		{name: "already invalid", status: http.StatusBadRequest},
		{name: "server error", status: http.StatusServiceUnavailable, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			err := revokeTokenAt(context.Background(), srv.URL, "refresh-token")
			if tc.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestRevokeTokenAtUnreachable(t *testing.T) {
	// Port 1 is closed, so the request fails before reaching any server.
	if err := revokeTokenAt(context.Background(), "http://127.0.0.1:1", "refresh-token"); err == nil {
		t.Fatal("expected a network error")
	}
}
