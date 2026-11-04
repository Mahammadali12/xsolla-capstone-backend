package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// UserStore abstracts user persistence for the service layer.
type UserStore interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, email string, name string, role string, passwordHash string) (int64, error)
	GetUserByEmail(ctx context.Context, email string) (int64, string, string, error)
	GetUserByID(ctx context.Context, id int64) (int64, string, string, string, time.Time, error)
}

// ErrEmailTaken is re-exported from the store package for handler use.
var ErrEmailTaken = store.ErrEmailTaken

// ErrUserNotFound is re-exported from the store package for handler use.
var ErrUserNotFound = store.ErrUserNotFound

// ErrInvalidCredentials is returned when email or password is incorrect.
var ErrInvalidCredentials = errors.New("invalid credentials")

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

// Signup creates a new user. The first registered user becomes admin.
func (s *AuthService) Signup(ctx context.Context, email string, name string, password string) (int64, string, error) {
	count, err := s.userStore.CountUsers(ctx)
	if err != nil {
		return 0, "", err
	}
	role := "student"
	if count == 0 {
		role = "admin"
	}

	hash, err := auth.Hash(password)
	if err != nil {
		return 0, "", err
	}

	id, err := s.userStore.CreateUser(ctx, email, name, role, hash)
	if err != nil {
		if errors.Is(err, store.ErrEmailTaken) {
			return 0, "", store.ErrEmailTaken
		}
		return 0, "", err
	}
	return id, role, nil
}

// Login validates credentials and returns the user ID and role.
func (s *AuthService) Login(ctx context.Context, email string, password string) (int64, string, error) {
	id, hash, role, err := s.userStore.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, store.ErrUserNotFound) {
			return 0, "", err
		}
		return 0, "", err
	}

	if err := auth.Compare(hash, password); err != nil {
		return 0, "", ErrInvalidCredentials
	}
	return id, role, nil
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

// GetUserByID looks up a user by ID.
func (s *AuthService) GetUserByID(ctx context.Context, id int64) (int64, string, string, string, time.Time, error) {
	return s.userStore.GetUserByID(ctx, id)
}
