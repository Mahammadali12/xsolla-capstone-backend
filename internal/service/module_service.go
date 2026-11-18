package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

// ModuleService coordinates module and enrollment operations.
type ModuleService struct {
	store store.Store
}

// NewModuleService creates a ModuleService backed by the given store.
func NewModuleService(s store.Store) *ModuleService {
	return &ModuleService{store: s}
}

// CreateModule creates a new module with the given user as teacher.
// All active students are automatically enrolled in the new module.
func (s *ModuleService) CreateModule(ctx context.Context, userID int64, title string, description string, opensAt, closesAt time.Time) (*store.Module, error) {
	m, err := s.store.CreateModule(ctx, title, description, userID, opensAt, closesAt)
	if err != nil {
		return nil, err
	}

	// Auto-enroll the creator so they can see the module in their list.
	if _, err := s.store.CreateEnrollment(ctx, m.ID, userID); err != nil && !errors.Is(err, store.ErrAlreadyEnrolled) {
		return nil, err
	}

	// Auto-enroll all active students in the new module.
	studentIDs, err := s.store.GetActiveStudentIDs(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.store.EnrollStudentsInModule(ctx, m.ID, studentIDs); err != nil {
		return nil, err
	}

	return m, nil
}

func (s *ModuleService) GetModuleByID(ctx context.Context, userID int64, roles []string, moduleID int64) (*store.Module, error) {
	m, err := s.store.GetModuleByID(ctx, moduleID)
	if err != nil {
		return nil, err
	}

	if !(auth.HasRole(roles, "admin") || auth.HasRole(roles, "mentor") || auth.HasRole(roles, "lecturer")) {
		enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, userID)
		if err != nil {
			return nil, err
		}
		if !enrolled {
			return nil, ErrNotEnrolled
		}
	}

	one := []store.Module{*m}
	s.addLecturers(ctx, one)
	return &one[0], nil
}

// GetModulesForUser lists the user's modules, each with the lecturers who teach it.
func (s *ModuleService) GetModulesForUser(ctx context.Context, userID int64, roles []string) ([]store.Module, error) {
	modules, err := s.modulesForUser(ctx, userID, roles)
	if err != nil {
		return nil, err
	}
	s.addLecturers(ctx, modules)
	return modules, nil
}

// addLecturers fills Module.Lecturers: the enrolled users who have the lecturer role. Their names are not private, and
// a student cannot read the enrollment list of a module, so the module carries them. A failed lookup leaves the list empty.
func (s *ModuleService) addLecturers(ctx context.Context, modules []store.Module) {
	for i := range modules {
		enrollments, err := s.store.GetEnrollmentsForModule(ctx, modules[i].ID)
		if err != nil {
			continue
		}
		for _, e := range enrollments {
			if auth.HasRole(e.Roles, "lecturer") {
				modules[i].Lecturers = append(modules[i].Lecturers, store.ModuleLecturer{ID: e.UserID, Name: e.Name})
			}
		}
	}
}

func (s *ModuleService) modulesForUser(ctx context.Context, userID int64, roles []string) ([]store.Module, error) {
	if auth.HasRole(roles, "admin") || auth.HasRole(roles, "lecturer") {
		return s.store.GetModulesForUser(ctx, userID, roles)
	}
	if auth.HasRole(roles, "mentor") {
		team, err := s.store.GetMentorTeamDetail(ctx, userID)
		if err != nil {
			if errors.Is(err, store.ErrTeamNotFound) {
				return []store.Module{}, nil
			}
			return nil, err
		}
		studentIDs := make([]int64, 0, len(team.Members))
		for _, m := range team.Members {
			studentIDs = append(studentIDs, m.ID)
		}
		if len(studentIDs) == 0 {
			return []store.Module{}, nil
		}
		return s.store.GetModulesWithEnrolledStudents(ctx, studentIDs)
	}
	return s.store.GetModulesForUser(ctx, userID, roles)
}

// PatchModule updates a module if the user is authorized.
func (s *ModuleService) PatchModule(ctx context.Context, userID int64, roles []string, moduleID int64, title *string, description string, opensAt, closesAt time.Time) (*store.Module, error) {
	opensAt, closesAt, err := s.resolvePatchDates(ctx, moduleID, opensAt, closesAt)
	if err != nil {
		return nil, err
	}

	// admins edit every week; a lecturer only the weeks they created or are assigned to (checked below)
	if auth.HasRole(roles, "admin") {
		return s.store.PatchModule(ctx, moduleID, title, description, userID, true, opensAt, closesAt)
	}

	teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !teacher {
		return nil, ErrNotModuleTeacher
	}

	return s.store.PatchModule(ctx, moduleID, title, description, userID, false, opensAt, closesAt)
}

// resolvePatchDates replaces zero opens_at/closes_at with the module's current
// values so a partial PATCH never overwrites NOT NULL date columns with zero time.
func (s *ModuleService) resolvePatchDates(ctx context.Context, moduleID int64, opensAt, closesAt time.Time) (time.Time, time.Time, error) {
	if !opensAt.IsZero() && !closesAt.IsZero() {
		return opensAt, closesAt, nil
	}
	current, err := s.store.GetModuleByID(ctx, moduleID)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if opensAt.IsZero() {
		opensAt = current.OpensAt
	}
	if closesAt.IsZero() {
		closesAt = current.ClosesAt
	}
	return opensAt, closesAt, nil
}

// DeleteModule soft-deletes a module if the user is authorized.
func (s *ModuleService) DeleteModule(ctx context.Context, userID int64, roles []string, moduleID int64) error {
	// admins delete every week; a lecturer only the weeks they created or are assigned to (checked below)
	if auth.HasRole(roles, "admin") {
		return s.store.DeleteModule(ctx, moduleID, userID, true)
	}

	teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
	if err != nil {
		return err
	}
	if !teacher {
		return ErrNotModuleTeacher
	}

	return s.store.DeleteModule(ctx, moduleID, userID, false)
}

// CreateEnrollment enrolls a user in a module.
func (s *ModuleService) CreateEnrollment(ctx context.Context, actorID int64, actorRoles []string, moduleID int64, userID int64) (*store.Enrollment, error) {
	if _, err := s.store.GetModuleByID(ctx, moduleID); err != nil {
		return nil, err // store.ErrModuleNotFound for an unknown or deleted week
	}

	// Students can only enroll themselves
	if auth.HasRole(actorRoles, "student") && actorID != userID {
		return nil, ErrNotEnrolled
	}

	// Lecturer/admin can enroll anyone, but must be authorized for the module
	if auth.HasRole(actorRoles, "lecturer") || auth.HasRole(actorRoles, "admin") {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, actorID)
		if err != nil {
			return nil, err
		}
		if !teacher && auth.HasRole(actorRoles, "lecturer") {
			return nil, ErrNotModuleTeacher
		}
	}

	e, err := s.store.CreateEnrollment(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// DeleteEnrollment removes a user from a module.
func (s *ModuleService) DeleteEnrollment(ctx context.Context, actorID int64, actorRoles []string, moduleID int64, userID int64) error {
	// Students can only unenroll themselves
	if auth.HasRole(actorRoles, "student") && actorID != userID {
		return ErrNotEnrolled
	}

	// Lecturer/admin can unenroll anyone, but must be authorized for the module
	if auth.HasRole(actorRoles, "lecturer") || auth.HasRole(actorRoles, "admin") {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, actorID)
		if err != nil {
			return err
		}
		if !teacher && auth.HasRole(actorRoles, "lecturer") {
			return ErrNotModuleTeacher
		}
	}

	return s.store.DeleteEnrollment(ctx, moduleID, userID)
}

func (s *ModuleService) GetEnrollmentsForModule(ctx context.Context, userID int64, roles []string, moduleID int64) ([]store.Enrollment, error) {
	if auth.HasRole(roles, "admin") || auth.HasRole(roles, "mentor") || auth.HasRole(roles, "lecturer") {
		return s.store.GetEnrollmentsForModule(ctx, moduleID)
	}
	enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, ErrNotEnrolled
	}

	enrollments, err := s.store.GetEnrollmentsForModule(ctx, moduleID)
	if err != nil {
		return nil, err
	}
	// a student reads only their own enrollment: their week grade and their mentor's feedback are for them alone
	own := []store.Enrollment{}
	for _, e := range enrollments {
		if e.UserID == userID {
			own = append(own, e)
		}
	}
	return own, nil
}

// UpdateEnrollmentGrade sets a student's grade for a module with the mentor's module feedback (mentor/admin only).
func (s *ModuleService) UpdateEnrollmentGrade(ctx context.Context, userID int64, roles []string, moduleID int64, studentID int64, grade string, feedback string) (*store.Enrollment, error) {
	if auth.HasRole(roles, "admin") {
		// Admin can grade any enrollment
	} else if auth.HasRole(roles, "mentor") {
		// Mentor can only grade students in their own team
		team, err := s.store.GetMentorTeamDetail(ctx, userID)
		if err != nil {
			if errors.Is(err, store.ErrTeamNotFound) {
				return nil, store.ErrNotModuleTeacher
			}
			return nil, err
		}
		found := false
		for _, m := range team.Members {
			if m.ID == studentID {
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

	// Validate grade value
	switch grade {
	case "red", "yellow", "green":
		// valid
	default:
		return nil, ErrInvalidGrade
	}

	feedback = strings.TrimSpace(feedback)
	if feedback == "" {
		return nil, ErrGradeFeedbackRequired
	}
	if utf8.RuneCountInString(feedback) > maxGradeFeedback {
		return nil, ErrGradeFeedbackTooLong
	}

	gradePtr := &grade
	return s.store.UpdateEnrollmentGrade(ctx, moduleID, studentID, gradePtr, feedback, userID)
}

// maxGradeFeedback is the longest module feedback that comes with a week grade, in characters.
const maxGradeFeedback = 1000

// The module feedback is required with every week grade.
var (
	ErrGradeFeedbackRequired = errors.New("feedback is required")
	ErrGradeFeedbackTooLong  = errors.New("feedback must be at most 1000 characters")
)

// ErrNotModuleTeacher is re-exported from the store package for handler use.
var ErrNotModuleTeacher = store.ErrNotModuleTeacher

// ErrNotEnrolled is re-exported from the store package for handler use.
var ErrNotEnrolled = store.ErrNotEnrolled

// ErrInvalidGrade is returned when a grade value is not red/yellow/green.
var ErrInvalidGrade = errors.New("grade must be one of: red, yellow, green")

// ErrParentMustBeSection is re-exported from the store package for handler use.
var ErrParentMustBeSection = store.ErrParentMustBeSection

// ErrParentWrongModule is re-exported from the store package for handler use.
var ErrParentWrongModule = store.ErrParentWrongModule

// ErrAlreadyEnrolled is re-exported from the store package for handler use.
var ErrAlreadyEnrolled = store.ErrAlreadyEnrolled
