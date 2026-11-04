package service

import (
	"context"

	"xsolla-capstone-api/internal/store"
)

// CourseService coordinates course and enrollment operations.
type CourseService struct {
	store *store.Store
}

// NewCourseService creates a CourseService backed by the given store.
func NewCourseService(s *store.Store) *CourseService {
	return &CourseService{store: s}
}

// CreateCourse creates a new course with the given user as teacher.
func (s *CourseService) CreateCourse(ctx context.Context, userID int64, title string, description string) (int64, string, string, error) {
	c, err := s.store.CreateCourse(ctx, title, description, userID)
	if err != nil {
		return 0, "", "", err
	}

	// Auto-enroll the creator so they can see the course in their list.
	if _, err := s.store.CreateEnrollment(ctx, c.ID, userID); err != nil {
		return 0, "", "", err
	}

	return c.ID, c.Title, c.Description, nil
}

// GetCourseByID returns a course if the user is authorized to view it.
func (s *CourseService) GetCourseByID(ctx context.Context, userID int64, role string, courseID int64) (*store.Course, error) {
	c, err := s.store.GetCourseByID(ctx, courseID)
	if err != nil {
		return nil, err
	}

	if role == "admin" {
		return c, nil
	}

	enrolled, err := s.store.IsUserEnrolled(ctx, courseID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, ErrNotEnrolled
	}

	teacher, err := s.store.IsUserTeacherOfCourse(ctx, courseID, userID)
	if err != nil {
		return nil, err
	}
	if teacher {
		return c, nil
	}

	return c, nil
}

// GetCoursesForUser returns courses accessible to the user.
func (s *CourseService) GetCoursesForUser(ctx context.Context, userID int64, role string) ([]store.Course, error) {
	return s.store.GetCoursesForUser(ctx, userID, role)
}

// PatchCourse updates a course if the user is authorized.
func (s *CourseService) PatchCourse(ctx context.Context, userID int64, role string, courseID int64, title string, description string) (*store.Course, error) {
	if role == "admin" {
		return s.store.PatchCourse(ctx, courseID, title, description, userID, true)
	}

	teacher, err := s.store.IsUserTeacherOfCourse(ctx, courseID, userID)
	if err != nil {
		return nil, err
	}
	if !teacher {
		return nil, ErrNotCourseTeacher
	}

	return s.store.PatchCourse(ctx, courseID, title, description, userID, false)
}

// DeleteCourse soft-deletes a course if the user is authorized.
func (s *CourseService) DeleteCourse(ctx context.Context, userID int64, role string, courseID int64) error {
	if role == "admin" {
		return s.store.DeleteCourse(ctx, courseID, userID, true)
	}

	teacher, err := s.store.IsUserTeacherOfCourse(ctx, courseID, userID)
	if err != nil {
		return err
	}
	if !teacher {
		return ErrNotCourseTeacher
	}

	return s.store.DeleteCourse(ctx, courseID, userID, false)
}

// CreateEnrollment enrolls a user in a course.
func (s *CourseService) CreateEnrollment(ctx context.Context, actorID int64, actorRole string, courseID int64, userID int64) (*store.Enrollment, error) {
	// Students can only enroll themselves
	if actorRole == "student" && actorID != userID {
		return nil, ErrNotEnrolled
	}

	// Teacher/admin can enroll anyone, but must be authorized for the course
	if actorRole == "teacher" || actorRole == "admin" {
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, courseID, actorID)
		if err != nil {
			return nil, err
		}
		if !teacher && actorRole == "teacher" {
			return nil, ErrNotCourseTeacher
		}
	}

	e, err := s.store.CreateEnrollment(ctx, courseID, userID)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// DeleteEnrollment removes a user from a course.
func (s *CourseService) DeleteEnrollment(ctx context.Context, actorID int64, actorRole string, courseID int64, userID int64) error {
	// Students can only unenroll themselves
	if actorRole == "student" && actorID != userID {
		return ErrNotEnrolled
	}

	// Teacher/admin can unenroll anyone, but must be authorized for the course
	if actorRole == "teacher" || actorRole == "admin" {
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, courseID, actorID)
		if err != nil {
			return err
		}
		if !teacher && actorRole == "teacher" {
			return ErrNotCourseTeacher
		}
	}

	return s.store.DeleteEnrollment(ctx, courseID, userID)
}

// GetEnrollmentsForCourse returns all enrollments for a course if authorized.
func (s *CourseService) GetEnrollmentsForCourse(ctx context.Context, userID int64, role string, courseID int64) ([]store.Enrollment, error) {
	enrolled, err := s.store.IsUserEnrolled(ctx, courseID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, courseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher && role != "admin" {
			return nil, ErrNotEnrolled
		}
	}

	return s.store.GetEnrollmentsForCourse(ctx, courseID)
}

// ErrCourseNotFound is re-exported from the store package for handler use.
var ErrCourseNotFound = store.ErrCourseNotFound

// ErrNotCourseTeacher is re-exported from the store package for handler use.
var ErrNotCourseTeacher = store.ErrNotCourseTeacher

// ErrNotEnrolled is re-exported from the store package for handler use.
var ErrNotEnrolled = store.ErrNotEnrolled

// ErrAlreadyEnrolled is re-exported from the store package for handler use.
var ErrAlreadyEnrolled = store.ErrAlreadyEnrolled
