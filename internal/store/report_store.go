package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/lib/pq"
)

// ErrReportNotFound means there is no report with that id.
var ErrReportNotFound = errors.New("report not found")

// Report is a problem someone reported to the admins, with who sent it and how it was handled.
type Report struct {
	ID         int64
	ReporterID *int64 // nil once the reporter's account is deleted
	Reporter   *UserBrief
	Roles      []string // the reporter's roles now
	Subject    string
	Message    string
	Status     string // open or resolved
	AdminNote  *string
	ResolvedBy *UserBrief
	ResolvedAt *time.Time
	CreatedAt  time.Time
}

// ReportFilter narrows the admin's list. Zero values mean "any".
type ReportFilter struct {
	Status     string // open or resolved
	Role       string // the reporter has this role
	ReporterID int64
	Query      string // words in the subject or the message
}

const reportColumns = `
	SELECT r.id, r.reporter_id, u.name, u.email,
	       ARRAY(SELECT ur.role FROM user_roles ur WHERE ur.user_id = r.reporter_id ORDER BY ur.role),
	       r.subject, r.message, r.status, r.admin_note, r.resolved_by, a.name, a.email, r.resolved_at, r.created_at
	FROM reports r
	LEFT JOIN users u ON u.id = r.reporter_id
	LEFT JOIN users a ON a.id = r.resolved_by`

func scanReport(row interface{ Scan(...any) error }) (*Report, error) {
	var (
		r               Report
		name, email     sql.NullString
		byID            sql.NullInt64
		byName, byEmail sql.NullString
		roles           []string
	)
	if err := row.Scan(&r.ID, &r.ReporterID, &name, &email, pq.Array(&roles), &r.Subject, &r.Message, &r.Status,
		&r.AdminNote, &byID, &byName, &byEmail, &r.ResolvedAt, &r.CreatedAt); err != nil {
		return nil, err
	}
	if r.ReporterID != nil && name.Valid {
		r.Reporter = &UserBrief{ID: *r.ReporterID, Name: name.String, Email: email.String}
	}
	if byID.Valid && byName.Valid {
		r.ResolvedBy = &UserBrief{ID: byID.Int64, Name: byName.String, Email: byEmail.String}
	}
	r.Roles = roles
	if r.Roles == nil {
		r.Roles = []string{}
	}
	return &r, nil
}

// CreateReport stores a new open report.
func (s *StoreImpl) CreateReport(ctx context.Context, reporterID int64, subject, message string) (*Report, error) {
	var id int64
	if err := s.db.QueryRowContext(ctx,
		`INSERT INTO reports (reporter_id, subject, message) VALUES ($1, $2, $3) RETURNING id`,
		reporterID, subject, message,
	).Scan(&id); err != nil {
		return nil, err
	}
	return s.GetReport(ctx, id)
}

// GetReport returns one report.
func (s *StoreImpl) GetReport(ctx context.Context, id int64) (*Report, error) {
	r, err := scanReport(s.db.QueryRowContext(ctx, reportColumns+` WHERE r.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReportNotFound
	}
	return r, err
}

// likePattern makes words safe to search for with ILIKE.
func likePattern(words string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(words) + "%"
}

// ListReports returns the reports that match the filter, newest first.
func (s *StoreImpl) ListReports(ctx context.Context, f ReportFilter) ([]Report, error) {
	query := ""
	if f.Query != "" {
		query = likePattern(f.Query)
	}
	rows, err := s.db.QueryContext(ctx, reportColumns+`
		WHERE ($1 = '' OR r.status = $1)
		  AND ($2 = '' OR EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = r.reporter_id AND ur.role = $2))
		  AND ($3 = 0 OR r.reporter_id = $3)
		  AND ($4 = '' OR r.subject ILIKE $4 OR r.message ILIKE $4)
		ORDER BY r.created_at DESC, r.id DESC`,
		f.Status, f.Role, f.ReporterID, query,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Report{}
	for rows.Next() {
		r, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, *r)
	}
	return list, rows.Err()
}

// CountReports returns how many reports are open and how many resolved.
func (s *StoreImpl) CountReports(ctx context.Context) (open, resolved int, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FILTER (WHERE status = 'open'), COUNT(*) FILTER (WHERE status = 'resolved') FROM reports`,
	).Scan(&open, &resolved)
	return open, resolved, err
}

// UpdateReport changes a report's status and/or the admin's note. Resolving records who resolved it and when (kept if
// it was already resolved); reopening clears them. An empty note removes the note; nil leaves it as it is.
func (s *StoreImpl) UpdateReport(ctx context.Context, id int64, status, adminNote *string, adminID int64) (*Report, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE reports SET
		   resolved_by = CASE WHEN $2::text = 'resolved' AND status <> 'resolved' THEN $4
		                      WHEN $2::text = 'open' THEN NULL ELSE resolved_by END,
		   resolved_at = CASE WHEN $2::text = 'resolved' AND status <> 'resolved' THEN NOW()
		                      WHEN $2::text = 'open' THEN NULL ELSE resolved_at END,
		   status      = COALESCE($2::text, status),
		   admin_note  = CASE WHEN $3::text IS NULL THEN admin_note ELSE NULLIF($3::text, '') END
		 WHERE id = $1`,
		id, status, adminNote, adminID,
	)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrReportNotFound
	}
	return s.GetReport(ctx, id)
}

// DeleteReport removes a report for good.
func (s *StoreImpl) DeleteReport(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM reports WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrReportNotFound
	}
	return nil
}
