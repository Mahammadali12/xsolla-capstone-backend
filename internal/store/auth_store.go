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

// User represents a row from the users table, with roles loaded from user_roles.
type User struct {
	ID            int64
	Email         string
	Name          string
	Roles         []string
	RoleExpiresAt map[string]time.Time
	PasswordHash  string
	LeftAt        *time.Time
	DeletedAt     *time.Time
	CreatedAt     time.Time
}

// CreateUser inserts a new user and returns the generated ID.
// Roles are stored in the user_roles junction table (one row per role).
// shortcut: the users insert and user_roles inserts are not in the same
// transaction; if user_roles fails we get an orphan user. Wrap in WithTx if
// atomicity matters.
func (s *StoreImpl) CreateUser(ctx context.Context, email string, name string, roles []string, passwordHash string) (int64, error) {
	var id int64
	err := s.DB().InsertUser(ctx, email, name, passwordHash).Scan(&id)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, ErrEmailTaken
		}
		return 0, err
	}
	for _, role := range roles {
		if _, err := s.DB().InsertUserRole(ctx, id, role); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// GetUserCount returns the total number of users.
func (s *StoreImpl) GetUserCount(ctx context.Context) (int, error) {
	var count int
	err := s.DB().GetUserCount(ctx).Scan(&count)
	return count, err
}

// GetUserByEmail looks up a user by email (case-insensitive).
func (s *StoreImpl) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	var leftAt, deletedAt *time.Time
	err := s.DB().GetUserByEmail(ctx, email).Scan(
		&u.ID, &u.PasswordHash, &u.Name, &u.CreatedAt, &leftAt, &deletedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	u.Email = email
	u.LeftAt = leftAt
	u.DeletedAt = deletedAt
	roles, exps, err := s.GetUserRoles(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	u.RoleExpiresAt = exps
	return &u, nil
}

// GetUserRoles returns a user's roles and their expiries (only roles with an
// expiry date appear in the map).
func (s *StoreImpl) GetUserRoles(ctx context.Context, userID int64) ([]string, map[string]time.Time, error) {
	rows, err := s.DB().GetUserRoles(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	roles := []string{}
	expiries := map[string]time.Time{}
	for rows.Next() {
		var role string
		var expiresAt *time.Time
		if err := rows.Scan(&role, &expiresAt); err != nil {
			return nil, nil, err
		}
		roles = append(roles, role)
		if expiresAt != nil {
			expiries[role] = *expiresAt
		}
	}
	return roles, expiries, nil
}

// SaveRefreshToken stores a refresh token hash for a user.
func (s *StoreImpl) SaveRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := s.DB().InsertRefreshToken(ctx, userID, tokenHash, expiresAt)
	return err
}

// DeleteRefreshTokensForUser removes all refresh tokens for a user.
func (s *StoreImpl) DeleteRefreshTokensForUser(ctx context.Context, userID int64) error {
	_, err := s.DB().DeleteRefreshTokensForUser(ctx, userID)
	return err
}

// DeleteRefreshToken removes a refresh token by hash.
func (s *StoreImpl) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := s.DB().DeleteRefreshToken(ctx, tokenHash)
	return err
}

// GetUserByID looks up a user by ID.
func (s *StoreImpl) GetUserByID(ctx context.Context, id int64) (*User, error) {
	var u User
	var leftAt, deletedAt *time.Time
	err := s.DB().GetUserByID(ctx, id).Scan(
		&u.ID, &u.Email, &u.Name, &u.CreatedAt, &leftAt, &deletedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	u.LeftAt = leftAt
	u.DeletedAt = deletedAt
	roles, exps, err := s.GetUserRoles(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	u.RoleExpiresAt = exps
	return &u, nil
}

// FindRefreshTokenByHash looks up an active refresh token by hash.
func (s *StoreImpl) FindRefreshTokenByHash(ctx context.Context, tokenHash string) (int64, string, time.Time, error) {
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
