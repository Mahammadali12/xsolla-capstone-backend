package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/store"
)

// mockGoogleTokenService implements handlers.AuthService, recording only the
// DeleteGoogleRefreshToken call. All other methods return zero values.
type mockGoogleTokenService struct {
	deleteGoogleUserID int64
	deleteGoogleCalled bool
	deleteGoogleErr    error
	getGoogleToken     string
	getGoogleErr       error
}

func (m *mockGoogleTokenService) Signup(ctx context.Context, email, name, password string) (int64, []string, error) {
	return 0, nil, nil
}
func (m *mockGoogleTokenService) Login(ctx context.Context, email, password string) (*store.User, error) {
	return nil, nil
}
func (m *mockGoogleTokenService) SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	return nil
}
func (m *mockGoogleTokenService) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	return nil
}
func (m *mockGoogleTokenService) GetUserByID(ctx context.Context, id int64) (*store.User, error) {
	return nil, nil
}
func (m *mockGoogleTokenService) FindRefreshTokenByHash(ctx context.Context, tokenHash string) (int64, string, time.Time, error) {
	return 0, "", time.Time{}, nil
}
func (m *mockGoogleTokenService) SaveGoogleRefreshToken(ctx context.Context, userID int64, encryptedToken string, expiresAt time.Time) error {
	return nil
}
func (m *mockGoogleTokenService) GetGoogleRefreshToken(ctx context.Context, userID int64) (string, time.Time, error) {
	return m.getGoogleToken, time.Time{}, m.getGoogleErr
}
func (m *mockGoogleTokenService) DeleteGoogleRefreshToken(ctx context.Context, userID int64) error {
	m.deleteGoogleCalled = true
	m.deleteGoogleUserID = userID
	return m.deleteGoogleErr
}

func TestGoogleLogoutSuccess(t *testing.T) {
	secret := []byte("test-secret")
	svc := &mockGoogleTokenService{}
	authH := NewAuthHandlers(svc, secret, secret, 15*time.Minute, 7*24*time.Hour)

	token, err := auth.AccessToken(42, []string{"student"}, secret, 15*time.Minute)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /auth/google/logout", (&middleware.Authenticator{Secret: secret}).Middleware(http.HandlerFunc(authH.GoogleLogout)))

	req := httptest.NewRequest(http.MethodPost, "/auth/google/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if !svc.deleteGoogleCalled || svc.deleteGoogleUserID != 42 {
		t.Fatalf("expected DeleteGoogleRefreshToken called with 42, got called=%v userID=%d", svc.deleteGoogleCalled, svc.deleteGoogleUserID)
	}
}

func TestGoogleLogoutUnauthenticated(t *testing.T) {
	secret := []byte("test-secret")
	svc := &mockGoogleTokenService{}
	authH := NewAuthHandlers(svc, secret, secret, 15*time.Minute, 7*24*time.Hour)

	mux := http.NewServeMux()
	mux.Handle("POST /auth/google/logout", (&middleware.Authenticator{Secret: secret}).Middleware(http.HandlerFunc(authH.GoogleLogout)))

	req := httptest.NewRequest(http.MethodPost, "/auth/google/logout", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.deleteGoogleCalled {
		t.Fatal("expected DeleteGoogleRefreshToken not to be called for unauthenticated request")
	}
}

func TestGoogleLogoutStoreError(t *testing.T) {
	secret := []byte("test-secret")
	svc := &mockGoogleTokenService{deleteGoogleErr: errBoom}
	authH := NewAuthHandlers(svc, secret, secret, 15*time.Minute, 7*24*time.Hour)

	token, err := auth.AccessToken(7, []string{"student"}, secret, 15*time.Minute)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /auth/google/logout", (&middleware.Authenticator{Secret: secret}).Middleware(http.HandlerFunc(authH.GoogleLogout)))

	req := httptest.NewRequest(http.MethodPost, "/auth/google/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

var errBoom = &boomError{}

type boomError struct{}

func (e *boomError) Error() string { return "boom" }

func TestGoogleRevokeNotConnected(t *testing.T) {
	svc := &mockGoogleTokenService{getGoogleErr: store.ErrRefreshTokenNotFound}
	rec := callGoogleRevoke(t, svc)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if !svc.deleteGoogleCalled || svc.deleteGoogleUserID != 42 {
		t.Fatalf("expected DeleteGoogleRefreshToken called with 42, got called=%v userID=%d", svc.deleteGoogleCalled, svc.deleteGoogleUserID)
	}
}

func TestGoogleRevokeUndecryptableToken(t *testing.T) {
	svc := &mockGoogleTokenService{getGoogleToken: "not-base64"}
	rec := callGoogleRevoke(t, svc)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if !svc.deleteGoogleCalled {
		t.Fatal("expected DeleteGoogleRefreshToken to be called")
	}
}

func TestGoogleRevokeReadError(t *testing.T) {
	svc := &mockGoogleTokenService{getGoogleErr: errBoom}
	rec := callGoogleRevoke(t, svc)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.deleteGoogleCalled {
		t.Fatal("expected DeleteGoogleRefreshToken not to be called after a read error")
	}
}

func TestGoogleRevokeStoreError(t *testing.T) {
	svc := &mockGoogleTokenService{getGoogleErr: store.ErrRefreshTokenNotFound, deleteGoogleErr: errBoom}
	rec := callGoogleRevoke(t, svc)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGoogleRevokeUnauthenticated(t *testing.T) {
	secret := []byte("test-secret")
	svc := &mockGoogleTokenService{}
	authH := NewAuthHandlers(svc, secret, secret, 15*time.Minute, 7*24*time.Hour)

	mux := http.NewServeMux()
	mux.Handle("POST /auth/google/revoke", (&middleware.Authenticator{Secret: secret}).Middleware(http.HandlerFunc(authH.GoogleRevoke)))

	req := httptest.NewRequest(http.MethodPost, "/auth/google/revoke", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.deleteGoogleCalled {
		t.Fatal("expected DeleteGoogleRefreshToken not to be called for unauthenticated request")
	}
}

// callGoogleRevoke sends an authenticated request to GoogleRevoke.
func callGoogleRevoke(t *testing.T, svc *mockGoogleTokenService) *httptest.ResponseRecorder {
	t.Helper()
	secret := []byte("test-secret")
	authH := NewAuthHandlers(svc, secret, secret, 15*time.Minute, 7*24*time.Hour)

	token, err := auth.AccessToken(42, []string{"student"}, secret, 15*time.Minute)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /auth/google/revoke", (&middleware.Authenticator{Secret: secret}).Middleware(http.HandlerFunc(authH.GoogleRevoke)))

	req := httptest.NewRequest(http.MethodPost, "/auth/google/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// Ensure store import is referenced so it is not flagged as unused if the mock
// is ever trimmed.
var _ = context.Background
var _ = store.User{}
