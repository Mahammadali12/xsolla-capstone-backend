package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

var (
	// ErrModuleNotFound is returned when a module does not exist or is deleted.
	ErrModuleNotFound = errors.New("module not found")
	// ErrNotModuleTeacher is returned when a user is not the teacher of a module.
	ErrNotModuleTeacher = errors.New("not the teacher of this module")
	// ErrNotEnrolled is returned when a user is not enrolled in a module.
	ErrNotEnrolled = errors.New("not enrolled in this module")
	// ErrAlreadyEnrolled is returned when a user is already enrolled in a module.
	ErrAlreadyEnrolled = errors.New("already enrolled in this module")
)

// Module represents a module row from the database.
type Module struct {
	ID          int64
	Title       string
	Description string
	CreatedBy   int64
	CreatedAt   time.Time
}

// Enrollment represents an enrollment row with user details.
type Enrollment struct {
	ID        int64
	ModuleID  int64
	UserID    int64
	CreatedAt time.Time
	Email     string
	Name      string
	Role      string
}

// CreateModule inserts a new module.
func (s *Store) CreateModule(ctx context.Context, title string, description string, createdBy int64) (*Module, error) {
	var m Module
	err := s.DB().CreateModule(ctx, title, description, createdBy).Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetModuleByID returns a module by ID if it exists and is not deleted.
func (s *Store) GetModuleByID(ctx context.Context, id int64) (*Module, error) {
	var m Module
	err := s.DB().GetModuleByID(ctx, id).Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}
	return &m, nil
}

// GetModulesForUser returns modules accessible to the user based on their role.
func (s *Store) GetModulesForUser(ctx context.Context, userID int64, role string) ([]Module, error) {
	rows, err := s.DB().GetModulesForUser(ctx, userID, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modules []Module
	for rows.Next() {
		var m Module
		if err := rows.Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.CreatedAt); err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	return modules, nil
}

// PatchModule updates a module title/description if the user is authorized.
func (s *Store) PatchModule(ctx context.Context, id int64, title string, description string, createdBy int64, isAdmin bool) (*Module, error) {
	var m Module
	err := s.DB().PatchModule(ctx, id, title, description, createdBy, isAdmin).Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}
	return &m, nil
}

// DeleteModule soft-deletes a module if the user is authorized.
func (s *Store) DeleteModule(ctx context.Context, id int64, createdBy int64, isAdmin bool) error {
	result, err := s.DB().DeleteModule(ctx, id, createdBy, isAdmin)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrModuleNotFound
	}
	return nil
}

// CreateEnrollment creates an enrollment record.
func (s *Store) CreateEnrollment(ctx context.Context, moduleID int64, userID int64) (*Enrollment, error) {
	var e Enrollment
	err := s.DB().CreateEnrollment(ctx, moduleID, userID).Scan(&e.ID, &e.ModuleID, &e.UserID, &e.CreatedAt)
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
func (s *Store) DeleteEnrollment(ctx context.Context, moduleID int64, userID int64) error {
	result, err := s.DB().DeleteEnrollment(ctx, moduleID, userID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotEnrolled
	}
	return nil
}

// GetEnrollmentsForModule returns all enrollments for a module with user details.
func (s *Store) GetEnrollmentsForModule(ctx context.Context, moduleID int64) ([]Enrollment, error) {
	rows, err := s.DB().GetEnrollmentsForModule(ctx, moduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var enrollments []Enrollment
	for rows.Next() {
		var e Enrollment
		if err := rows.Scan(&e.ID, &e.ModuleID, &e.UserID, &e.CreatedAt, &e.Email, &e.Name, &e.Role); err != nil {
			return nil, err
		}
		enrollments = append(enrollments, e)
	}
	return enrollments, nil
}

// IsUserEnrolled checks if a user is enrolled in a module.
func (s *Store) IsUserEnrolled(ctx context.Context, moduleID int64, userID int64) (bool, error) {
	var exists int
	err := s.DB().IsUserEnrolled(ctx, moduleID, userID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsUserTeacherOfModule checks if a user created (teaches) a module.
func (s *Store) IsUserTeacherOfModule(ctx context.Context, moduleID int64, userID int64) (bool, error) {
	var exists int
	err := s.DB().IsUserTeacherOfModule(ctx, moduleID, userID).Scan(&exists)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
