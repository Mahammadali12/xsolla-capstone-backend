package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

// CreateUser inserts a new user and its roles atomically (user + user_roles).
// shortcut: transaction is opened inside the store; callers cannot supply their
// own tx. Wrap at a higher layer if CreateUser must join a larger tx.
func (s *StoreImpl) CreateUser(ctx context.Context, email string, name string, roles []string, passwordHash string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	q := WithTx(tx)

	var id int64
	err = q.InsertUser(ctx, email, name, passwordHash).Scan(&id)
	if err != nil {
		tx.Rollback()
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return 0, ErrEmailTaken
		}
		return 0, err
	}
	for _, role := range roles {
		if _, err := q.InsertUserRole(ctx, id, role); err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// GetUsers returns all users filtered by search / role / status, with effective
// roles applied (expired roles removed). search matches name or email
// case-insensitively. status: "active" (default), "left", "all".
func (s *StoreImpl) GetUsers(ctx context.Context, search, role, status string) ([]User, error) {
	rows, err := s.DB().GetUsers(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	type rawUser struct {
		u     User
		roles []string
	}
	raw := []rawUser{}
	for rows.Next() {
		var u User
		var leftAt, deletedAt *time.Time
		var rolesStr []string
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &leftAt, &deletedAt, pq.Array(&rolesStr)); err != nil {
			return nil, err
		}
		u.LeftAt = leftAt
		u.DeletedAt = deletedAt
		raw = append(raw, rawUser{u: u, roles: rolesStr})
	}

	// Load all non-null expiries, grouped by user.
	expiryRows, err := s.DB().GetUserRoleExpiriesAll(ctx)
	if err != nil {
		return nil, err
	}
	defer expiryRows.Close()
	expiriesByUser := map[int64]map[string]time.Time{}
	for expiryRows.Next() {
		var userID int64
		var role string
		var expiresAt time.Time
		if err := expiryRows.Scan(&userID, &role, &expiresAt); err != nil {
			return nil, err
		}
		if expiriesByUser[userID] == nil {
			expiriesByUser[userID] = map[string]time.Time{}
		}
		expiriesByUser[userID][role] = expiresAt
	}

	result := []User{}
	for _, r := range raw {
		u := r.u
		u.RoleExpiresAt = map[string]time.Time{}
		effective := []string{}
		for _, roleName := range r.roles {
			exp, hasExp := expiriesByUser[u.ID][roleName]
			if hasExp && !exp.After(now) {
				continue // role expired
			}
			effective = append(effective, roleName)
			if hasExp {
				u.RoleExpiresAt[roleName] = exp
			}
		}
		u.Roles = effective

		// Filters (effective roles already reflect expiry).
		if search != "" {
			sLow := strings.ToLower(search)
			if !strings.Contains(strings.ToLower(u.Name), sLow) && !strings.Contains(strings.ToLower(u.Email), sLow) {
				continue
			}
		}
		if role != "" {
			found := false
			for _, rn := range u.Roles {
				if rn == role {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if status == "active" && (u.LeftAt != nil || u.DeletedAt != nil) {
			continue
		}
		if status == "left" && u.LeftAt == nil {
			continue
		}
		result = append(result, u)
	}
	return result, nil
}

// UpdateUser updates name and/or email, returning the updated user. Nil
// pointers leave the corresponding field unchanged.
func (s *StoreImpl) UpdateUser(ctx context.Context, id int64, name, email *string) (*User, error) {
	var u User
	var leftAt, deletedAt *time.Time
	err := s.DB().UpdateUser(ctx, id, name, email).Scan(
		&u.ID, &u.Email, &u.Name, &u.CreatedAt, &leftAt, &deletedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
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

// UpdateUserPassword replaces the user's password hash.
func (s *StoreImpl) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) (sql.Result, error) {
	return s.DB().UpdateUserPassword(ctx, id, passwordHash)
}

// SetUserLeftAt deactivates (left_at=NOW) or reactivates (left_at=NULL) a user.
func (s *StoreImpl) SetUserLeftAt(ctx context.Context, id int64, deactivate bool) (sql.Result, error) {
	return s.DB().SetUserLeftAt(ctx, id, deactivate)
}

// HardDeleteUser permanently deletes a user and everything that references them.
// Course content (modules, files) is transferred to transferTo; personal data
// (roles, refresh tokens, Google tokens, enrollments, team membership and
// submissions) is removed by the schema's cascades; mentor slots are deleted
// and bookings the user made are cleared. Everything runs in one transaction.
func (s *StoreImpl) HardDeleteUser(ctx context.Context, id int64, transferTo int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	steps := []struct {
		query string
		args  []any
	}{
		{`UPDATE modules SET created_by = $2 WHERE created_by = $1`, []any{id, transferTo}},
		{`UPDATE files SET uploaded_by = $2 WHERE uploaded_by = $1`, []any{id, transferTo}},
		{`UPDATE content_items SET created_by = NULL WHERE created_by = $1`, []any{id}},
		{`UPDATE submissions SET graded_by = NULL WHERE graded_by = $1`, []any{id}},
		{`DELETE FROM mentor_slots WHERE mentor_id = $1`, []any{id}},
		{`UPDATE mentor_slots SET booked_by = NULL, meeting_url = NULL, google_event_id = NULL WHERE booked_by = $1`, []any{id}},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return rollback(err)
		}
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return rollback(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return rollback(err)
	}
	if rows == 0 {
		return rollback(ErrUserNotFound)
	}
	return tx.Commit()
}

// CountAdmins counts non-deleted users holding the admin role.
func (s *StoreImpl) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.DB().CountAdmins(ctx).Scan(&count)
	return count, err
}

// DeleteUserRoles removes all role assignments for a user.
func (s *StoreImpl) DeleteUserRoles(ctx context.Context, userID int64) (sql.Result, error) {
	return s.DB().DeleteUserRoles(ctx, userID)
}

// ReplaceUserRoles atomically replaces a user's role set with optional expiries.
func (s *StoreImpl) ReplaceUserRoles(ctx context.Context, userID int64, roles []string, expiries map[string]time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	q := WithTx(tx)
	if _, err := q.DeleteUserRoles(ctx, userID); err != nil {
		tx.Rollback()
		return err
	}
	for _, role := range roles {
		var expiresAt interface{}
		if exp, ok := expiries[role]; ok && !exp.IsZero() {
			expiresAt = exp
		}
		if _, err := q.DBTX.ExecContext(ctx,
			`INSERT INTO user_roles (user_id, role, expires_at) VALUES ($1, $2, $3)`,
			userID, role, expiresAt,
		); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
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

// SaveGoogleRefreshToken stores an encrypted Google refresh token for a user.
func (s *StoreImpl) SaveGoogleRefreshToken(ctx context.Context, userID int64, encryptedToken string, expiresAt time.Time) error {
	q := s.DB()
	_, err := q.UpsertGoogleRefreshToken(ctx, userID, encryptedToken, expiresAt)
	return err
}

// GetGoogleRefreshToken retrieves the encrypted Google refresh token for a user.
func (s *StoreImpl) GetGoogleRefreshToken(ctx context.Context, userID int64) (string, time.Time, error) {
	q := s.DB()
	var encryptedToken string
	var expiresAt time.Time
	err := q.GetGoogleRefreshToken(ctx, userID).Scan(&encryptedToken, &expiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", time.Time{}, ErrRefreshTokenNotFound
		}
		return "", time.Time{}, err
	}
	return encryptedToken, expiresAt, nil
}
