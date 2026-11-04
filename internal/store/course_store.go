package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

var (
	// ErrCourseNotFound is returned when a course does not exist or is deleted.
	ErrCourseNotFound = errors.New("course not found")
	// ErrNotCourseTeacher is returned when a user is not the teacher of a course.
	ErrNotCourseTeacher = errors.New("not the teacher of this course")
	// ErrNotEnrolled is returned when a user is not enrolled in a course.
	ErrNotEnrolled = errors.New("not enrolled in this course")
	// ErrAlreadyEnrolled is returned when a user is already enrolled in a course.
	ErrAlreadyEnrolled = errors.New("already enrolled in this course")
)

// Course represents a course row from the database.
type Course struct {
	ID          int64
	Title       string
	Description string
	CreatedBy   int64
	CreatedAt   time.Time
}

// Enrollment represents an enrollment row with user details.
type Enrollment struct {
	ID        int64
	CourseID  int64
	UserID    int64
	CreatedAt time.Time
	Email     string
	Name      string
	Role      string
}

// CreateCourse inserts a new course.
func (s *Store) CreateCourse(ctx context.Context, title string, description string, createdBy int64) (*Course, error) {
	var c Course
	err := s.DB().CreateCourse(ctx, title, description, createdBy).Scan(&c.ID, &c.Title, &c.Description, &c.CreatedBy, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetCourseByID returns a course by ID if it exists and is not deleted.
func (s *Store) GetCourseByID(ctx context.Context, id int64) (*Course, error) {
	var c Course
	err := s.DB().GetCourseByID(ctx, id).Scan(&c.ID, &c.Title, &c.Description, &c.CreatedBy, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCourseNotFound
		}
		return nil, err
	}
	return &c, nil
}

// GetCoursesForUser returns courses accessible to the user based on their role.
func (s *Store) GetCoursesForUser(ctx context.Context, userID int64, role string) ([]Course, error) {
	rows, err := s.DB().GetCoursesForUser(ctx, userID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var courses []Course
	for rows.Next() {
		var c Course
		if err := rows.Scan(&c.ID, &c.Title, &c.Description, &c.CreatedBy, &c.CreatedAt); err != nil {
			return nil, err
		}
		courses = append(courses, c)
	}
	return courses, nil
}

// PatchCourse updates a course title/description if the user is authorized.
func (s *Store) PatchCourse(ctx context.Context, id int64, title string, description string, createdBy int64, isAdmin bool) (*Course, error) {
	var c Course
	err := s.DB().PatchCourse(ctx, id, title, description, createdBy, isAdmin).Scan(&c.ID, &c.Title, &c.Description, &c.CreatedBy, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrCourseNotFound
		}
		return nil, err
	}
	return &c, nil
}

// DeleteCourse soft-deletes a course if the user is authorized.
func (s *Store) DeleteCourse(ctx context.Context, id int64, createdBy int64, isAdmin bool) error {
	result, err := s.DB().DeleteCourse(ctx, id, createdBy, isAdmin)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrCourseNotFound
	}
	return nil
}

// CreateEnrollment creates an enrollment record.
func (s *Store) CreateEnrollment(ctx context.Context, courseID int64, userID int64) (*Enrollment, error) {
	var e Enrollment
	err := s.DB().CreateEnrollment(ctx, courseID, userID).Scan(&e.ID, &e.CourseID, &e.UserID, &e.CreatedAt)
	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrAlreadyEnrolled
		}
		return nil, err
	}
	return &e, nil
}

// DeleteEnrollment removes an enrollment record.
func (s *Store) DeleteEnrollment(ctx context.Context, courseID int64, userID int64) error {
	result, err := s.DB().DeleteEnrollment(ctx, courseID, userID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotEnrolled
	}
	return nil
}

// GetEnrollmentsForCourse returns all enrollments for a course with user details.
func (s *Store) GetEnrollmentsForCourse(ctx context.Context, courseID int64) ([]Enrollment, error) {
	rows, err := s.DB().GetEnrollmentsForCourse(ctx, courseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var enrollments []Enrollment
	for rows.Next() {
		var e Enrollment
		if err := rows.Scan(&e.ID, &e.CourseID, &e.UserID, &e.CreatedAt, &e.Email, &e.Name, &e.Role); err != nil {
			return nil, err
		}
		enrollments = append(enrollments, e)
	}
	return enrollments, nil
}

// IsUserEnrolled checks if a user is enrolled in a course.
func (s *Store) IsUserEnrolled(ctx context.Context, courseID int64, userID int64) (bool, error) {
	var exists int
	err := s.DB().IsUserEnrolled(ctx, courseID, userID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsUserTeacherOfCourse checks if a user created (teaches) a course.
func (s *Store) IsUserTeacherOfCourse(ctx context.Context, courseID int64, userID int64) (bool, error) {
	var exists int
	err := s.DB().IsUserTeacherOfCourse(ctx, courseID, userID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
