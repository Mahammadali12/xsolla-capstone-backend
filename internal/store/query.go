package store

import (
	"context"
	"database/sql"
	"time"
)

// DBTX abstracts over *sql.DB and *sql.Tx so queries work in transactions too.
type DBTX interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Query holds SQL statements for the LMS schema.
type Query struct {
	DBTX DBTX
}

func (q *Query) InsertUser(ctx context.Context, email string, name string, role string, passwordHash string) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO users (email, name, role, password_hash) VALUES ($1, $2, $3, $4) RETURNING id`,
		email, name, role, passwordHash,
	)
}

func (q *Query) GetUserByEmail(ctx context.Context, email string) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, password_hash, role FROM users WHERE lower(email) = $1`,
		email,
	)
}

func (q *Query) GetUserCount(ctx context.Context) *sql.Row {
	return q.DBTX.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`)
}


func (q *Query) InsertRefreshToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		userID, tokenHash, expiresAt,
	)
}

func (q *Query) DeleteRefreshTokensForUser(ctx context.Context, userID int64) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE user_id = $1`,
		userID,
	)
}

func (q *Query) FindRefreshTokenByHash(ctx context.Context, tokenHash string) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT user_id, token_hash, expires_at FROM refresh_tokens WHERE expires_at > NOW() AND token_hash = $1`,
		tokenHash,
	)
}

func (q *Query) DeleteRefreshToken(ctx context.Context, tokenHash string) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`DELETE FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	)
}

func (q *Query) CountRefreshTokens(ctx context.Context) *sql.Row {
	return q.DBTX.QueryRowContext(ctx, `SELECT COUNT(*) FROM refresh_tokens WHERE expires_at > NOW()`)
}

func (q *Query) GetUserByID(ctx context.Context, id int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, email, name, role, created_at FROM users WHERE id = $1`,
		id,
	)
}
