package service

import (
	"context"
	"errors"

	"xsolla-capstone-api/internal/store"
)

// SubmissionService coordinates submission operations.
type SubmissionService struct {
	store *store.Store
}

// NewSubmissionService creates a SubmissionService backed by the given store.
func NewSubmissionService(s *store.Store) *SubmissionService {
	return &SubmissionService{store: s}
}

// ErrAlreadySubmitted is returned when a student has already submitted.
var ErrAlreadySubmitted = errors.New("student has already submitted to this assignment")

// CreateSubmission creates a new submission for a student on an assignment.
// The student must be enrolled in the course, the content item must be an
// assignment that is visible, and the student must not already have a submission.
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

	// Verify the student is enrolled in the course
	enrolled, err := s.store.IsUserEnrolled(ctx, courseID, studentID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, store.ErrNotEnrolled
	}

	// Check the student hasn't already submitted (single attempt)
	existing, err := s.store.GetSubmissionByStudentAndAssignment(ctx, studentID, contentItemID)
	if err != nil {
		if !errors.Is(err, store.ErrSubmissionNotFound) {
			return nil, err
		}
	}
	if existing != nil {
		return nil, ErrAlreadySubmitted
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

	return sub, nil
}

// GetSubmissionByID returns a submission if the user is authorized to view it.
func (s *SubmissionService) GetSubmissionByID(ctx context.Context, userID int64, role string, submissionID int64) (*store.Submission, error) {
	sub, err := s.store.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		return nil, err
	}

	if role == "admin" {
		return sub, nil
	}

	// Student can only view their own submission
	if sub.StudentID == userID {
		return sub, nil
	}

	// Teacher must be enrolled in the course
	ci, err := s.store.GetContentItemByID(ctx, sub.ContentItemID)
	if err != nil {
		return nil, err
	}
	teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
	if err != nil {
		return nil, err
	}
	if !teacher {
		return nil, store.ErrNotEnrolled
	}

	return sub, nil
}

// GetSubmissionByStudentAndAssignment returns a student's submission for an assignment.
func (s *SubmissionService) GetSubmissionByStudentAndAssignment(ctx context.Context, studentID int64, contentItemID int64) (*store.Submission, error) {
	return s.store.GetSubmissionByStudentAndAssignment(ctx, studentID, contentItemID)
}

// GetSubmissionsForAssignment returns all submissions for an assignment.
// Teachers see all students; students see only their own.
func (s *SubmissionService) GetSubmissionsForAssignment(ctx context.Context, userID int64, role string, contentItemID int64) ([]store.Submission, error) {
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		if errors.Is(err, store.ErrContentItemNotFound) {
			return nil, store.ErrContentItemNotFound
		}
		return nil, err
	}

	if role == "admin" {
		return s.store.GetSubmissionsForAssignment(ctx, contentItemID)
	}

	if role == "teacher" {
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotCourseTeacher
		}
		return s.store.GetSubmissionsForAssignment(ctx, contentItemID)
	}

	// Student: return only their own submission
	sub, err := s.store.GetSubmissionByStudentAndAssignment(ctx, userID, contentItemID)
	if err != nil {
		if errors.Is(err, store.ErrSubmissionNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return []store.Submission{*sub}, nil
}

// UpdateSubmissionGrade updates a submission's grade and feedback.
// Only the teacher of the course or an admin can grade.
func (s *SubmissionService) UpdateSubmissionGrade(ctx context.Context, userID int64, role string, submissionID int64, grade *float64, feedback string) (*store.Submission, error) {
	sub, err := s.store.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		return nil, err
	}

	if role == "admin" {
		// Admin can grade any submission
	} else {
		ci, err := s.store.GetContentItemByID(ctx, sub.ContentItemID)
		if err != nil {
			return nil, err
		}
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, store.ErrNotCourseTeacher
		}
	}

	return s.store.UpdateSubmissionGrade(ctx, submissionID, grade, feedback, userID)
}

// DeleteSubmission soft-deletes a submission.
func (s *SubmissionService) DeleteSubmission(ctx context.Context, userID int64, role string, submissionID int64) error {
	sub, err := s.store.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		return err
	}

	if role == "admin" {
		return s.store.DeleteSubmission(ctx, submissionID)
	}

	// Student can delete their own submission (before grading)
	if sub.StudentID == userID {
		if sub.Grade != nil {
			return errors.New("cannot delete a graded submission")
		}
		return s.store.DeleteSubmission(ctx, submissionID)
	}

	return store.ErrNotCourseTeacher
}
