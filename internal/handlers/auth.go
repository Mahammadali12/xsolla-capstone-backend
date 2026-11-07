package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
)

// AuthService is the interface that auth handlers depend on.
type AuthService interface {
	Signup(ctx context.Context, email string, name string, password string) (int64, []string, error)
	Login(ctx context.Context, email string, password string) (int64, []string, error)
	SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
	GetUserByID(ctx context.Context, id int64) (int64, string, string, []string, time.Time, error)
	FindRefreshTokenByHash(ctx context.Context, tokenHash string) (int64, string, time.Time, error)
}

type AuthHandlers struct {
	svc           AuthService
	accessSecret  []byte
	refreshSecret []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
}

// NewAuthHandlers creates auth handlers backed by the given service.
func NewAuthHandlers(svc AuthService, accessSecret []byte, refreshSecret []byte, accessTTL time.Duration, refreshTTL time.Duration) *AuthHandlers {
	return &AuthHandlers{
		svc:           svc,
		accessSecret:  accessSecret,
		refreshSecret: refreshSecret,
		accessTTL:     accessTTL,
		refreshTTL:    refreshTTL,
	}
}

// @Summary     Sign up
// @Description Create a new user account. First user becomes admin, @xsolla.com emails become teacher, rest are student.
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       request body dto.SignupRequest true "Registration credentials"
// @Success     201 {object} dto.AuthResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     409 {object} handlers.ErrorResponse
// @Router      /auth/signup [post]
func (h *AuthHandlers) Signup(w http.ResponseWriter, r *http.Request) {
	var req dto.SignupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Name = strings.TrimSpace(req.Name)
	if req.Email == "" || req.Name == "" || req.Password == "" {
		writeError(w, 400, "bad_request", "email, name, and password are required", nil)
		return
	}

	id, roles, err := h.svc.Signup(r.Context(), req.Email, req.Name, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrEmailTaken) {
			writeError(w, 409, "conflict", "email already exists", nil)
			return
		}
		writeError(w, 500, "server_error", "could not create user", nil)
		return
	}

	access, err := auth.AccessToken(id, roles, h.accessSecret, h.accessTTL)
	if err != nil {
		writeError(w, 500, "server_error", "could not sign access token", nil)
		return
	}
	refresh, err := auth.RefreshToken(id, h.refreshSecret, h.refreshTTL)
	if err != nil {
		writeError(w, 500, "server_error", "could not sign refresh token", nil)
		return
	}

	if err := h.svc.SaveRefreshToken(r.Context(), id, hashToken(refresh), time.Now().Add(h.refreshTTL)); err != nil {
		writeError(w, 500, "server_error", "could not store refresh token", nil)
		return
	}

	writeJSON(w, 201, dto.AuthResponse{
		User: dto.UserResponse{
			ID:         id,
			Email:      req.Email,
			Name:       req.Name,
			Roles:      roles,
			CreatedAt:  time.Now().UTC(),
		},
		AccessToken:  access,
		RefreshToken: refresh,
	})
}

// @Summary     Log in
// @Description Authenticate with email and password to receive JWT and refresh tokens
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       request body dto.LoginRequest true "Login credentials"
// @Success     200 {object} dto.AuthResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Router      /auth/login [post]
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	req.Login = strings.ToLower(strings.TrimSpace(req.Login))

	id, roles, err := h.svc.Login(r.Context(), req.Login, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) || errors.Is(err, service.ErrInvalidCredentials) {
			writeError(w, 401, "invalid_credentials", "invalid email or password", nil)
		} else {
			writeError(w, 500, "server_error", "database error", nil)
		}
		return
	}

	access, err := auth.AccessToken(id, roles, h.accessSecret, h.accessTTL)
	if err != nil {
		writeError(w, 500, "server_error", "could not sign access token", nil)
		return
	}
	refresh, err := auth.RefreshToken(id, h.refreshSecret, h.refreshTTL)
	if err != nil {
		writeError(w, 500, "server_error", "could not sign refresh token", nil)
		return
	}

	if err := h.svc.SaveRefreshToken(r.Context(), id, hashToken(refresh), time.Now().Add(h.refreshTTL)); err != nil {
		writeError(w, 500, "server_error", "could not store refresh token", nil)
		return
	}

	writeJSON(w, 200, dto.AuthResponse{
		User: dto.UserResponse{
			ID:    id,
			Email: req.Login,
			Roles: roles,
		},
		AccessToken:  access,
		RefreshToken: refresh,
	})
}

// @Summary     Refresh access token
// @Description Rotate the refresh token and receive a new JWT
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       request body dto.RefreshRequest true "Refresh token"
// @Success     200 {object} dto.TokenResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Router      /auth/refresh [post]
func (h *AuthHandlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var req dto.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	userID, _, _, err := h.svc.FindRefreshTokenByHash(r.Context(), req.RefreshToken)
	if err != nil {
		writeError(w, 401, "invalid_credentials", "invalid or expired refresh token", nil)
		return
	}

	_, _, _, roles, _, err := h.svc.GetUserByID(r.Context(), userID)
	if err != nil {
		writeError(w, 500, "server_error", "user lookup failed", nil)
		return
	}

	access, err := auth.AccessToken(userID, roles, h.accessSecret, h.accessTTL)
	if err != nil {
		writeError(w, 500, "server_error", "could not sign access token", nil)
		return
	}
	newRefresh, err := auth.RefreshToken(userID, h.refreshSecret, h.refreshTTL)
	if err != nil {
		writeError(w, 500, "server_error", "could not sign refresh token", nil)
		return
	}

	if err := h.svc.DeleteRefreshToken(r.Context(), req.RefreshToken); err != nil {
		writeError(w, 500, "server_error", "could not rotate refresh token", nil)
		return
	}
	if err := h.svc.SaveRefreshToken(r.Context(), userID, hashToken(newRefresh), time.Now().Add(h.refreshTTL)); err != nil {
		writeError(w, 500, "server_error", "could not store refresh token", nil)
		return
	}

	writeJSON(w, 200, dto.TokenResponse{
		AccessToken:  access,
		RefreshToken: newRefresh,
	})
}

// @Summary     Log out
// @Description Revoke the refresh token
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       request body dto.LogoutRequest true "Refresh token"
// @Success     204 {string} string "No content"
// @Failure     400 {object} handlers.ErrorResponse
// @Router      /auth/logout [post]
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	var req dto.LogoutRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.RefreshToken == "" {
		req.RefreshToken = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}

	// Best effort: token may already be deleted
	_ = h.svc.DeleteRefreshToken(r.Context(), req.RefreshToken)

	w.WriteHeader(204)
}

// @Summary     Get current user
// @Description Get the currently authenticated user profile
// @Tags        users
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} dto.UserResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /users/me [get]
func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	if userID == 0 {
		writeError(w, 401, "unauthorized", "missing user context", nil)
		return
	}
	_, email, name, roles, _, err := h.svc.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			writeError(w, 404, "not_found", "user not found", nil)
		} else {
			writeError(w, 500, "server_error", "database error", nil)
		}
		return
	}
	writeJSON(w, 200, dto.UserResponse{
		ID:         userID,
		Email:      email,
		Name:       name,
		Roles:      roles,
		CreatedAt:  time.Now().UTC(),
	})
}

// hashToken hashes a raw token string for storage.
// Refresh tokens are already high-entropy JWTs, so SHA-256 is sufficient.
// (bcrypt has a 72-byte input limit and JWTs often exceed it.)
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
