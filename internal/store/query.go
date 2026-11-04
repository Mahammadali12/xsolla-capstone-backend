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

// Course queries

func (q *Query) CreateCourse(ctx context.Context, title string, description string, createdBy int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO courses (title, description, created_by) VALUES ($1, $2, $3) RETURNING id, title, description, created_by, created_at`,
		title, description, createdBy,
	)
}

func (q *Query) GetCourseByID(ctx context.Context, id int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, title, description, created_by, created_at FROM courses WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
}

func (q *Query) GetCoursesForUser(ctx context.Context, userID int64, role string) (*sql.Rows, error) {
	if role == "admin" {
		return q.DBTX.QueryContext(ctx,
			`SELECT c.id, c.title, c.description, c.created_by, c.created_at
			 FROM courses c WHERE c.deleted_at IS NULL`,
		)
	}
	return q.DBTX.QueryContext(ctx,
		`SELECT c.id, c.title, c.description, c.created_by, c.created_at
		 FROM courses c
		 JOIN enrollments e ON e.course_id = c.id
		 WHERE c.deleted_at IS NULL AND e.user_id = $1`,
		userID,
	)
}

func (q *Query) PatchCourse(ctx context.Context, id int64, title string, description string, createdBy int64, isAdmin bool) *sql.Row {
	if isAdmin {
		return q.DBTX.QueryRowContext(ctx,
			`UPDATE courses SET title = COALESCE($2, title), description = COALESCE($3, description)
			 WHERE id = $1 AND deleted_at IS NULL
			 RETURNING id, title, description, created_by, created_at`,
			id, title, description,
		)
	}
	return q.DBTX.QueryRowContext(ctx,
		`UPDATE courses SET title = COALESCE($2, title), description = COALESCE($3, description)
		 WHERE id = $1 AND created_by = $4 AND deleted_at IS NULL
		 RETURNING id, title, description, created_by, created_at`,
		id, title, description, createdBy,
	)
}

func (q *Query) DeleteCourse(ctx context.Context, id int64, createdBy int64, isAdmin bool) (sql.Result, error) {
	if isAdmin {
		return q.DBTX.ExecContext(ctx,
			`UPDATE courses SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
			id,
		)
	}
	return q.DBTX.ExecContext(ctx,
		`UPDATE courses SET deleted_at = NOW() WHERE id = $1 AND created_by = $2 AND deleted_at IS NULL`,
		id, createdBy,
	)
}

// Enrollment queries

func (q *Query) CreateEnrollment(ctx context.Context, courseID int64, userID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO enrollments (course_id, user_id) VALUES ($1, $2) RETURNING id, course_id, user_id, created_at`,
		courseID, userID,
	)
}

func (q *Query) DeleteEnrollment(ctx context.Context, courseID int64, userID int64) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`DELETE FROM enrollments WHERE course_id = $1 AND user_id = $2`,
		courseID, userID,
	)
}

func (q *Query) GetEnrollmentsForCourse(ctx context.Context, courseID int64) (*sql.Rows, error) {
	return q.DBTX.QueryContext(ctx,
		`SELECT e.id, e.course_id, e.user_id, e.created_at, u.email, u.name, u.role
		 FROM enrollments e
		 JOIN users u ON u.id = e.user_id
		 WHERE e.course_id = $1`,
		courseID,
	)
}

func (q *Query) IsUserEnrolled(ctx context.Context, courseID int64, userID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT 1 FROM enrollments WHERE course_id = $1 AND user_id = $2`,
		courseID, userID,
	)
}

func (q *Query) IsUserTeacherOfCourse(ctx context.Context, courseID int64, userID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT 1 FROM courses WHERE id = $1 AND created_by = $2 AND deleted_at IS NULL`,
		courseID, userID,
	)
}

// Content item queries

func (q *Query) CreateContentItem(ctx context.Context, courseID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO content_items (course_id, parent_id, type, title, description, position, is_visible, file_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, course_id, parent_id, type, title, description, position, is_visible, file_id, created_at`,
		courseID, parentID, itemType, title, description, position, isVisible, fileID,
	)
}

func (q *Query) GetContentItemByID(ctx context.Context, id int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, course_id, parent_id, type, title, description, position, is_visible, file_id, created_at
		 FROM content_items WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
}

func (q *Query) GetContentItemsForCourse(ctx context.Context, courseID int64, visibleOnly bool) (*sql.Rows, error) {
	if visibleOnly {
		return q.DBTX.QueryContext(ctx,
			`SELECT id, course_id, parent_id, type, title, description, position, is_visible, file_id, created_at
			 FROM content_items
			 WHERE course_id = $1 AND is_visible = true AND deleted_at IS NULL
			 ORDER BY position`,
			courseID,
		)
	}
	return q.DBTX.QueryContext(ctx,
		`SELECT id, course_id, parent_id, type, title, description, position, is_visible, file_id, created_at
		 FROM content_items
			 WHERE course_id = $1 AND deleted_at IS NULL
		 ORDER BY position`,
		courseID,
	)
}

func (q *Query) GetContentItemChildren(ctx context.Context, parentID int64, visibleOnly bool) (*sql.Rows, error) {
	if visibleOnly {
		return q.DBTX.QueryContext(ctx,
			`SELECT id, course_id, parent_id, type, title, description, position, is_visible, file_id, created_at
			 FROM content_items
			 WHERE parent_id = $1 AND is_visible = true AND deleted_at IS NULL
			 ORDER BY position`,
			parentID,
		)
	}
	return q.DBTX.QueryContext(ctx,
		`SELECT id, course_id, parent_id, type, title, description, position, is_visible, file_id, created_at
			 FROM content_items
			 WHERE parent_id = $1 AND deleted_at IS NULL
		 ORDER BY position`,
		parentID,
	)
}

func (q *Query) UpdateContentItem(ctx context.Context, id int64, title string, description string, position *int, isVisible *bool, parentID *int64, fileID *int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`UPDATE content_items
		 SET title = COALESCE($2, title),
		     description = COALESCE($3, description),
		     position = COALESCE($4, position),
		     is_visible = COALESCE($5, is_visible),
		     parent_id = COALESCE($6, parent_id),
		     file_id = COALESCE($7, file_id)
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING id, course_id, parent_id, type, title, description, position, is_visible, file_id, created_at`,
		id, title, description, position, isVisible, parentID, fileID,
	)
}

func (q *Query) DeleteContentItem(ctx context.Context, id int64) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`UPDATE content_items SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
}

// Assignment queries

func (q *Query) CreateAssignment(ctx context.Context, contentItemID int64, dueAt *time.Time, maxPoints int) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO assignments (content_item_id, due_at, max_points) VALUES ($1, $2, $3)
		 RETURNING content_item_id, due_at, max_points`,
		contentItemID, dueAt, maxPoints,
	)
}

func (q *Query) GetAssignmentByContentItemID(ctx context.Context, contentItemID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT content_item_id, due_at, max_points FROM assignments WHERE content_item_id = $1 AND deleted_at IS NULL`,
		contentItemID,
	)
}

func (q *Query) UpdateAssignment(ctx context.Context, contentItemID int64, dueAt *time.Time, maxPoints int) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`UPDATE assignments
		 SET due_at = COALESCE($2, due_at),
		     max_points = COALESCE($3, max_points)
		 WHERE content_item_id = $1 AND deleted_at IS NULL
		 RETURNING content_item_id, due_at, max_points`,
		contentItemID, dueAt, maxPoints,
	)
}

func (q *Query) DeleteAssignment(ctx context.Context, contentItemID int64) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`UPDATE assignments SET deleted_at = NOW() WHERE content_item_id = $1 AND deleted_at IS NULL`,
		contentItemID,
	)
}

