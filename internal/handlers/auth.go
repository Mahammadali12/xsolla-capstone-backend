package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/middleware"
)

type AuthHandlers struct {
	DB             *sql.DB
	AccessSecret   []byte
	RefreshSecret  []byte
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
}

// respondJSON writes a JSON response with the given status code.
func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// respondError writes a standard error JSON response.
func respondError(w http.ResponseWriter, status int, code, message string) {
	respondJSON(w, status, map[string]string{"error": code, "message": message, "status": strconv.Itoa(status)})
}

// POST /auth/signup
func (h *AuthHandlers) Signup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	body.Name = strings.TrimSpace(body.Name)
	if body.Email == "" || body.Name == "" || body.Password == "" {
		respondError(w, 400, "bad_request", "email, name, and password are required")
		return
	}

	hash, err := auth.Hash(body.Password)
	if err != nil {
		respondError(w, 500, "server_error", "could not hash password")
		return
	}

	// first registered user becomes admin
	var count int
	h.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	role := "student"
	if count == 0 {
		role = "admin"
	}

	var id int64
	err = h.DB.QueryRow(
		`INSERT INTO users (email, name, role, password_hash) VALUES ($1, $2, $3, $4) RETURNING id`,
		body.Email, body.Name, role, hash,
	).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "Unique") {
			respondError(w, 409, "conflict", "email already exists")
			return
		}
		respondError(w, 500, "server_error", "could not create user")
		return
	}

	access, err := auth.AccessToken(id, role, h.AccessSecret, h.AccessTTL)
	if err != nil {
		respondError(w, 500, "server_error", "could not sign access token")
		return
	}
	refresh, err := auth.RefreshToken(id, h.RefreshSecret, h.RefreshTTL)
	if err != nil {
		respondError(w, 500, "server_error", "could not sign refresh token")
		return
	}

	// store refresh token in DB
	if _, err := h.DB.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		id, hashToken(refresh), time.Now().Add(h.RefreshTTL),
	); err != nil {
		respondError(w, 500, "server_error", "could not store refresh token")
		return
	}

	respondJSON(w, 201, map[string]any{
		"user": map[string]any{
			"id":         id,
			"email":      body.Email,
			"name":       body.Name,
			"role":       role,
			"created_at": time.Now().UTC(),
		},
		"access_token":  access,
		"refresh_token": refresh,
	})
}

// POST /auth/login
func (h *AuthHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}
	body.Login = strings.ToLower(strings.TrimSpace(body.Login))

	var id int64
	var hash, role string
	err := h.DB.QueryRow(
		`SELECT id, password_hash, role FROM users WHERE lower(email) = $1`,
		body.Login,
	).Scan(&id, &hash, &role)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, 401, "invalid_credentials", "invalid email or password")
		} else {
			respondError(w, 500, "server_error", "database error")
		}
		return
	}

	if err := auth.Compare(hash, body.Password); err != nil {
		respondError(w, 401, "invalid_credentials", "invalid email or password")
		return
	}

	access, err := auth.AccessToken(id, role, h.AccessSecret, h.AccessTTL)
	if err != nil {
		respondError(w, 500, "server_error", "could not sign access token")
		return
	}
	refresh, err := auth.RefreshToken(id, h.RefreshSecret, h.RefreshTTL)
	if err != nil {
		respondError(w, 500, "server_error", "could not sign refresh token")
		return
	}

	// replace any existing refresh token for this user (single session)
	h.DB.Exec(`DELETE FROM refresh_tokens WHERE user_id = $1`, id)
	if _, err := h.DB.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		id, hashToken(refresh), time.Now().Add(h.RefreshTTL),
	); err != nil {
		respondError(w, 500, "server_error", "could not store refresh token")
		return
	}

	respondJSON(w, 200, map[string]any{
		"user": map[string]any{
			"id":    id,
			"email": body.Login,
			"role":  role,
		},
		"access_token":  access,
		"refresh_token": refresh,
	})
}

// POST /auth/refresh
func (h *AuthHandlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, 400, "bad_request", "invalid JSON body")
		return
	}

	type rt struct {
		userID   int64
		tokenHash string
		expires  time.Time
	}
	rows, err := h.DB.Query(`SELECT user_id, token_hash, expires_at FROM refresh_tokens WHERE expires_at > NOW()`)
	if err != nil {
		respondError(w, 500, "server_error", "database error")
		return
	}
	defer rows.Close()

	var match *rt
	for rows.Next() {
		var r rt
		if err := rows.Scan(&r.userID, &r.tokenHash, &r.expires); err != nil {
			continue
		}
		if compareToken(r.tokenHash, body.RefreshToken) {
			match = &r
			break
		}
	}
	if match == nil {
		respondError(w, 401, "invalid_credentials", "invalid or expired refresh token")
		return
	}

	// look up user role
	var role string
	if err := h.DB.QueryRow(`SELECT role FROM users WHERE id = $1`, match.userID).Scan(&role); err != nil {
		respondError(w, 500, "server_error", "user not found")
		return
	}

	access, err := auth.AccessToken(match.userID, role, h.AccessSecret, h.AccessTTL)
	if err != nil {
		respondError(w, 500, "server_error", "could not sign access token")
		return
	}
	newRefresh, err := auth.RefreshToken(match.userID, h.RefreshSecret, h.RefreshTTL)
	if err != nil {
		respondError(w, 500, "server_error", "could not sign refresh token")
		return
	}

	// rotate: delete old, insert new
	h.DB.Exec(`DELETE FROM refresh_tokens WHERE user_id = $1 AND token_hash = $2`, match.userID, match.tokenHash)
	if _, err := h.DB.Exec(
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		match.userID, hashToken(newRefresh), time.Now().Add(h.RefreshTTL),
	); err != nil {
		respondError(w, 500, "server_error", "could not store refresh token")
		return
	}

	respondJSON(w, 200, map[string]any{
		"access_token":  access,
		"refresh_token": newRefresh,
	})
}

// POST /auth/logout
func (h *AuthHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.RefreshToken == "" {
		// also accept from Authorization header
		body.RefreshToken = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}

	rows, _ := h.DB.Query(`SELECT user_id, token_hash FROM refresh_tokens`)
	defer rows.Close()
	for rows.Next() {
		var uid int64
		var hash string
		if err := rows.Scan(&uid, &hash); err != nil {
			continue
		}
		if compareToken(hash, body.RefreshToken) {
			h.DB.Exec(`DELETE FROM refresh_tokens WHERE user_id = $1 AND token_hash = $2`, uid, hash)
			break
		}
	}
	w.WriteHeader(204)
}

// GET /users/me
func (h *AuthHandlers) Me(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	if userID == 0 {
		respondError(w, 401, "unauthorized", "missing user context")
		return
	}
	var email, name, role string
	var createdAt time.Time
	if err := h.DB.QueryRow(
		`SELECT email, name, role, created_at FROM users WHERE id = $1`,
		userID,
	).Scan(&email, &name, &role, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			respondError(w, 404, "not_found", "user not found")
		} else {
			respondError(w, 500, "server_error", "database error")
		}
		return
	}
	respondJSON(w, 200, map[string]any{
		"id":         userID,
		"email":      email,
		"name":       name,
		"role":       role,
		"created_at": createdAt.UTC(),
	})
}

// hashToken hashes a raw token string for storage.
// Refresh tokens are already high-entropy JWTs, so SHA-256 is sufficient.
// (bcrypt has a 72-byte input limit and JWTs often exceed it.)
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// compareToken checks a raw token against a stored SHA-256 hash.
func compareToken(storedHash, token string) bool {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:]) == storedHash
}
