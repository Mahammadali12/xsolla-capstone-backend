package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/service/google"
	"xsolla-capstone-api/internal/store"
)

// UserStore abstracts user persistence for the service layer.
type UserStore interface {
	GetUserCount(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, email string, name string, roles []string, passwordHash string) (int64, error)
	GetUserByEmail(ctx context.Context, email string) (*store.User, error)
	GetUserRoles(ctx context.Context, userID int64) ([]string, map[string]time.Time, error)
	GetUserByID(ctx context.Context, id int64) (*store.User, error)
	GetUserPasswordHash(ctx context.Context, id int64) (string, error)
	UpdateUserPassword(ctx context.Context, id int64, passwordHash string) (sql.Result, error)
	// Google OAuth token storage
	SaveGoogleRefreshToken(ctx context.Context, userID int64, encryptedToken string, expiresAt time.Time) error
	GetGoogleRefreshToken(ctx context.Context, userID int64) (string, time.Time, error)
	DeleteGoogleRefreshToken(ctx context.Context, userID int64) error
}

// ErrEmailTaken is re-exported from the store package for handler use.
var ErrEmailTaken = store.ErrEmailTaken

// ErrUserNotFound is re-exported from the store package for handler use.
var ErrUserNotFound = store.ErrUserNotFound

// ErrInvalidCredentials is returned when email or password is incorrect.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrInactive is returned when a user account is inactive (left the school).
var ErrInactive = errors.New("account is inactive")

// RefreshTokenStore abstracts refresh token persistence for the service layer.
type RefreshTokenStore interface {
	SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	DeleteRefreshTokensForUser(ctx context.Context, userID int64) error
	FindRefreshTokenByHash(ctx context.Context, tokenHash string) (int64, string, time.Time, error)
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
}

// AuthService coordinates auth operations between handlers and the store.
type AuthService struct {
	userStore         UserStore
	refreshTokenStore RefreshTokenStore
}

// NewAuthService creates an AuthService backed by the given stores.
func NewAuthService(userStore UserStore, refreshTokenStore RefreshTokenStore) *AuthService {
	return &AuthService{
		userStore:         userStore,
		refreshTokenStore: refreshTokenStore,
	}
}

// Signup creates a new user.
// First user becomes admin, @xsolla.com emails become lecturer, rest are student.
func (s *AuthService) Signup(ctx context.Context, email string, name string, password string) (int64, []string, error) {
	email = normalizeEmail(email)
	if err := ValidateEmail(email); err != nil {
		return 0, nil, err
	}

	count, err := s.userStore.GetUserCount(ctx)
	if err != nil {
		return 0, nil, err
	}

	roles := []string{"student"}
	if count == 0 {
		roles = []string{"admin"}
	} else if strings.HasSuffix(email, "@xsolla.com") {
		roles = []string{"lecturer"}
	}

	hash, err := auth.Hash(password)
	if err != nil {
		return 0, nil, err
	}

	id, err := s.userStore.CreateUser(ctx, email, name, roles, hash)
	if err != nil {
		if errors.Is(err, store.ErrEmailTaken) {
			return 0, nil, store.ErrEmailTaken
		}
		return 0, nil, err
	}
	return id, roles, nil
}

// Login validates credentials and returns the user with effective roles.
// A user whose account is inactive (left_at set) gets ErrInactive.
func (s *AuthService) Login(ctx context.Context, email string, password string) (*store.User, error) {
	user, err := s.userStore.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	// Inactive accounts cannot sign in.
	if user.LeftAt != nil || user.DeletedAt != nil {
		return nil, ErrInactive
	}

	if err := auth.Compare(user.PasswordHash, password); err != nil {
		return nil, ErrInvalidCredentials
	}

	applyEffectiveRoles(user)
	return user, nil
}

// ChangePassword replaces the signed-in user's password after the caller
// proves the current one. Existing sessions are left alone.
func (s *AuthService) ChangePassword(ctx context.Context, userID int64, oldPassword string, newPassword string) error {
	user, err := s.userStore.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	hash, err := s.userStore.GetUserPasswordHash(ctx, userID)
	if err != nil {
		return err
	}
	if err := auth.Compare(hash, oldPassword); err != nil {
		return ErrInvalidCredentials
	}
	if err := validateNewPassword(newPassword, user.Name, user.Email); err != nil {
		return err
	}
	newHash, err := auth.Hash(newPassword)
	if err != nil {
		return err
	}
	_, err = s.userStore.UpdateUserPassword(ctx, userID, newHash)
	return err
}

// GetUserByID looks up a user by ID with effective roles.
func (s *AuthService) GetUserByID(ctx context.Context, id int64) (*store.User, error) {
	user, err := s.userStore.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	applyEffectiveRoles(user)
	return user, nil
}

// applyEffectiveRoles fills in Roles (raw roles minus expired) and
// RoleExpiresAt (non-expired expiries only) on the user.
// Roles and their expiries are already loaded by the store lookup.
func applyEffectiveRoles(user *store.User) {
	now := time.Now()
	effective := []string{}
	kept := map[string]time.Time{}
	for _, role := range user.Roles {
		if exp, ok := user.RoleExpiresAt[role]; ok && !exp.After(now) {
			continue // role expired
		}
		effective = append(effective, role)
		if exp, ok := user.RoleExpiresAt[role]; ok {
			kept[role] = exp
		}
	}
	user.Roles = effective
	user.RoleExpiresAt = kept
}

// SaveRefreshToken persists a refresh token for a user.
func (s *AuthService) SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	return s.refreshTokenStore.SaveRefreshToken(ctx, userID, tokenHash, expiresAt)
}

// DeleteRefreshTokensForUser removes all refresh tokens for a user.
func (s *AuthService) DeleteRefreshTokensForUser(ctx context.Context, userID int64) error {
	return s.refreshTokenStore.DeleteRefreshTokensForUser(ctx, userID)
}

// hashRefreshToken hashes a raw refresh token for store lookup.
func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// FindRefreshTokenByHash looks up an active refresh token by hash.
func (s *AuthService) FindRefreshTokenByHash(ctx context.Context, tokenHash string) (int64, string, time.Time, error) {
	hashed := hashRefreshToken(tokenHash)
	return s.refreshTokenStore.FindRefreshTokenByHash(ctx, hashed)
}

// DeleteRefreshToken removes a refresh token by hash.
func (s *AuthService) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	hashed := hashRefreshToken(tokenHash)
	return s.refreshTokenStore.DeleteRefreshToken(ctx, hashed)
}

// SaveGoogleRefreshToken persists an encrypted Google refresh token for a user.
func (s *AuthService) SaveGoogleRefreshToken(ctx context.Context, userID int64, encryptedToken string, expiresAt time.Time) error {
	return s.userStore.SaveGoogleRefreshToken(ctx, userID, encryptedToken, expiresAt)
}

// GetGoogleRefreshToken retrieves the encrypted Google refresh token for a user.
func (s *AuthService) GetGoogleRefreshToken(ctx context.Context, userID int64) (string, time.Time, error) {
	return s.userStore.GetGoogleRefreshToken(ctx, userID)
}

// DeleteGoogleRefreshToken removes the stored Google OAuth refresh token for a user.
func (s *AuthService) DeleteGoogleRefreshToken(ctx context.Context, userID int64) error {
	return s.userStore.DeleteGoogleRefreshToken(ctx, userID)
}

// GetValidAccessToken returns a usable Google access token for the user,
// refreshing via the stored refresh token if needed.
// Implements google.Service for use with google.Client.
func (s *AuthService) GetValidAccessToken(ctx context.Context, userID int64) (string, error) {
	encrypted, _, err := s.userStore.GetGoogleRefreshToken(ctx, userID)
	if err != nil {
		return "", err
	}

	refreshToken, err := google.DecryptToken(encrypted)
	if err != nil {
		return "", err
	}

	tokens, err := google.ExchangeRefreshToken(
		os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		refreshToken,
	)
	if err != nil {
		return "", err
	}
	return tokens.AccessToken, nil
}
