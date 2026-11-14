package handlers

import (
	"context"
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

// mockGoogleTokenService implements handlers.AuthService, recording only the
// DeleteGoogleRefreshToken call. All other methods return zero values.
type mockGoogleTokenService struct {
	deleteGoogleUserID int64
	deleteGoogleCalled bool
	deleteGoogleErr    error
	getGoogleToken     string
	getGoogleErr       error

	changePasswordCalled bool
	changePasswordUserID int64
	changePasswordValue  string
	changePasswordErr    error

	resetPasswordCalled bool
	resetPasswordEmail  string
	resetPasswordOld    string
	resetPasswordNew    string
	resetPasswordErr    error
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
func (m *mockGoogleTokenService) ChangePassword(ctx context.Context, userID int64, newPassword string) error {
	m.changePasswordCalled = true
	m.changePasswordUserID = userID
	m.changePasswordValue = newPassword
	return m.changePasswordErr
}
func (m *mockGoogleTokenService) ResetPassword(ctx context.Context, email string, oldPassword string, newPassword string) error {
	m.resetPasswordCalled = true
	m.resetPasswordEmail = email
	m.resetPasswordOld = oldPassword
	m.resetPasswordNew = newPassword
	return m.resetPasswordErr
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

func TestPasswordResetSuccess(t *testing.T) {
	svc := &mockGoogleTokenService{}
	rec := callAuth(t, svc, "/auth/password-reset", `{"password":"Str0ng-Passphrase!"}`, true)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if !svc.changePasswordCalled || svc.changePasswordUserID != 42 || svc.changePasswordValue != "Str0ng-Passphrase!" {
		t.Fatalf("unexpected ChangePassword call: called=%v userID=%d password=%q",
			svc.changePasswordCalled, svc.changePasswordUserID, svc.changePasswordValue)
	}
}

func TestPasswordResetUnauthenticated(t *testing.T) {
	svc := &mockGoogleTokenService{}
	rec := callAuth(t, svc, "/auth/password-reset", `{"password":"Str0ng-Passphrase!"}`, false)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.changePasswordCalled {
		t.Fatal("expected ChangePassword not to be called")
	}
}

func TestPasswordResetMissingPassword(t *testing.T) {
	svc := &mockGoogleTokenService{}
	rec := callAuth(t, svc, "/auth/password-reset", `{}`, true)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.changePasswordCalled {
		t.Fatal("expected ChangePassword not to be called")
	}
}

func TestPasswordResetPolicyError(t *testing.T) {
	svc := &mockGoogleTokenService{changePasswordErr: errors.New("add a number")}
	rec := callAuth(t, svc, "/auth/password-reset", `{"password":"short"}`, true)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "add a number") {
		t.Fatalf("expected policy message, got %s", rec.Body.String())
	}
}

func TestResetSuccess(t *testing.T) {
	svc := &mockGoogleTokenService{}
	rec := callAuth(t, svc, "/auth/reset",
		`{"email":" ALICE@Example.com ","old_password":"old","new_password":"Str0ng-Passphrase!"}`, false)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if !svc.resetPasswordCalled || svc.resetPasswordEmail != "alice@example.com" {
		t.Fatalf("unexpected ResetPassword call: called=%v email=%q", svc.resetPasswordCalled, svc.resetPasswordEmail)
	}
	if svc.resetPasswordOld != "old" || svc.resetPasswordNew != "Str0ng-Passphrase!" {
		t.Fatalf("unexpected passwords: old=%q new=%q", svc.resetPasswordOld, svc.resetPasswordNew)
	}
}

func TestResetInvalidCredentials(t *testing.T) {
	svc := &mockGoogleTokenService{resetPasswordErr: service.ErrInvalidCredentials}
	rec := callAuth(t, svc, "/auth/reset",
		`{"email":"alice@example.com","old_password":"wrong","new_password":"Str0ng-Passphrase!"}`, false)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResetInactive(t *testing.T) {
	svc := &mockGoogleTokenService{resetPasswordErr: service.ErrInactive}
	rec := callAuth(t, svc, "/auth/reset",
		`{"email":"alice@example.com","old_password":"old","new_password":"Str0ng-Passphrase!"}`, false)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResetMissingFields(t *testing.T) {
	svc := &mockGoogleTokenService{}
	rec := callAuth(t, svc, "/auth/reset", `{"email":"alice@example.com"}`, false)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.resetPasswordCalled {
		t.Fatal("expected ResetPassword not to be called")
	}
}

// callAuth posts a JSON body to /auth/password-reset or /auth/reset.
func callAuth(t *testing.T, svc *mockGoogleTokenService, route, body string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	secret := []byte("test-secret")
	authH := NewAuthHandlers(svc, secret, secret, 15*time.Minute, 7*24*time.Hour)

	var handler http.HandlerFunc
	switch route {
	case "/auth/password-reset":
		handler = authH.PasswordReset
	case "/auth/reset":
		handler = authH.Reset
	default:
		t.Fatalf("unknown route %q", route)
	}

	mux := http.NewServeMux()
	if authenticated {
		mux.Handle("POST "+route, (&middleware.Authenticator{Secret: secret}).Middleware(handler))
	} else {
		mux.HandleFunc("POST "+route, handler)
	}

	req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
	if authenticated {
		token, err := auth.AccessToken(42, []string{"student"}, secret, 15*time.Minute)
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// Ensure store import is referenced so it is not flagged as unused if the mock
// is ever trimmed.
var _ = context.Background
var _ = store.User{}
