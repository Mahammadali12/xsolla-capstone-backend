package service

import (
	"context"
	"errors"
	"fmt"

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
// ErrInvalidSubmission is returned for a hand-in that breaks a rule; the message says which.
var ErrInvalidSubmission = errors.New("invalid submission")

func invalidSubmission(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidSubmission, msg) }

func (s *SubmissionService) CreateSubmission(ctx context.Context, studentID int64, courseID int64, contentItemID int64, textBody string, fileIDs []int64) (*store.Submission, error) {
	// Verify the content item exists and is an assignment
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		if errors.Is(err, store.ErrContentItemNotFound) {
			return nil, store.ErrContentItemNotFound
		}
		return nil, err
	}
	if ci.ModuleID != courseID {
		return nil, store.ErrContentItemNotFound // not an item of this week
	}
	if ci.Type != "assignment" {
		return nil, invalidSubmission("content item is not an assignment")
	}
	if !ci.IsVisible {
		return nil, invalidSubmission("assignment is not visible")
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
				return nil, invalidSubmission("file not found")
			}
			return nil, err
		}
		if f.UploadedBy != studentID {
			return nil, invalidSubmission("file does not belong to student")
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
// Access is cumulative: admin, the student themselves, a mentor of the student's
// team, or the teacher of the submission's module (regardless of the other roles).
func (s *SubmissionService) GetSubmissionByID(ctx context.Context, userID int64, roles []string, submissionID int64) (*store.Submission, error) {
	sub, err := s.store.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		return nil, err
	}

	allowed := auth.HasRole(roles, "admin") || sub.StudentID == userID

	if !allowed && auth.HasRole(roles, "mentor") {
		team, err := s.store.GetMentorTeamDetail(ctx, userID)
		if err != nil {
			if !errors.Is(err, store.ErrTeamNotFound) {
				return nil, err
			}
		} else {
			for _, m := range team.Members {
				if m.ID == sub.StudentID {
					allowed = true
					break
				}
			}
		}
	}

	if !allowed && auth.HasRole(roles, "lecturer") {
		ci, err := s.store.GetContentItemByID(ctx, sub.ContentItemID)
		if err != nil {
			return nil, err
		}
		teacher, err := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		allowed = teacher
	}

	if !allowed {
		return nil, store.ErrNotEnrolled
	}

	// Load attached files
	sub.Files, err = s.store.GetSubmissionFiles(ctx, sub.ID)
	if err != nil {
		return nil, err
	}

	return sub, nil
}

// GetSubmissionByStudentAndAssignment returns a student's submission for an assignment.
// The newest hand-in is returned with its files; History holds the earlier ones, newest first, each with its files.
func (s *SubmissionService) GetSubmissionByStudentAndAssignment(ctx context.Context, studentID int64, contentItemID int64) (*store.Submission, error) {
	sub, err := s.store.GetSubmissionByStudentAndAssignment(ctx, studentID, contentItemID)
	if err != nil {
		return nil, err
	}
	sub.Files, err = s.store.GetSubmissionFiles(ctx, sub.ID)
	if err != nil {
		return nil, err
	}
	all, err := s.store.GetSubmissionsByStudentAndAssignment(ctx, studentID, contentItemID)
	if err != nil {
		return nil, err
	}
	for _, old := range all {
		if old.ID == sub.ID {
			continue
		}
		if old.Files, err = s.store.GetSubmissionFiles(ctx, old.ID); err != nil {
			return nil, err
		}
		sub.History = append(sub.History, old)
	}
	return sub, nil
}

// GetSubmissionsForAssignment returns the submissions for an assignment.
// Access is cumulative: admins see all, the module's teacher sees all, a mentor
// sees their own team's students (even when they also hold the lecturer role),
// and a student sees only their own attempts.
func (s *SubmissionService) GetSubmissionsForAssignment(ctx context.Context, userID int64, roles []string, contentItemID int64) ([]store.Submission, error) {
	ci, err := s.store.GetContentItemByID(ctx, contentItemID)
	if err != nil {
		if errors.Is(err, store.ErrContentItemNotFound) {
			return nil, store.ErrContentItemNotFound
		}
		return nil, err
	}

	var submissions []store.Submission
	isAdmin := auth.HasRole(roles, "admin")
	isLecturer := auth.HasRole(roles, "lecturer")
	isMentor := auth.HasRole(roles, "mentor")

	switch {
	case isAdmin:
		submissions, err = s.store.GetSubmissionsForAssignment(ctx, contentItemID)

	case isLecturer:
		teacher, teacherErr := s.store.IsUserTeacherOfModule(ctx, ci.ModuleID, userID)
		if teacherErr != nil {
			return nil, teacherErr
		}
		if teacher {
			// The teacher sees every student's work, even if they are also a mentor.
			submissions, err = s.store.GetSubmissionsForAssignment(ctx, contentItemID)
		} else if isMentor {
			// A lecturer with an extra mentor role keeps the mentor's team access.
			submissions, err = s.submissionsForMentor(ctx, userID, contentItemID)
		} else {
			return nil, store.ErrNotModuleTeacher
		}

	case isMentor:
		submissions, err = s.submissionsForMentor(ctx, userID, contentItemID)

	default:
		// Student: all of their own attempts, most recent first
		submissions, err = s.store.GetSubmissionsByStudentAndAssignment(ctx, userID, contentItemID)
	}
	if err != nil {
		return nil, err
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

// submissionsForMentor returns the submissions of the mentor's own team.
// A mentor without a team, or with an empty team, gets an empty list.
func (s *SubmissionService) submissionsForMentor(ctx context.Context, mentorID, contentItemID int64) ([]store.Submission, error) {
	team, err := s.store.GetMentorTeamDetail(ctx, mentorID)
	if err != nil {
		if errors.Is(err, store.ErrTeamNotFound) {
			return []store.Submission{}, nil
		}
		return nil, err
	}

	studentIDs := make([]int64, 0, len(team.Members))
	for _, m := range team.Members {
		studentIDs = append(studentIDs, m.ID)
	}
	if len(studentIDs) == 0 {
		return []store.Submission{}, nil
	}

	return s.store.GetSubmissionsForAssignmentByStudents(ctx, contentItemID, studentIDs)
}

// UpdateSubmissionFeedback updates a submission's feedback.
// Only the mentor of the student's team or an admin can grade.
func (s *SubmissionService) UpdateSubmissionFeedback(ctx context.Context, userID int64, roles []string, submissionID int64, feedback string) (*store.Submission, error) {
	sub, err := s.store.GetSubmissionByID(ctx, submissionID)
	if err != nil {
		return nil, err
	}

	if auth.HasRole(roles, "admin") {
		// Admin can grade any submission
	} else if auth.HasRole(roles, "mentor") {
		// Mentor can only grade submissions from students in their own team
		team, err := s.store.GetMentorTeamDetail(ctx, userID)
		if err != nil {
			if errors.Is(err, store.ErrTeamNotFound) {
				return nil, store.ErrNotModuleTeacher
			}
			return nil, err
		}
		found := false
		for _, m := range team.Members {
			if m.ID == sub.StudentID {
				found = true
				break
			}
		}
		if !found {
			return nil, store.ErrNotModuleTeacher
		}
	} else {
		return nil, store.ErrNotModuleTeacher
	}

	updated, err := s.store.UpdateSubmissionFeedback(ctx, submissionID, feedback, userID)
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

// UpdateModuleGrade updates a module's grade (red/yellow/green).
// Only the mentor of a student in the module or an admin can grade.
func (s *SubmissionService) UpdateModuleGrade(ctx context.Context, userID int64, roles []string, moduleID int64, grade string) (*store.Module, error) {
	if auth.HasRole(roles, "admin") {
		// Admin can grade any module
	} else if auth.HasRole(roles, "mentor") {
		// Mentor can grade only if they mentor at least one student enrolled in the module
		team, err := s.store.GetMentorTeamDetail(ctx, userID)
		if err != nil {
			if errors.Is(err, store.ErrTeamNotFound) {
				return nil, store.ErrNotModuleTeacher
			}
			return nil, err
		}
		canGrade := false
		for _, m := range team.Members {
			enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, m.ID)
			if err != nil {
				return nil, err
			}
			if enrolled {
				canGrade = true
				break
			}
		}
		if !canGrade {
			return nil, store.ErrNotModuleTeacher
		}
	} else {
		return nil, store.ErrNotModuleTeacher
	}

	// Validate grade value
	switch grade {
	case "red", "yellow", "green":
		// valid
	default:
		return nil, errors.New("grade must be one of: red, yellow, green")
	}

	updated, err := s.store.UpdateModuleGrade(ctx, moduleID, &grade, userID)
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

	// Student can delete their own submission (before feedback)
	if sub.StudentID == userID {
		if sub.Feedback != nil && *sub.Feedback != "" {
			return errors.New("cannot delete a graded submission")
		}
		return s.store.DeleteSubmission(ctx, submissionID)
	}

	return store.ErrNotModuleTeacher
}
