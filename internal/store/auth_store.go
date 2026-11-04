package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

var (
	// ErrUserNotFound is returned when a user does not exist.
	ErrUserNotFound = errors.New("user not found")
	// ErrEmailTaken is returned when a duplicate email is inserted.
	ErrEmailTaken = errors.New("email already taken")
	// ErrRefreshTokenNotFound is returned when a refresh token is missing or expired.
	ErrRefreshTokenNotFound = errors.New("refresh token not found")
)

// IsDuplicateEmail checks whether an error is a Postgres unique violation on email.
func IsDuplicateEmail(err error) bool {
	return errors.Is(err, ErrEmailTaken)
}

// CreateUser inserts a new user and returns the generated ID.
func (s *Store) CreateUser(ctx context.Context, email string, name string, role string, passwordHash string) (int64, error) {
	var id int64
	err := s.DB().InsertUser(ctx, email, name, role, passwordHash).Scan(&id)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, ErrEmailTaken
		}
		return 0, err
	}
	return id, nil
}

// CountUsers returns the total number of users.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	err := s.DB().GetUserCount(ctx).Scan(&count)
	return count, err
}

// GetUserByEmail looks up a user by email (case-insensitive).
func (s *Store) GetUserByEmail(ctx context.Context, email string) (int64, string, string, error) {
	var id int64
	var hash, role string
	err := s.DB().GetUserByEmail(ctx, email).Scan(&id, &hash, &role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", "", ErrUserNotFound
		}
		return 0, "", "", err
	}
	return id, hash, role, nil
}

// SaveRefreshToken stores a refresh token hash for a user.
func (s *Store) SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := s.DB().InsertRefreshToken(ctx, userID, tokenHash, expiresAt)
	return err
}

// DeleteRefreshTokensForUser removes all refresh tokens for a user.
func (s *Store) DeleteRefreshTokensForUser(ctx context.Context, userID int64) error {
	_, err := s.DB().DeleteRefreshTokensForUser(ctx, userID)
	return err
}

// DeleteRefreshToken removes a refresh token by hash.
func (s *Store) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := s.DB().DeleteRefreshToken(ctx, tokenHash)
	return err
}

// GetUserByID looks up a user by ID.
func (s *Store) GetUserByID(ctx context.Context, id int64) (int64, string, string, string, time.Time, error) {
	var userID int64
	var email, name, role string
	var createdAt time.Time
	err := s.DB().GetUserByID(ctx, id).Scan(&userID, &email, &name, &role, &createdAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", "", "", time.Time{}, ErrUserNotFound
		}
		return 0, "", "", "", time.Time{}, err
	}
	return userID, email, name, role, createdAt, nil
}

// FindRefreshTokenByHash looks up an active refresh token by hash.
func (s *Store) FindRefreshTokenByHash(ctx context.Context, tokenHash string) (int64, string, time.Time, error) {
	var userID int64
	var tokenHashOut string
	var expiresAt time.Time
	err := s.DB().FindRefreshTokenByHash(ctx, tokenHash).Scan(&userID, &tokenHashOut, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", time.Time{}, ErrRefreshTokenNotFound
		}
		return 0, "", time.Time{}, err
	}
	return userID, tokenHashOut, expiresAt, nil
}


