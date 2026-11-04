package handlers

import (
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

type AuthHandlers struct {
	svc           *service.AuthService
	accessSecret  []byte
	refreshSecret []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
}

// NewAuthHandlers creates auth handlers backed by the given service.
func NewAuthHandlers(svc *service.AuthService, accessSecret []byte, refreshSecret []byte, accessTTL time.Duration, refreshTTL time.Duration) *AuthHandlers {
	return &AuthHandlers{
		svc:           svc,
		accessSecret:  accessSecret,
		refreshSecret: refreshSecret,
		accessTTL:     accessTTL,
		refreshTTL:    refreshTTL,
	}
}

// POST /auth/signup
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

	id, role, err := h.svc.Signup(r.Context(), req.Email, req.Name, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrEmailTaken) {
			writeError(w, 409, "conflict", "email already exists", nil)
			return
		}
		writeError(w, 500, "server_error", "could not create user", nil)
		return
	}

	access, err := auth.AccessToken(id, role, h.accessSecret, h.accessTTL)
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
			Role:       role,
			CreatedAt:  time.Now().UTC(),
		},
		AccessToken:  access,
		RefreshToken: refresh,
	})
}

// POST /auth/login
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	req.Login = strings.ToLower(strings.TrimSpace(req.Login))

	id, role, err := h.svc.Login(r.Context(), req.Login, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) || errors.Is(err, service.ErrInvalidCredentials) {
			writeError(w, 401, "invalid_credentials", "invalid email or password", nil)
		} else {
			writeError(w, 500, "server_error", "database error", nil)
		}
		return
	}

	access, err := auth.AccessToken(id, role, h.accessSecret, h.accessTTL)
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
			Role:  role,
		},
		AccessToken:  access,
		RefreshToken: refresh,
	})
}

// POST /auth/refresh
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

	_, _, _, role, _, err := h.svc.GetUserByID(r.Context(), userID)
	if err != nil {
		writeError(w, 500, "server_error", "user lookup failed", nil)
		return
	}

	access, err := auth.AccessToken(userID, role, h.accessSecret, h.accessTTL)
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

// POST /auth/logout
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

// GET /users/me
func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	if userID == 0 {
		writeError(w, 401, "unauthorized", "missing user context", nil)
		return
	}
	_, email, name, role, _, err := h.svc.GetUserByID(r.Context(), userID)
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
		Role:       role,
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
