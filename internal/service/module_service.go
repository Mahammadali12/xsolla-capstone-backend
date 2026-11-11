package service

import (
	"context"
	"errors"
	"time"

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

// GetModuleByID returns a module if the user is authorized to view it.
func (s *ModuleService) GetModuleByID(ctx context.Context, userID int64, roles []string, moduleID int64) (*store.Module, error) {
	m, err := s.store.GetModuleByID(ctx, moduleID)
	if err != nil {
		return nil, err
	}

	if auth.HasRole(roles, "admin") {
		return m, nil
	}

	enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, ErrNotEnrolled
	}

	// Enrolled users (including teachers who are auto-enrolled) can view the module
	return m, nil
}

// GetModulesForUser returns modules accessible to the user.
func (s *ModuleService) GetModulesForUser(ctx context.Context, userID int64, roles []string) ([]store.Module, error) {
	return s.store.GetModulesForUser(ctx, userID, roles)
}

// PatchModule updates a module if the user is authorized.
func (s *ModuleService) PatchModule(ctx context.Context, userID int64, roles []string, moduleID int64, title string, description string, opensAt, closesAt time.Time) (*store.Module, error) {
	opensAt, closesAt, err := s.resolvePatchDates(ctx, moduleID, opensAt, closesAt)
	if err != nil {
		return nil, err
	}

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

// GetEnrollmentsForModule returns all enrollments for a module if authorized.
func (s *ModuleService) GetEnrollmentsForModule(ctx context.Context, userID int64, roles []string, moduleID int64) ([]store.Enrollment, error) {
	enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher && !auth.HasRole(roles, "admin") {
			return nil, ErrNotEnrolled
		}
	}

	return s.store.GetEnrollmentsForModule(ctx, moduleID)
}

// UpdateEnrollmentGrade sets a student's grade for a module (mentor/admin only).
func (s *ModuleService) UpdateEnrollmentGrade(ctx context.Context, userID int64, roles []string, moduleID int64, studentID int64, grade string) (*store.Enrollment, error) {
	if auth.HasRole(roles, "admin") {
		// Admin can grade any enrollment
	} else if auth.HasRole(roles, "mentor") {
		// Mentor can grade
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

	gradePtr := &grade
	return s.store.UpdateEnrollmentGrade(ctx, moduleID, studentID, gradePtr)
}

// ErrNotModuleTeacher is re-exported from the store package for handler use.
var ErrNotModuleTeacher = store.ErrNotModuleTeacher

// ErrNotEnrolled is re-exported from the store package for handler use.
var ErrNotEnrolled = store.ErrNotEnrolled

// ErrParentMustBeSection is re-exported from the store package for handler use.
var ErrParentMustBeSection = store.ErrParentMustBeSection

// ErrParentWrongModule is re-exported from the store package for handler use.
var ErrParentWrongModule = store.ErrParentWrongModule

// ErrAlreadyEnrolled is re-exported from the store package for handler use.
var ErrAlreadyEnrolled = store.ErrAlreadyEnrolled
