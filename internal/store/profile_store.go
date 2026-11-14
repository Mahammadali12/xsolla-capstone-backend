package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lib/pq"
)

// ErrProfileNotFound is returned when the person has never saved a profile (the defaults apply).
var ErrProfileNotFound = errors.New("profile not found")

// UserProfile is the "about me" part of a person's profile.
type UserProfile struct {
	UserID       int64
	Bio          string
	SlackHandle  string
	GitlabHandle string
	Visibility   string
	AvatarKey    *string
}

// ProfilePatch holds the fields to change; nil fields stay as they are.
type ProfilePatch struct {
	Bio          *string
	SlackHandle  *string
	GitlabHandle *string
	Visibility   *string
}

// AvatarVisibility is a person's picture and who may see it.
type AvatarVisibility struct {
	UserID     int64
	AvatarKey  string
	Visibility string
}

const profileColumns = `user_id, bio, slack_handle, gitlab_handle, visibility, avatar_key`

func scanProfile(row interface{ Scan(...any) error }) (*UserProfile, error) {
	var p UserProfile
	if err := row.Scan(&p.UserID, &p.Bio, &p.SlackHandle, &p.GitlabHandle, &p.Visibility, &p.AvatarKey); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetUserProfile returns the saved profile, or ErrProfileNotFound when there is none yet.
func (s *StoreImpl) GetUserProfile(ctx context.Context, userID int64) (*UserProfile, error) {
	p, err := scanProfile(s.db.QueryRowContext(ctx,
		`SELECT `+profileColumns+` FROM user_profiles WHERE user_id = $1`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrProfileNotFound
	}
	return p, err
}

// UpdateUserProfile creates the profile if needed and changes only the fields set in the patch.
func (s *StoreImpl) UpdateUserProfile(ctx context.Context, userID int64, patch ProfilePatch) (*UserProfile, error) {
	return scanProfile(s.db.QueryRowContext(ctx, `
		INSERT INTO user_profiles (user_id, bio, slack_handle, gitlab_handle, visibility)
		VALUES ($1, COALESCE($2, ''), COALESCE($3, ''), COALESCE($4, ''), COALESCE($5, 'everyone'))
		ON CONFLICT (user_id) DO UPDATE SET
			bio           = COALESCE($2, user_profiles.bio),
			slack_handle  = COALESCE($3, user_profiles.slack_handle),
			gitlab_handle = COALESCE($4, user_profiles.gitlab_handle),
			visibility    = COALESCE($5, user_profiles.visibility),
			updated_at    = NOW()
		RETURNING `+profileColumns,
		userID, patch.Bio, patch.SlackHandle, patch.GitlabHandle, patch.Visibility))
}

// SetUserAvatar stores the new picture key (nil removes the picture) and returns the key it replaced, if any.
// The row is locked while it is swapped, so two uploads at once cannot lose track of a file.
func (s *StoreImpl) SetUserAvatar(ctx context.Context, userID int64, key *string) (old *string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if key == nil {
		// removing: nothing to create when the person never had a profile
		err = tx.QueryRowContext(ctx, `SELECT avatar_key FROM user_profiles WHERE user_id = $1 FOR UPDATE`, userID).Scan(&old)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, tx.Commit()
		}
	} else {
		if _, err = tx.ExecContext(ctx, `INSERT INTO user_profiles (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, userID); err != nil {
			return nil, err
		}
		err = tx.QueryRowContext(ctx, `SELECT avatar_key FROM user_profiles WHERE user_id = $1 FOR UPDATE`, userID).Scan(&old)
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE user_profiles SET avatar_key = $2, updated_at = NOW() WHERE user_id = $1`, userID, key); err != nil {
		return nil, err
	}
	return old, tx.Commit()
}

// IsCurrentAvatar reports whether the key is someone's current picture (only those are served publicly).
func (s *StoreImpl) IsCurrentAvatar(ctx context.Context, key string) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM user_profiles WHERE avatar_key = $1)`, key).Scan(&ok)
	return ok, err
}

// ListAvatars returns the pictures of the given people who have one, with their visibility.
func (s *StoreImpl) ListAvatars(ctx context.Context, userIDs []int64) ([]AvatarVisibility, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id, avatar_key, visibility FROM user_profiles WHERE user_id = ANY($1) AND avatar_key IS NOT NULL`,
		pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AvatarVisibility
	for rows.Next() {
		var a AvatarVisibility
		if err := rows.Scan(&a.UserID, &a.AvatarKey, &a.Visibility); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetUserPreferences returns the saved settings JSON, or nil when the person never saved any.
func (s *StoreImpl) GetUserPreferences(ctx context.Context, userID int64) ([]byte, error) {
	var prefs []byte
	err := s.db.QueryRowContext(ctx, `SELECT prefs FROM user_preferences WHERE user_id = $1`, userID).Scan(&prefs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return prefs, err
}

// MergeUserPreferences adds the given keys to the saved settings (creating the row on first use) and returns
// the whole saved object. Keys that are not sent keep their value.
func (s *StoreImpl) MergeUserPreferences(ctx context.Context, userID int64, patch []byte) ([]byte, error) {
	var prefs []byte
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO user_preferences (user_id, prefs) VALUES ($1, $2::jsonb)
		ON CONFLICT (user_id) DO UPDATE SET prefs = user_preferences.prefs || EXCLUDED.prefs, updated_at = NOW()
		RETURNING prefs`,
		userID, string(patch)).Scan(&prefs)
	return prefs, err
}
