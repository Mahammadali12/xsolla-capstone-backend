package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// UserService is the interface that user handlers depend on.
type UserService interface {
	ListUsers(ctx context.Context, search, role, status string) ([]store.User, error)
	CreateUser(ctx context.Context, email, name, password string, roles []string) (int64, []string, error)
	UpdateUser(ctx context.Context, id int64, name, email *string, active *bool) (*store.User, error)
	UpdatePassword(ctx context.Context, id int64, password string) error
	DeleteUser(ctx context.Context, id, actorID int64) error
	ReplaceUserRoles(ctx context.Context, userID int64, roles []string, expiries map[string]time.Time) error
}

type UserHandlers struct {
	svc UserService
}

// NewUserHandlers creates user handlers backed by the given service.
func NewUserHandlers(svc UserService) *UserHandlers {
	return &UserHandlers{svc: svc}
}

// GET /users?search=&role=&status=
func (h *UserHandlers) ListUsers(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	role := strings.TrimSpace(r.URL.Query().Get("role"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status == "" {
		status = "active"
	}

	users, err := h.svc.ListUsers(r.Context(), search, role, status)
	if err != nil {
		log.Printf("user handler: ListUsers error: %v", err)
		writeError(w, 500, "server_error", "could not list users", nil)
		return
	}

	resp := make([]dto.UserResponse, len(users))
	for i, u := range users {
		resp[i] = dto.UserResponse{
			ID:            u.ID,
			Email:         u.Email,
			Name:          u.Name,
			Roles:         u.Roles,
			RoleExpiresAt: u.RoleExpiresAt,
			LeftAt:        u.LeftAt,
			CreatedAt:     u.CreatedAt,
		}
	}
	writeJSON(w, 200, resp)
}

// POST /users
func (h *UserHandlers) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string   `json:"email"`
		Name     string   `json:"name"`
		Password string   `json:"password"`
		Roles    []string `json:"roles"`
	}
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

	id, roles, err := h.svc.CreateUser(r.Context(), req.Email, req.Name, req.Password, req.Roles)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidEmail):
			writeError(w, 400, "bad_request", "enter a valid email address", nil)
		case errors.Is(err, service.ErrInvalidName):
			writeError(w, 400, "bad_request", "name cannot be empty", nil)
		case errors.Is(err, service.ErrPasswordTooShort):
			writeError(w, 400, "bad_request", "password must be at least 8 characters", nil)
		case errors.Is(err, service.ErrInvalidRoleSet):
			writeError(w, 422, "validation_failed", "invalid role set", nil)
		case errors.Is(err, store.ErrEmailTaken):
			writeError(w, 409, "conflict", "There is already an account with this email.", nil)
		default:
			log.Printf("user handler: CreateUser error: %v", err)
			writeError(w, 500, "server_error", "could not create user", nil)
		}
		return
	}

	writeJSON(w, 201, dto.UserResponse{
		ID:            id,
		Email:         req.Email,
		Name:          req.Name,
		Roles:         roles,
		RoleExpiresAt: map[string]time.Time{},
		CreatedAt:     time.Now().UTC(),
	})
}

// PATCH /users/:id
func (h *UserHandlers) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid user ID", nil)
		return
	}

	var req struct {
		Name   *string `json:"name"`
		Email  *string `json:"email"`
		Active *bool   `json:"active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	var name, email *string
	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		name = &trimmed
	}
	if req.Email != nil {
		normalized := strings.ToLower(strings.TrimSpace(*req.Email))
		email = &normalized
	}

	u, err := h.svc.UpdateUser(r.Context(), id, name, email, req.Active)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrUserNotFound):
			writeError(w, 404, "not_found", "user not found", nil)
		case errors.Is(err, service.ErrInvalidEmail):
			writeError(w, 400, "bad_request", "enter a valid email address", nil)
		case errors.Is(err, service.ErrInvalidName):
			writeError(w, 400, "bad_request", "name cannot be empty", nil)
		case errors.Is(err, store.ErrEmailTaken):
			writeError(w, 409, "conflict", "There is already an account with this email.", nil)
		default:
			log.Printf("user handler: UpdateUser error: %v", err)
			writeError(w, 500, "server_error", "could not update user", nil)
		}
		return
	}

	writeJSON(w, 200, dto.UserResponse{
		ID:            u.ID,
		Email:         u.Email,
		Name:          u.Name,
		Roles:         u.Roles,
		RoleExpiresAt: u.RoleExpiresAt,
		LeftAt:        u.LeftAt,
		CreatedAt:     u.CreatedAt,
	})
}

// PATCH /users/:id/password
func (h *UserHandlers) UpdatePassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid user ID", nil)
		return
	}

	var req dto.UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	if req.Password == "" {
		writeError(w, 400, "bad_request", "password is required", nil)
		return
	}

	if err := h.svc.UpdatePassword(r.Context(), id, req.Password); err != nil {
		switch {
		case errors.Is(err, store.ErrUserNotFound):
			writeError(w, 404, "not_found", "user not found", nil)
		default:
			writeError(w, 400, "bad_request", err.Error(), nil)
		}
		return
	}

	w.WriteHeader(204)
}

// DELETE /users/:id
// Permanently deletes the account. Course content (modules, files) is moved to
// the acting admin; personal data is erased.
func (h *UserHandlers) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid user ID", nil)
		return
	}

	actorID := middleware.CtxUserID(r.Context())
	if err := h.svc.DeleteUser(r.Context(), id, actorID); err != nil {
		switch {
		case errors.Is(err, store.ErrUserNotFound):
			writeError(w, 404, "not_found", "user not found", nil)
		case errors.Is(err, service.ErrLastAdmin):
			writeError(w, 422, "validation_failed", "cannot remove the last admin", nil)
		case errors.Is(err, service.ErrCannotDeleteSelf):
			writeError(w, 422, "validation_failed", "cannot delete your own account", nil)
		default:
			log.Printf("user handler: DeleteUser error: %v", err)
			writeError(w, 500, "server_error", "could not delete user", nil)
		}
		return
	}
	w.WriteHeader(204)
}

// PUT /users/:id/roles
func (h *UserHandlers) ReplaceUserRoles(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid user ID", nil)
		return
	}

	var req struct {
		Roles     []string          `json:"roles"`
		ExpiresAt map[string]string `json:"expires_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	expiries := map[string]time.Time{}
	for role, dateStr := range req.ExpiresAt {
		if dateStr == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			writeError(w, 400, "bad_request", "invalid expiry date for role "+role, nil)
			return
		}
		expiries[role] = t
	}

	if err := h.svc.ReplaceUserRoles(r.Context(), id, req.Roles, expiries); err != nil {
		switch {
		case errors.Is(err, store.ErrUserNotFound):
			writeError(w, 404, "not_found", "user not found", nil)
		case errors.Is(err, service.ErrInvalidRoleSet):
			writeError(w, 422, "validation_failed", "invalid role set", nil)
		default:
			log.Printf("user handler: ReplaceUserRoles error: %v", err)
			writeError(w, 500, "server_error", "could not update roles", nil)
		}
		return
	}

	writeJSON(w, 200, dto.UserResponse{
		ID:            id,
		Roles:         req.Roles,
		RoleExpiresAt: expiries,
		CreatedAt:     time.Now().UTC(),
	})
}
