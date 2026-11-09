package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// AuthService is the interface that auth handlers depend on.
type AuthService interface {
	Signup(ctx context.Context, email string, name string, password string) (int64, []string, error)
	Login(ctx context.Context, email string, password string) (*store.User, error)
	SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
	GetUserByID(ctx context.Context, id int64) (*store.User, error)
	FindRefreshTokenByHash(ctx context.Context, tokenHash string) (int64, string, time.Time, error)
	SaveGoogleRefreshToken(ctx context.Context, userID int64, encryptedToken string, expiresAt time.Time) error
	GetGoogleRefreshToken(ctx context.Context, userID int64) (string, time.Time, error)
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
			ID:            id,
			Email:         req.Email,
			Name:          req.Name,
			Roles:         roles,
			RoleExpiresAt: map[string]time.Time{},
			CreatedAt:     time.Now().UTC(),
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

	user, err := h.svc.Login(r.Context(), req.Login, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInactive):
			writeError(w, 403, "account_inactive", "This account is no longer active.", nil)
			return
		case errors.Is(err, service.ErrInvalidCredentials) || errors.Is(err, service.ErrUserNotFound):
			writeError(w, 401, "invalid_credentials", "invalid email or password", nil)
			return
		default:
			writeError(w, 500, "server_error", "database error", nil)
			return
		}
	}

	access, err := auth.AccessToken(user.ID, user.Roles, h.accessSecret, h.accessTTL)
	if err != nil {
		writeError(w, 500, "server_error", "could not sign access token", nil)
		return
	}
	refresh, err := auth.RefreshToken(user.ID, h.refreshSecret, h.refreshTTL)
	if err != nil {
		writeError(w, 500, "server_error", "could not sign refresh token", nil)
		return
	}

	if err := h.svc.SaveRefreshToken(r.Context(), user.ID, hashToken(refresh), time.Now().Add(h.refreshTTL)); err != nil {
		writeError(w, 500, "server_error", "could not store refresh token", nil)
		return
	}

	writeJSON(w, 200, dto.AuthResponse{
		User: dto.UserResponse{
			ID:            user.ID,
			Email:         user.Email,
			Name:          user.Name,
			Roles:         user.Roles,
			RoleExpiresAt: user.RoleExpiresAt,
			CreatedAt:     user.CreatedAt,
		},
		AccessToken:  access,
		RefreshToken: refresh,
	})
}

// @Summary     Google OAuth login
// @Description Redirect user to Google consent screen to connect their Google account
// @Tags        auth
// @Produce     html
// @Success     302 {string} string "Redirect to Google"
// @Router      /auth/google/login [get]
func (h *AuthHandlers) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	if userID == 0 {
		// Fallback: allow access_token as a query parameter so a plain <a>
		// tag from the frontend can carry the JWT without setting headers.
		accessToken := r.URL.Query().Get("access_token")
		if accessToken == "" {
			writeError(w, 401, "unauthorized", "missing user context", nil)
			return
		}
		claims, err := auth.Verify(accessToken, h.accessSecret)
		if err != nil {
			writeError(w, 401, "unauthorized", "invalid access token", nil)
			return
		}
		userID = claims.UserID
	}

	state, err := generateRandomString(32)
	if err != nil {
		writeError(w, 500, "server_error", "could not generate state", nil)
		return
	}

	googleLoginStates.Store(state, googleOAuthState{
		userID:    userID,
		expiresAt: time.Now().Add(10 * time.Minute),
	})

	params := url.Values{
		"client_id":     {os.Getenv("GOOGLE_OAUTH_CLIENT_ID")},
		"redirect_uri":  {os.Getenv("GOOGLE_OAUTH_REDIRECT_URI")},
		"response_type": {"code"},
		"scope":         {
			"https://www.googleapis.com/auth/calendar.events " +
				"https://www.googleapis.com/auth/gmail.send " +
				"openid email",
		},
		"access_type": {"offline"},
		"prompt":      {"consent"},
		"state":       {state},
	}

	googleURL := "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()
	http.Redirect(w, r, googleURL, http.StatusFound)
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

	user, err := h.svc.GetUserByID(r.Context(), userID)
	if err != nil {
		writeError(w, 500, "server_error", "user lookup failed", nil)
		return
	}

	access, err := auth.AccessToken(userID, user.Roles, h.accessSecret, h.accessTTL)
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
	user, err := h.svc.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			writeError(w, 404, "not_found", "user not found", nil)
		} else {
			writeError(w, 500, "server_error", "database error", nil)
		}
		return
	}
	writeJSON(w, 200, dto.UserResponse{
		ID:            user.ID,
		Email:         user.Email,
		Name:          user.Name,
		Roles:         user.Roles,
		RoleExpiresAt: user.RoleExpiresAt,
		CreatedAt:     user.CreatedAt,
	})
}

// hashToken hashes a raw token string for storage.
// Refresh tokens are already high-entropy JWTs, so SHA-256 is sufficient.
// (bcrypt has a 72-byte input limit and JWTs often exceed it.)
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// googleOAuthState holds temporary state during the Google OAuth consent flow.
type googleOAuthState struct {
	userID    int64
	expiresAt time.Time
}

// googleLoginStates temporarily holds OAuth state values during the consent flow.
// shortcut: in-memory map, lost on restart. Not safe across multiple server instances.
// Upgrade path: replace with signed JWT cookie or Redis.
var googleLoginStates = sync.Map{}

// generateRandomString returns a random base64url string of n bytes.
func generateRandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// googleTokenEncryptionKey is the AES-256 key used to encrypt refresh tokens at rest.
// It must be 32 bytes, provided as base64 in the GOOGLE_TOKEN_ENCRYPTION_KEY env var.
// shortcut: key is read once from env on first use. No key rotation support yet.
// Upgrade path: integrate with a KMS or secrets manager.
var googleTokenEncryptionKey []byte
var googleTokenEncryptionKeyOnce sync.Once

func getGoogleTokenEncryptionKey() ([]byte, error) {
	var err error
	googleTokenEncryptionKeyOnce.Do(func() {
		encoded := os.Getenv("GOOGLE_TOKEN_ENCRYPTION_KEY")
		if encoded == "" {
			err = errors.New("GOOGLE_TOKEN_ENCRYPTION_KEY env var is required")
			return
		}
		googleTokenEncryptionKey, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			err = fmt.Errorf("invalid GOOGLE_TOKEN_ENCRYPTION_KEY: %w", err)
			return
		}
		if len(googleTokenEncryptionKey) != 32 {
			err = errors.New("GOOGLE_TOKEN_ENCRYPTION_KEY must decode to 32 bytes")
			return
		}
	})
	return googleTokenEncryptionKey, err
}

// encryptToken encrypts a plaintext token with AES-GCM and returns base64(nonce+ciphertext).
func encryptToken(plaintext string) (string, error) {
	key, err := getGoogleTokenEncryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// decryptToken decrypts a base64(nonce+ciphertext) token back to plaintext.
func decryptToken(ciphertext string) (string, error) {
	key, err := getGoogleTokenEncryptionKey()
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// googleTokenExchangeResponse is the response from Google's token endpoint.
type googleTokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

// @Summary     Google OAuth callback
// @Description Handle Google OAuth redirect, exchange code for tokens, store encrypted refresh token
// @Tags        auth
// @Produce     html
// @Success     302 {string} string "Redirect to frontend"
// @Router      /auth/google/callback [get]
func (h *AuthHandlers) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		log.Printf("google callback: missing code or state")
		http.Redirect(w, r, frontendRedirect("google=error"), http.StatusFound)
		return
	}

	val, ok := googleLoginStates.Load(state)
	if !ok {
		log.Printf("google callback: state not found")
		http.Redirect(w, r, frontendRedirect("google=error"), http.StatusFound)
		return
	}
	oauthState, ok := val.(googleOAuthState)
	if !ok {
		log.Printf("google callback: invalid state type")
		http.Redirect(w, r, frontendRedirect("google=error"), http.StatusFound)
		return
	}
	if time.Now().After(oauthState.expiresAt) {
		log.Printf("google callback: state expired for user %d", oauthState.userID)
		googleLoginStates.Delete(state)
		http.Redirect(w, r, frontendRedirect("google=error"), http.StatusFound)
		return
	}
	googleLoginStates.Delete(state)

	// Exchange authorization code for tokens.
	tokens, err := exchangeGoogleCode(code)
	if err != nil {
		log.Printf("google callback: token exchange failed: %v", err)
		http.Redirect(w, r, frontendRedirect("google=error"), http.StatusFound)
		return
	}
	if tokens.RefreshToken == "" {
		log.Printf("google callback: no refresh token in response")
		http.Redirect(w, r, frontendRedirect("google=error"), http.StatusFound)
		return
	}

	// Encrypt and store the refresh token.
	encrypted, err := encryptToken(tokens.RefreshToken)
	if err != nil {
		log.Printf("google callback: token encryption failed: %v", err)
		http.Redirect(w, r, frontendRedirect("google=error"), http.StatusFound)
		return
	}
	expiresAt := time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second)
	if err := h.svc.SaveGoogleRefreshToken(r.Context(), oauthState.userID, encrypted, expiresAt); err != nil {
		log.Printf("google callback: save refresh token failed for user %d: %v", oauthState.userID, err)
		http.Redirect(w, r, frontendRedirect("google=error"), http.StatusFound)
		return
	}

	log.Printf("google callback: refresh token saved for user %d", oauthState.userID)
	http.Redirect(w, r, frontendRedirect("google=connected"), http.StatusFound)
}

// frontendRedirect builds a redirect URL to the frontend settings page.
func frontendRedirect(query string) string {
	base := os.Getenv("GOOGLE_OAUTH_FRONTEND_REDIRECT_URI")
	if base == "" {
		base = "http://localhost:5173/settings"
	}
	if query != "" {
		base += "?" + query
	}
	return base
}

// exchangeGoogleCode exchanges an authorization code for Google OAuth tokens.
func exchangeGoogleCode(code string) (*googleTokenExchangeResponse, error) {
	data := url.Values{
		"code":          {code},
		"client_id":     {os.Getenv("GOOGLE_OAUTH_CLIENT_ID")},
		"client_secret": {os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")},
		"redirect_uri":  {os.Getenv("GOOGLE_OAUTH_REDIRECT_URI")},
		"grant_type":    {"authorization_code"},
	}

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google token exchange failed: %s", resp.Status)
	}

	var tokens googleTokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}

// googleUserInfo is the response from Google's userinfo endpoint.
type googleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
	Locale        string `json:"locale"`
	HD            string `json:"hd"`
	VerifiedEmail bool   `json:"verified_email"`
}

// exchangeGoogleRefreshToken exchanges a Google refresh token for a new access token.
func exchangeGoogleRefreshToken(refreshToken string) (*googleTokenExchangeResponse, error) {
	data := url.Values{
		"client_id":     {os.Getenv("GOOGLE_OAUTH_CLIENT_ID")},
		"client_secret": {os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET")},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google token refresh failed: %s", resp.Status)
	}

	var tokens googleTokenExchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}

// @Summary     Google user profile
// @Description Return the connected Google account profile for the signed-in user
// @Tags        auth
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} googleUserInfo
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     502 {object} handlers.ErrorResponse
// @Router      /auth/google/me [get]
func (h *AuthHandlers) GoogleMe(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	if userID == 0 {
		writeError(w, 401, "unauthorized", "missing user context", nil)
		return
	}

	encrypted, _, err := h.svc.GetGoogleRefreshToken(r.Context(), userID)
	if err != nil {
		if errors.Is(err, store.ErrRefreshTokenNotFound) {
			writeError(w, 404, "google_not_connected", "Google account not connected", nil)
			return
		}
		log.Printf("google me: get refresh token failed for user %d: %v", userID, err)
		writeError(w, 500, "server_error", "could not load Google token", nil)
		return
	}

	refreshToken, err := decryptToken(encrypted)
	if err != nil {
		log.Printf("google me: decrypt failed for user %d: %v", userID, err)
		writeError(w, 500, "server_error", "could not decrypt Google token", nil)
		return
	}

	tokens, err := exchangeGoogleRefreshToken(refreshToken)
	if err != nil {
		log.Printf("google me: refresh token exchange failed for user %d: %v", userID, err)
		writeError(w, 401, "google_token_expired", "Google refresh token expired or revoked", nil)
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		log.Printf("google me: create request failed: %v", err)
		writeError(w, 500, "server_error", "could not create request", nil)
		return
	}
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("google me: userinfo request failed: %v", err)
		writeError(w, 502, "upstream_error", "could not reach Google", nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("google me: userinfo returned %d", resp.StatusCode)
		writeError(w, 502, "upstream_error", "Google API error", nil)
		return
	}

	var info googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		log.Printf("google me: decode userinfo failed: %v", err)
		writeError(w, 502, "upstream_error", "could not parse Google response", nil)
		return
	}

	writeJSON(w, 200, info)
}
