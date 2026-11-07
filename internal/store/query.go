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

func (q *Query) InsertUser(ctx context.Context, email string, name string, roles []string, passwordHash string) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO users (email, name, roles, password_hash) VALUES ($1, $2, $3, $4) RETURNING id`,
		email, name, roles, passwordHash,
	)
}

func (q *Query) GetUserByEmail(ctx context.Context, email string) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, password_hash, roles FROM users WHERE lower(email) = $1`,
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
		`SELECT id, email, name, roles, created_at FROM users WHERE id = $1`,
		id,
	)
}

// Module queries

func (q *Query) CreateModule(ctx context.Context, title string, description string, createdBy int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO modules (title, description, created_by) VALUES ($1, $2, $3) RETURNING id, title, description, created_by, created_at`,
		title, description, createdBy,
	)
}

func (q *Query) GetModuleByID(ctx context.Context, id int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, title, description, created_by, created_at FROM modules WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
}

func (q *Query) GetModulesForUser(ctx context.Context, userID int64, roles []string) (*sql.Rows, error) {
	isAdmin := false
	for _, r := range roles {
		if r == "admin" {
			isAdmin = true
			break
		}
	}
	if isAdmin {
		return q.DBTX.QueryContext(ctx,
			`SELECT m.id, m.title, m.description, m.created_by, m.created_at
			 FROM modules m WHERE m.deleted_at IS NULL`,
		)
	}
	return q.DBTX.QueryContext(ctx,
		`SELECT m.id, m.title, m.description, m.created_by, m.created_at
		 FROM modules m
		 JOIN enrollments e ON e.module_id = m.id
		 WHERE m.deleted_at IS NULL AND e.user_id = $1`,
		userID,
	)
}

func (q *Query) PatchModule(ctx context.Context, id int64, title string, description string, createdBy int64, isAdmin bool) *sql.Row {
	if isAdmin {
		return q.DBTX.QueryRowContext(ctx,
			`UPDATE modules SET title = COALESCE($2, title), description = COALESCE($3, description)
			 WHERE id = $1 AND deleted_at IS NULL
			 RETURNING id, title, description, created_by, created_at`,
			id, title, description,
		)
	}
	return q.DBTX.QueryRowContext(ctx,
		`UPDATE modules SET title = COALESCE($2, title), description = COALESCE($3, description)
		 WHERE id = $1 AND created_by = $4 AND deleted_at IS NULL
		 RETURNING id, title, description, created_by, created_at`,
		id, title, description, createdBy,
	)
}

func (q *Query) DeleteModule(ctx context.Context, id int64, createdBy int64, isAdmin bool) (sql.Result, error) {
	if isAdmin {
		return q.DBTX.ExecContext(ctx,
			`UPDATE modules SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
			id,
		)
	}
	return q.DBTX.ExecContext(ctx,
		`UPDATE modules SET deleted_at = NOW() WHERE id = $1 AND created_by = $2 AND deleted_at IS NULL`,
		id, createdBy,
	)
}

// Enrollment queries

func (q *Query) CreateEnrollment(ctx context.Context, moduleID int64, userID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO enrollments (module_id, user_id) VALUES ($1, $2) RETURNING id, module_id, user_id, created_at`,
		moduleID, userID,
	)
}

func (q *Query) DeleteEnrollment(ctx context.Context, moduleID int64, userID int64) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`DELETE FROM enrollments WHERE module_id = $1 AND user_id = $2`,
		moduleID, userID,
	)
}

func (q *Query) GetEnrollmentsForModule(ctx context.Context, moduleID int64) (*sql.Rows, error) {
	return q.DBTX.QueryContext(ctx,
		`SELECT e.id, e.module_id, e.user_id, e.created_at, u.email, u.name, u.role
		 FROM enrollments e
		 JOIN users u ON u.id = e.user_id
		 WHERE e.module_id = $1`,
		moduleID,
	)
}

func (q *Query) IsUserEnrolled(ctx context.Context, moduleID int64, userID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT 1 FROM enrollments WHERE module_id = $1 AND user_id = $2`,
		moduleID, userID,
	)
}

func (q *Query) IsUserTeacherOfModule(ctx context.Context, moduleID int64, userID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT 1 FROM modules WHERE id = $1 AND created_by = $2 AND deleted_at IS NULL`,
		moduleID, userID,
	)
}

// Content item queries

func (q *Query) CreateContentItem(ctx context.Context, moduleID int64, parentID *int64, itemType string, title string, description string, position int, isVisible bool, fileID *int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO content_items (module_id, parent_id, type, title, description, position, is_visible, file_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, module_id, parent_id, type, title, description, position, is_visible, file_id, created_at`,
		moduleID, parentID, itemType, title, description, position, isVisible, fileID,
	)
}

func (q *Query) GetContentItemByID(ctx context.Context, id int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT ci.id, ci.module_id, ci.parent_id, ci.type, ci.title, ci.description, ci.position, ci.is_visible, ci.file_id, ci.created_at,
			 a.due_at, COALESCE(a.max_points, 0)
		 FROM content_items ci
		 LEFT JOIN assignments a ON a.content_item_id = ci.id
		 WHERE ci.id = $1 AND ci.deleted_at IS NULL`,
		id,
	)
}

func (q *Query) GetContentItemsForModule(ctx context.Context, moduleID int64, visibleOnly bool) (*sql.Rows, error) {
	if visibleOnly {
		return q.DBTX.QueryContext(ctx,
			`SELECT ci.id, ci.module_id, ci.parent_id, ci.type, ci.title, ci.description, ci.position, ci.is_visible, ci.file_id, ci.created_at,
			 a.due_at, COALESCE(a.max_points, 0)
			 FROM content_items ci
			 LEFT JOIN assignments a ON a.content_item_id = ci.id
			 WHERE ci.module_id = $1 AND ci.is_visible = true AND ci.deleted_at IS NULL
			 ORDER BY ci.position`,
			moduleID,
		)
	}
	return q.DBTX.QueryContext(ctx,
		`SELECT ci.id, ci.module_id, ci.parent_id, ci.type, ci.title, ci.description, ci.position, ci.is_visible, ci.file_id, ci.created_at,
			 a.due_at, COALESCE(a.max_points, 0)
			 FROM content_items ci
			 LEFT JOIN assignments a ON a.content_item_id = ci.id
			 WHERE ci.module_id = $1 AND ci.deleted_at IS NULL
		 ORDER BY ci.position`,
		moduleID,
	)
}

func (q *Query) GetContentItemChildren(ctx context.Context, parentID int64, visibleOnly bool) (*sql.Rows, error) {
	if visibleOnly {
		return q.DBTX.QueryContext(ctx,
			`SELECT ci.id, ci.module_id, ci.parent_id, ci.type, ci.title, ci.description, ci.position, ci.is_visible, ci.file_id, ci.created_at,
			 a.due_at, COALESCE(a.max_points, 0)
			 FROM content_items ci
			 LEFT JOIN assignments a ON a.content_item_id = ci.id
			 WHERE ci.parent_id = $1 AND ci.is_visible = true AND ci.deleted_at IS NULL
			 ORDER BY ci.position`,
			parentID,
		)
	}
	return q.DBTX.QueryContext(ctx,
		`SELECT ci.id, ci.module_id, ci.parent_id, ci.type, ci.title, ci.description, ci.position, ci.is_visible, ci.file_id, ci.created_at,
			 a.due_at, COALESCE(a.max_points, 0)
			 FROM content_items ci
			 LEFT JOIN assignments a ON a.content_item_id = ci.id
			 WHERE ci.parent_id = $1 AND ci.deleted_at IS NULL
		 ORDER BY ci.position`,
		parentID,
	)
}

func (q *Query) UpdateContentItem(ctx context.Context, id int64, title string, description string, position *int, isVisible *bool, parentID *int64, fileID *int64, dueAt *time.Time) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`UPDATE content_items AS ci
		 SET title = COALESCE($2, title),
		     description = COALESCE($3, description),
		     position = COALESCE($4, position),
		     is_visible = COALESCE($5, is_visible),
		     parent_id = COALESCE($6, parent_id),
		     file_id = COALESCE($7, file_id)
		 WHERE ci.id = $1 AND ci.deleted_at IS NULL
		 RETURNING ci.id, ci.module_id, ci.parent_id, ci.type, ci.title, ci.description, ci.position, ci.is_visible, ci.file_id, ci.created_at,
		     (SELECT a.due_at FROM assignments a WHERE a.content_item_id = ci.id AND a.deleted_at IS NULL),
		     COALESCE((SELECT a.max_points FROM assignments a WHERE a.content_item_id = ci.id AND a.deleted_at IS NULL), 0)`,
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

// File queries

func (q *Query) CreateFile(ctx context.Context, uploadedBy int64, originalName, storageKey, mimeType string, sizeBytes int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO files (uploaded_by, original_name, storage_key, mime_type, size_bytes)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, uploaded_by, original_name, storage_key, mime_type, size_bytes, created_at`,
		uploadedBy, originalName, storageKey, mimeType, sizeBytes,
	)
}

func (q *Query) GetFileByID(ctx context.Context, id int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, uploaded_by, original_name, storage_key, mime_type, size_bytes, created_at
		 FROM files WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
}

func (q *Query) GetFilesForUser(ctx context.Context, uploadedBy int64) (*sql.Rows, error) {
	return q.DBTX.QueryContext(ctx,
		`SELECT id, uploaded_by, original_name, storage_key, mime_type, size_bytes, created_at
		 FROM files WHERE uploaded_by = $1 AND deleted_at IS NULL
		 ORDER BY created_at DESC`,
		uploadedBy,
	)
}

func (q *Query) DeleteFile(ctx context.Context, id int64) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`UPDATE files SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
}

func (q *Query) GetContentItemForFile(ctx context.Context, fileID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, module_id FROM content_items WHERE file_id = $1 AND deleted_at IS NULL LIMIT 1`,
		fileID,
	)
}

func (q *Query) GetSubmissionForFile(ctx context.Context, fileID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT s.id, s.student_id, s.content_item_id
		 FROM submissions s
		 JOIN submission_files sf ON sf.submission_id = s.id
		 WHERE sf.file_id = $1 AND s.deleted_at IS NULL LIMIT 1`,
		fileID,
	)
}

func (q *Query) FileReferencedCount(ctx context.Context, fileID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM content_items WHERE file_id = $1 AND deleted_at IS NULL) +
		       (SELECT COUNT(*) FROM submission_files WHERE file_id = $1)`,
		fileID,
	)
}

// Submission queries

func (q *Query) CreateSubmission(ctx context.Context, contentItemID int64, studentID int64, textBody string) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO submissions (content_item_id, student_id, text_body)
		 VALUES ($1, $2, $3)
		 RETURNING id, content_item_id, student_id, text_body, submitted_at, grade, feedback, graded_by, graded_at, created_at`,
		contentItemID, studentID, textBody,
	)
}

func (q *Query) GetSubmissionByID(ctx context.Context, id int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, content_item_id, student_id, text_body, submitted_at, grade, feedback, graded_by, graded_at, created_at
		 FROM submissions WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
}

func (q *Query) GetSubmissionByStudentAndAssignment(ctx context.Context, studentID int64, contentItemID int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, content_item_id, student_id, text_body, submitted_at, grade, feedback, graded_by, graded_at, created_at
		 FROM submissions
		 WHERE student_id = $1 AND content_item_id = $2 AND deleted_at IS NULL`,
		studentID, contentItemID,
	)
}

func (q *Query) GetSubmissionsForAssignment(ctx context.Context, contentItemID int64) (*sql.Rows, error) {
	return q.DBTX.QueryContext(ctx,
		`SELECT s.id, s.content_item_id, s.student_id, s.text_body, s.submitted_at, s.grade, s.feedback, s.graded_by, s.graded_at, s.created_at,
		       u.id, u.email, u.name, u.role
		 FROM submissions s
		 JOIN users u ON u.id = s.student_id
		 WHERE s.content_item_id = $1 AND s.deleted_at IS NULL
		 ORDER BY u.name`,
		contentItemID,
	)
}

func (q *Query) UpdateSubmissionGrade(ctx context.Context, id int64, grade *float64, feedback string, gradedBy int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`UPDATE submissions
		 SET grade = COALESCE($2, grade),
		     feedback = COALESCE($3, feedback),
		     graded_by = $4,
		     graded_at = NOW()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING id, content_item_id, student_id, text_body, submitted_at, grade, feedback, graded_by, graded_at, created_at`,
		id, grade, feedback, gradedBy,
	)
}

func (q *Query) DeleteSubmission(ctx context.Context, id int64) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`UPDATE submissions SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
}

// Submission file queries

func (q *Query) CreateSubmissionFile(ctx context.Context, submissionID int64, fileID int64) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`INSERT INTO submission_files (submission_id, file_id) VALUES ($1, $2)`,
		submissionID, fileID,
	)
}

func (q *Query) DeleteSubmissionFiles(ctx context.Context, submissionID int64) (sql.Result, error) {
	return q.DBTX.ExecContext(ctx,
		`DELETE FROM submission_files WHERE submission_id = $1`,
		submissionID,
	)
}

func (q *Query) GetSubmissionFiles(ctx context.Context, submissionID int64) (*sql.Rows, error) {
	return q.DBTX.QueryContext(ctx,
		`SELECT f.id, f.uploaded_by, f.original_name, f.storage_key, f.mime_type, f.size_bytes, f.created_at
		 FROM submission_files sf
		 JOIN files f ON f.id = sf.file_id
		 WHERE sf.submission_id = $1 AND f.deleted_at IS NULL`,
		submissionID,
	)
}

// Event queries

func (q *Query) CreateEvent(ctx context.Context, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`INSERT INTO events (title, kind, starts_at, ends_at, all_day, location, description, module_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, title, kind, starts_at, ends_at, all_day, location, description, module_id, created_at`,
		title, kind, startsAt, endsAt, allDay, location, description, moduleID,
	)
}

func (q *Query) GetEventByID(ctx context.Context, id int64) *sql.Row {
	return q.DBTX.QueryRowContext(ctx,
		`SELECT id, title, kind, starts_at, ends_at, all_day, location, description, module_id, created_at
		 FROM events WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
}

func (q *Query) ListEventsByRange(ctx context.Context, from time.Time, to time.Time, userID int64, roles []string) (*sql.Rows, error) {
	isAdmin := false
	isLecturer := false
	for _, r := range roles {
		if r == "admin" {
			isAdmin = true
		}
		if r == "lecturer" {
			isLecturer = true
		}
	}
	if isAdmin {
		return q.DBTX.QueryContext(ctx,
			`SELECT e.id, e.title, e.kind, e.starts_at, e.ends_at, e.all_day, e.location, e.description, e.module_id, e.created_at
			 FROM events e
			 WHERE e.deleted_at IS NULL
			   AND e.starts_at <= $2
			   AND e.ends_at >= $1
			 ORDER BY e.starts_at`,
			from, to,
		)
	}
	// Lecturer: events for modules they teach
	if isLecturer {
		return q.DBTX.QueryContext(ctx,
			`SELECT e.id, e.title, e.kind, e.starts_at, e.ends_at, e.all_day, e.location, e.description, e.module_id, e.created_at
			 FROM events e
			 LEFT JOIN modules m ON m.id = e.module_id
			 WHERE e.deleted_at IS NULL
			   AND e.starts_at <= $3
			   AND e.ends_at >= $2
			   AND (m.id IS NULL OR m.deleted_at IS NULL)
			   AND (m.id IS NULL OR m.id IN (
			     SELECT m.id FROM modules m WHERE m.created_by = $1 AND m.deleted_at IS NULL
			   ))
			 ORDER BY e.starts_at`,
			userID, from, to,
		)
	}
	// Student: only events from modules in modules they're enrolled in
	return q.DBTX.QueryContext(ctx,
		`SELECT e.id, e.title, e.kind, e.starts_at, e.ends_at, e.all_day, e.location, e.description, e.module_id, e.created_at
		 FROM events e
		 LEFT JOIN modules m ON m.id = e.module_id
		 WHERE e.deleted_at IS NULL
		   AND e.starts_at <= $3
		   AND e.ends_at >= $2
		   AND (m.id IS NULL OR m.deleted_at IS NULL)
		   AND (m.id IS NULL OR m.id IN (
		     SELECT m.id FROM modules m
		     JOIN enrollments en ON en.module_id = m.id
		     WHERE m.deleted_at IS NULL AND en.user_id = $1
		   ))
		 ORDER BY e.starts_at`,
		userID, from, to,
	)
}

func (q *Query) UpdateEvent(ctx context.Context, id int64, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64, userID int64, isAdmin bool) *sql.Row {
	if isAdmin {
		return q.DBTX.QueryRowContext(ctx,
			`UPDATE events
			 SET title = COALESCE($2, title),
			     kind = COALESCE($3, kind),
			     starts_at = COALESCE($4, starts_at),
			     ends_at = COALESCE($5, ends_at),
			     all_day = COALESCE($6, all_day),
			     location = COALESCE($7, location),
			     description = COALESCE($8, description),
			     module_id = COALESCE($9, module_id)
			 WHERE id = $1 AND deleted_at IS NULL
			 RETURNING id, title, kind, starts_at, ends_at, all_day, location, description, module_id, created_at`,
			id, title, kind, startsAt, endsAt, allDay, location, description, moduleID,
		)
	}
	return q.DBTX.QueryRowContext(ctx,
		`UPDATE events
		 SET title = COALESCE($2, title),
		     kind = COALESCE($3, kind),
		     starts_at = COALESCE($4, starts_at),
		     ends_at = COALESCE($5, ends_at),
		     all_day = COALESCE($6, all_day),
		     location = COALESCE($7, location),
		     description = COALESCE($8, description),
		     module_id = COALESCE($9, module_id)
		 WHERE id = $1 AND deleted_at IS NULL
		   AND module_id IN (
		     SELECT m.id FROM modules m WHERE m.created_by = $10 AND m.deleted_at IS NULL
		   )
		 RETURNING id, title, kind, starts_at, ends_at, all_day, location, description, module_id, created_at`,
		id, title, kind, startsAt, endsAt, allDay, location, description, moduleID, userID,
	)
}

func (q *Query) DeleteEvent(ctx context.Context, id int64, userID int64, isAdmin bool) (sql.Result, error) {
	if isAdmin {
		return q.DBTX.ExecContext(ctx,
			`UPDATE events SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
			id,
		)
	}
	return q.DBTX.ExecContext(ctx,
		`UPDATE events SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL
		 AND module_id IN (
		   SELECT m.id FROM modules m WHERE m.created_by = $2 AND m.deleted_at IS NULL
		 )`,
		id, userID,
	)
}

