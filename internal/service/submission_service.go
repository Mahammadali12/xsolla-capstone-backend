package service

import (
	"context"
	"errors"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// SubmissionService coordinates submission operations.
type SubmissionService struct {
	store store.Store
}

// NewSubmissionService creates a SubmissionService backed by the given store.
func NewSubmissionService(s store.Store) *SubmissionService {
	return &SubmissionService{store: s}
}

// CreateSubmission creates a new submission for a student on an assignment.
// Multiple attempts are allowed: each POST inserts a new submission row.
func (s *SubmissionService) CreateSubmission(ctx context.Context, studentID int64, courseID int64, contentItemID int64, textBody string, fileIDs []int64) (*store.Submission, error) {
	// Verify the content item exists and is an assignment
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		if errors.Is(err, store.ErrContentItemNotFound) {
			return nil, store.ErrContentItemNotFound
		}
		return nil, err
	}
	if ci.Type != "assignment" {
		return nil, errors.New("content item is not an assignment")
	}
	if !ci.IsVisible {
		return nil, errors.New("assignment is not visible")
	}

	// Verify the student is enrolled in the module
	enrolled, err := s.store.IsUserEnrolled(ctx, courseID, studentID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, store.ErrNotEnrolled
	}

	// Verify all file IDs belong to the student
	for _, fileID := range fileIDs {
		f, err := s.store.GetFileByID(ctx, fileID)
		if err != nil {
			if errors.Is(err, store.ErrFileNotFound) {
				return nil, errors.New("file not found")
			}
			return nil, err
		}
		if f.UploadedBy != studentID {
			return nil, errors.New("file does not belong to student")
		}
	}

	// Create the submission
	sub, err := s.store.CreateSubmission(ctx, contentItemID, studentID, textBody)
	if err != nil {
		return nil, err
	}

	// Attach files
	for _, fileID := range fileIDs {
		if _, err := s.store.CreateSubmissionFile(ctx, sub.ID, fileID); err != nil {
			return nil, err
		}
	}

	// Reload files so the create response includes them
	sub.Files, err = s.store.GetSubmissionFiles(ctx, sub.ID)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

// GetSubmissionByID returns a submission if the user is authorized to view it.
func (s *SubmissionService) GetSubmissionByID(ctx context.Context, userID int64, roles []string, submissionID int64) (*store.Submission, error) {
	sub, err := s.store.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		return nil, err
	}

	if auth.HasRole(roles, "admin") {
		// Admin can view any submission
	} else if sub.StudentID == userID {
		// Student can only view their own submission
	} else {
		// Lecturer/mentor must be the teacher of the module
		ci, err := s.store.GetContentItemByID(ctx, sub.ContentItemID)
		if err != nil {
			return nil, err
		}
		teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotEnrolled
		}
	}

	// Load attached files
	sub.Files, err = s.store.GetSubmissionFiles(ctx, sub.ID)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

// GetSubmissionByStudentAndAssignment returns a student's submission for an assignment.
func (s *SubmissionService) GetSubmissionByStudentAndAssignment(ctx context.Context, studentID int64, contentItemID int64) (*store.Submission, error) {
	sub, err := s.store.GetSubmissionByStudentAndAssignment(ctx, studentID, contentItemID)
	if err != nil {
		return nil, err
	}
	sub.Files, err = s.store.GetSubmissionFiles(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// GetSubmissionsForAssignment returns all submissions for an assignment.
// Teachers see all students; students see only their own.
func (s *SubmissionService) GetSubmissionsForAssignment(ctx context.Context, userID int64, roles []string, contentItemID int64) ([]store.Submission, error) {
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		if errors.Is(err, store.ErrContentItemNotFound) {
			return nil, store.ErrContentItemNotFound
		}
		return nil, err
	}

	var submissions []store.Submission

	if auth.HasRole(roles, "admin") {
		submissions, err = s.store.GetSubmissionsForAssignment(ctx, contentItemID)
		if err != nil {
			return nil, err
		}
	} else if auth.HasRole(roles, "lecturer") {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotModuleTeacher
		}
		submissions, err = s.store.GetSubmissionsForAssignment(ctx, contentItemID)
		if err != nil {
			return nil, err
		}
	} else {
		// Student: return all of their own attempts, most recent first
		submissions, err = s.store.GetSubmissionsByStudentAndAssignment(ctx, userID, contentItemID)
		if err != nil {
			return nil, err
		}
	}

	// Load attached files for each submission
	for i := range submissions {
		submissions[i].Files, err = s.store.GetSubmissionFiles(ctx, submissions[i].ID)
		if err != nil {
			return nil, err
		}
	}

	return submissions, nil
}

// UpdateSubmissionGrade updates a submission's grade and feedback.
// Only the mentor of the module or an admin can grade. Lecturers cannot grade.
func (s *SubmissionService) UpdateSubmissionGrade(ctx context.Context, userID int64, roles []string, submissionID int64, grade *float64, feedback string) (*store.Submission, error) {
	if _, err := s.store.GetSubmissionByID(ctx, submissionID); err != nil {
		return nil, err
	}

	if auth.HasRole(roles, "admin") {
		// Admin can grade any submission
	} else if auth.HasRole(roles, "mentor") {
		// Mentor can grade
	} else {
		return nil, store.ErrNotModuleTeacher
	}

	updated, err := s.store.UpdateSubmissionGrade(ctx, submissionID, grade, feedback, userID)
	if err != nil {
		return nil, err
	}

	// Reload attached files
	updated.Files, err = s.store.GetSubmissionFiles(ctx, updated.ID)
	if err != nil {
		return nil, err
	}

	return updated, nil
}

// DeleteSubmission soft-deletes a submission.
func (s *SubmissionService) DeleteSubmission(ctx context.Context, userID int64, roles []string, submissionID int64) error {
	sub, err := s.store.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		return err
	}

	if auth.HasRole(roles, "admin") {
		return s.store.DeleteSubmission(ctx, submissionID)
	}

	// Student can delete their own submission (before grading)
	if sub.StudentID == userID {
		if sub.Grade != nil {
			return errors.New("cannot delete a graded submission")
		}
		return s.store.DeleteSubmission(ctx, submissionID)
	}

	return store.ErrNotModuleTeacher
}
