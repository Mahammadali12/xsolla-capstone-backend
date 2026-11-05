package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	// ErrSubmissionNotFound is returned when a submission does not exist or is deleted.
	ErrSubmissionNotFound = errors.New("submission not found")
)

// Submission represents a submission row from the database.
type Submission struct {
	ID           int64
	ContentItemID int64
	StudentID    int64
	TextBody     string
	SubmittedAt  time.Time
	Grade        *float64
	Feedback     *string
	GradedBy     *int64
	GradedAt     *time.Time
	CreatedAt    time.Time
	Files        []File
}

// SubmissionFile represents a row in submission_files with file metadata.
type SubmissionFile struct {
	ID           int64
	UploadedBy   int64
	OriginalName string
	StorageKey   string
	MimeType     string
	SizeBytes    int64
	CreatedAt    time.Time
}

// CreateSubmission inserts a new submission row.
func (s *Store) CreateSubmission(ctx context.Context, contentItemID int64, studentID int64, textBody string) (*Submission, error) {
	var sub Submission
	err := s.DB().CreateSubmission(ctx, contentItemID, studentID, textBody).Scan(
		&sub.ID, &sub.ContentItemID, &sub.StudentID, &sub.TextBody, &sub.SubmittedAt,
		&sub.Grade, &sub.Feedback, &sub.GradedBy, &sub.GradedAt, &sub.CreatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Load files attached to this submission
	files, err := s.GetSubmissionFiles(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	sub.Files = files

	return &sub, nil
}

// GetSubmissionByID returns a submission by ID if it exists and is not deleted.
func (s *Store) GetSubmissionByID(ctx context.Context, id int64) (*Submission, error) {
	var sub Submission
	var gradedBy *int64
	var gradedAt *time.Time
	err := s.DB().GetSubmissionByID(ctx, id).Scan(
		&sub.ID, &sub.ContentItemID, &sub.StudentID, &sub.TextBody, &sub.SubmittedAt,
		&sub.Grade, &sub.Feedback, &gradedBy, &gradedAt, &sub.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSubmissionNotFound
		}
		return nil, err
	}
	sub.GradedBy = gradedBy
	sub.GradedAt = gradedAt
	return &sub, nil
}

// GetSubmissionByStudentAndAssignment returns a student's submission for an assignment.
func (s *Store) GetSubmissionByStudentAndAssignment(ctx context.Context, studentID int64, contentItemID int64) (*Submission, error) {
	var sub Submission
	var gradedBy *int64
	var gradedAt *time.Time
	err := s.DB().GetSubmissionByStudentAndAssignment(ctx, studentID, contentItemID).Scan(
		&sub.ID, &sub.ContentItemID, &sub.StudentID, &sub.TextBody, &sub.SubmittedAt,
		&sub.Grade, &sub.Feedback, &gradedBy, &gradedAt, &sub.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSubmissionNotFound
		}
		return nil, err
	}
	sub.GradedBy = gradedBy
	sub.GradedAt = gradedAt
	return &sub, nil
}

// GetSubmissionsForAssignment returns all submissions for an assignment with student info.
func (s *Store) GetSubmissionsForAssignment(ctx context.Context, contentItemID int64) ([]Submission, error) {
	rows, err := s.DB().GetSubmissionsForAssignment(ctx, contentItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var submissions []Submission
	for rows.Next() {
		var sub Submission
		var studentID int64
		var studentName, studentEmail, studentRole string
		err := rows.Scan(
			&sub.ID, &sub.ContentItemID, &sub.StudentID, &sub.TextBody, &sub.SubmittedAt,
			&sub.Grade, &sub.Feedback, &sub.GradedBy, &sub.GradedAt, &sub.CreatedAt,
			&studentID, &studentEmail, &studentName, &studentRole,
		)
		if err != nil {
			return nil, err
		}
		sub.StudentID = studentID
		submissions = append(submissions, sub)
	}
	return submissions, nil
}

// UpdateSubmissionGrade updates a submission's grade and feedback.
func (s *Store) UpdateSubmissionGrade(ctx context.Context, id int64, grade *float64, feedback string, gradedBy int64) (*Submission, error) {
	var sub Submission
	var gradedByPtr *int64
	var gradedAt *time.Time
	err := s.DB().UpdateSubmissionGrade(ctx, id, grade, feedback, gradedBy).Scan(
		&sub.ID, &sub.ContentItemID, &sub.StudentID, &sub.TextBody, &sub.SubmittedAt,
		&sub.Grade, &sub.Feedback, &gradedByPtr, &gradedAt, &sub.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSubmissionNotFound
		}
		return nil, err
	}
	sub.GradedBy = gradedByPtr
	sub.GradedAt = gradedAt
	return &sub, nil
}

// DeleteSubmission soft-deletes a submission.
func (s *Store) DeleteSubmission(ctx context.Context, id int64) error {
	result, err := s.DB().DeleteSubmission(ctx, id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrSubmissionNotFound
	}
	return nil
}

// CreateSubmissionFile links a file to a submission.
func (s *Store) CreateSubmissionFile(ctx context.Context, submissionID int64, fileID int64) (sql.Result, error) {
	return s.DB().CreateSubmissionFile(ctx, submissionID, fileID)
}

// DeleteSubmissionFiles removes all file links from a submission.
func (s *Store) DeleteSubmissionFiles(ctx context.Context, submissionID int64) (sql.Result, error) {
	return s.DB().DeleteSubmissionFiles(ctx, submissionID)
}

// GetSubmissionFiles returns files attached to a submission.
func (s *Store) GetSubmissionFiles(ctx context.Context, submissionID int64) ([]File, error) {
	rows, err := s.DB().GetSubmissionFiles(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.UploadedBy, &f.OriginalName, &f.StorageKey, &f.MimeType, &f.SizeBytes, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}
