package service

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"xsolla-capstone-api/internal/store"
)

// Reports: anyone signed in (student, mentor, lecturer, admin) reports a problem with a subject and a message, e.g.
// a student's behaviour. Only admins read them, mark them resolved (with a note) and delete them.

const (
	ReportOpen     = "open"
	ReportResolved = "resolved"

	maxReportSubject = 200
	maxReportMessage = 5000
	maxReportNote    = 2000
)

// ErrReportNotFound is re-exported from the store package for handler use.
var ErrReportNotFound = store.ErrReportNotFound

// ReportFieldError is one problem with a report form or filter.
type ReportFieldError struct {
	Field   string
	Message string
}

// ReportInvalidError lists every problem at once, so the form can show them all.
type ReportInvalidError struct {
	Fields []ReportFieldError
}

func (e *ReportInvalidError) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Message
	}
	return strings.Join(parts, "; ")
}

type reportStore interface {
	CreateReport(ctx context.Context, reporterID int64, subject, message string) (*store.Report, error)
	GetReport(ctx context.Context, id int64) (*store.Report, error)
	ListReports(ctx context.Context, f store.ReportFilter) ([]store.Report, error)
	CountReports(ctx context.Context) (open, resolved int, err error)
	UpdateReport(ctx context.Context, id int64, status, adminNote *string, adminID int64) (*store.Report, error)
	DeleteReport(ctx context.Context, id int64) error
}

// ReportService handles reports. Who may call what is decided by the routes (sending: anyone signed in; the rest:
// admins).
type ReportService struct {
	store reportStore
}

// NewReportService creates the report service.
func NewReportService(s reportStore) *ReportService {
	return &ReportService{store: s}
}

// requiredText trims text and checks it is 1 to max characters.
func requiredText(field, text string, max int, problems *[]ReportFieldError) string {
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		*problems = append(*problems, ReportFieldError{field, field + " is required"})
	case utf8.RuneCountInString(text) > max:
		*problems = append(*problems, ReportFieldError{field, fmt.Sprintf("%s must be at most %d characters", field, max)})
	}
	return text
}

// Send stores a new report from the signed-in user.
func (s *ReportService) Send(ctx context.Context, reporterID int64, subject, message string) (*store.Report, error) {
	var problems []ReportFieldError
	subject = requiredText("subject", subject, maxReportSubject, &problems)
	message = requiredText("message", message, maxReportMessage, &problems)
	if len(problems) > 0 {
		return nil, &ReportInvalidError{Fields: problems}
	}
	return s.store.CreateReport(ctx, reporterID, subject, message)
}

// List returns the reports matching the filter, newest first.
func (s *ReportService) List(ctx context.Context, f store.ReportFilter) ([]store.Report, error) {
	var problems []ReportFieldError
	if f.Status != "" && f.Status != ReportOpen && f.Status != ReportResolved {
		problems = append(problems, ReportFieldError{"status", "status must be open or resolved"})
	}
	switch f.Role {
	case "", "student", "mentor", "lecturer", "admin":
	default:
		problems = append(problems, ReportFieldError{"role", "role must be student, mentor, lecturer or admin"})
	}
	if f.ReporterID < 0 {
		problems = append(problems, ReportFieldError{"reporter_id", "reporter_id must be a user ID"})
	}
	f.Query = strings.TrimSpace(f.Query)
	if len(problems) > 0 {
		return nil, &ReportInvalidError{Fields: problems}
	}
	return s.store.ListReports(ctx, f)
}

// Get returns one report.
func (s *ReportService) Get(ctx context.Context, id int64) (*store.Report, error) {
	return s.store.GetReport(ctx, id)
}

// Counts returns how many reports are open and how many resolved.
func (s *ReportService) Counts(ctx context.Context) (open, resolved int, err error) {
	return s.store.CountReports(ctx)
}

// Update marks a report open or resolved and/or sets the admin's note (an empty note removes it).
func (s *ReportService) Update(ctx context.Context, adminID, id int64, status, adminNote *string) (*store.Report, error) {
	var problems []ReportFieldError
	if status == nil && adminNote == nil {
		problems = append(problems, ReportFieldError{"status", "send status, admin_note or both"})
	}
	if status != nil && *status != ReportOpen && *status != ReportResolved {
		problems = append(problems, ReportFieldError{"status", "status must be open or resolved"})
	}
	if adminNote != nil {
		note := strings.TrimSpace(*adminNote)
		if utf8.RuneCountInString(note) > maxReportNote {
			problems = append(problems, ReportFieldError{"admin_note", fmt.Sprintf("admin_note must be at most %d characters", maxReportNote)})
		}
		adminNote = &note
	}
	if len(problems) > 0 {
		return nil, &ReportInvalidError{Fields: problems}
	}
	return s.store.UpdateReport(ctx, id, status, adminNote, adminID)
}

// Delete removes a report for good.
func (s *ReportService) Delete(ctx context.Context, id int64) error {
	return s.store.DeleteReport(ctx, id)
}
