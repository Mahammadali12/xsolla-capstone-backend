package service

import (
	"context"

	"xsolla-capstone-api/internal/store"
)

// ModuleService coordinates module and enrollment operations.
type ModuleService struct {
	store *store.Store
}

// NewModuleService creates a ModuleService backed by the given store.
func NewModuleService(s *store.Store) *ModuleService {
	return &ModuleService{store: s}
}

// CreateModule creates a new module with the given user as teacher.
func (s *ModuleService) CreateModule(ctx context.Context, userID int64, title string, description string) (int64, string, string, error) {
	m, err := s.store.CreateModule(ctx, title, description, userID)
	if err != nil {
		return 0, "", "", err
	}

	// Auto-enroll the creator so they can see the module in their list.
	if _, err := s.store.CreateEnrollment(ctx, m.ID, userID); err != nil {
		return 0, "", "", err
	}

	return m.ID, m.Title, m.Description, nil
}

// GetModuleByID returns a module if the user is authorized to view it.
func (s *ModuleService) GetModuleByID(ctx context.Context, userID int64, role string, moduleID int64) (*store.Module, error) {
	m, err := s.store.GetModuleByID(ctx, moduleID)
	if err != nil {
		return nil, err
	}

	if role == "admin" {
		return m, nil
	}

	enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, ErrNotEnrolled
	}

	teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if teacher {
		return m, nil
	}

	return m, nil
}

// GetModulesForUser returns modules accessible to the user.
func (s *ModuleService) GetModulesForUser(ctx context.Context, userID int64, role string) ([]store.Module, error) {
	return s.store.GetModulesForUser(ctx, userID, role)
}

// PatchModule updates a module if the user is authorized.
func (s *ModuleService) PatchModule(ctx context.Context, userID int64, role string, moduleID int64, title string, description string) (*store.Module, error) {
	if role == "admin" {
		return s.store.PatchModule(ctx, moduleID, title, description, userID, true)
	}

	teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !teacher {
		return nil, ErrNotModuleTeacher
	}

	return s.store.PatchModule(ctx, moduleID, title, description, userID, false)
}

// DeleteModule soft-deletes a module if the user is authorized.
func (s *ModuleService) DeleteModule(ctx context.Context, userID int64, role string, moduleID int64) error {
	if role == "admin" {
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
func (s *ModuleService) CreateEnrollment(ctx context.Context, actorID int64, actorRole string, moduleID int64, userID int64) (*store.Enrollment, error) {
	// Students can only enroll themselves
	if actorRole == "student" && actorID != userID {
		return nil, ErrNotEnrolled
	}

	// Teacher/admin can enroll anyone, but must be authorized for the module
	if actorRole == "teacher" || actorRole == "admin" {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, actorID)
		if err != nil {
			return nil, err
		}
		if !teacher && actorRole == "teacher" {
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
func (s *ModuleService) DeleteEnrollment(ctx context.Context, actorID int64, actorRole string, moduleID int64, userID int64) error {
	// Students can only unenroll themselves
	if actorRole == "student" && actorID != userID {
		return ErrNotEnrolled
	}

	// Teacher/admin can unenroll anyone, but must be authorized for the module
	if actorRole == "teacher" || actorRole == "admin" {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, actorID)
		if err != nil {
			return err
		}
		if !teacher && actorRole == "teacher" {
			return ErrNotModuleTeacher
		}
	}

	return s.store.DeleteEnrollment(ctx, moduleID, userID)
}

// GetEnrollmentsForModule returns all enrollments for a module if authorized.
func (s *ModuleService) GetEnrollmentsForModule(ctx context.Context, userID int64, role string, moduleID int64) ([]store.Enrollment, error) {
	enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher && role != "admin" {
			return nil, ErrNotEnrolled
		}
	}

	return s.store.GetEnrollmentsForModule(ctx, moduleID)
}

// ErrNotModuleTeacher is re-exported from the store package for handler use.
var ErrNotModuleTeacher = store.ErrNotModuleTeacher

// ErrNotEnrolled is re-exported from the store package for handler use.
var ErrNotEnrolled = store.ErrNotEnrolled

// ErrParentMustBeSection is re-exported from the store package for handler use.
var ErrParentMustBeSection = store.ErrParentMustBeSection

// ErrParentWrongCourse is re-exported from the store package for handler use.
var ErrParentWrongModule = store.ErrParentWrongModule

// ErrAlreadyEnrolled is re-exported from the store package for handler use.
var ErrAlreadyEnrolled = store.ErrAlreadyEnrolled
