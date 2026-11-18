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
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedBy   int64     `json:"created_by"`
	Grade       *string   `json:"grade,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	OpensAt     time.Time `json:"opens_at"`
	ClosesAt    time.Time `json:"closes_at"`

	// Lecturers teach the module; filled by the service, not by the module queries.
	Lecturers []ModuleLecturer `json:"-"`
}

// ModuleLecturer is a lecturer of a module: only what a student may see.
type ModuleLecturer struct {
	ID   int64
	Name string
}

// Enrollment represents an enrollment row with user details.
type Enrollment struct {
	ID        int64
	ModuleID  int64
	UserID    int64
	CreatedAt time.Time
	Grade     *string
	GradedBy  *int64
	Email     string
	Name      string
	Role      string
	Roles     []string // the user's current roles (GetEnrollmentsForModule)

	// GradeFeedback is the mentor's module feedback that came with the grade, GradedAt when the grade was last
	// written (both nil for a week graded before they were kept).
	GradeFeedback *string
	GradedAt      *time.Time
}

// CreateModule inserts a new module and its outbox event in one transaction.
func (s *StoreImpl) CreateModule(ctx context.Context, title string, description string, createdBy int64, opensAt, closesAt time.Time) (*Module, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := WithTx(tx)

	var m Module
	err = q.CreateModule(ctx, title, description, createdBy, opensAt, closesAt).Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.Grade, &m.CreatedAt, &m.OpensAt, &m.ClosesAt)
	if err != nil {
		return nil, err
	}

	recipients, err := q.activeStudentRecipients(ctx)
	if err != nil {
		return nil, err
	}
	payload, err := moduleCreatedData(m, recipients)
	if err != nil {
		return nil, err
	}
	if err := q.InsertOutboxEvent(ctx, ModuleCreatedEventType, payload); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &m, nil
}

// GetModuleByID returns a module by ID if it exists and is not deleted.
func (s *StoreImpl) GetModuleByID(ctx context.Context, id int64) (*Module, error) {
	var m Module
	err := s.DB().GetModuleByID(ctx, id).Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.Grade, &m.CreatedAt, &m.OpensAt, &m.ClosesAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}
	return &m, nil
}

// GetModulesForUser returns modules accessible to the user based on their role.
func (s *StoreImpl) GetModulesForUser(ctx context.Context, userID int64, roles []string) ([]Module, error) {
	rows, err := s.DB().GetModulesForUser(ctx, userID, roles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modules []Module
	for rows.Next() {
		var m Module
		if err := rows.Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.Grade, &m.CreatedAt, &m.OpensAt, &m.ClosesAt); err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	return modules, nil
}

// GetModulesWithEnrolledStudents returns modules where any of the given students are enrolled.
func (s *StoreImpl) GetModulesWithEnrolledStudents(ctx context.Context, studentIDs []int64) ([]Module, error) {
	rows, err := s.DB().GetModulesWithEnrolledStudents(ctx, studentIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modules []Module
	for rows.Next() {
		var m Module
		if err := rows.Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.Grade, &m.CreatedAt, &m.OpensAt, &m.ClosesAt); err != nil {
			return nil, err
		}
		modules = append(modules, m)
	}
	return modules, nil
}

// PatchModule updates a module title/description if the user is authorized.
func (s *StoreImpl) PatchModule(ctx context.Context, id int64, title *string, description string, createdBy int64, isAdmin bool, opensAt, closesAt time.Time) (*Module, error) {
	var m Module
	err := s.DB().PatchModule(ctx, id, title, description, createdBy, isAdmin, opensAt, closesAt).Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.Grade, &m.CreatedAt, &m.OpensAt, &m.ClosesAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}
	return &m, nil
}

// UpdateModuleGrade updates a module's grade.
func (s *StoreImpl) UpdateModuleGrade(ctx context.Context, id int64, grade *string, gradedBy int64) (*Module, error) {
	var m Module
	err := s.DB().UpdateModuleGrade(ctx, id, grade, gradedBy).Scan(&m.ID, &m.Title, &m.Description, &m.CreatedBy, &m.Grade, &m.CreatedAt, &m.OpensAt, &m.ClosesAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrModuleNotFound
		}
		return nil, err
	}
	return &m, nil
}

// DeleteModule soft-deletes a module if the user is authorized.
func (s *StoreImpl) DeleteModule(ctx context.Context, id int64, createdBy int64, isAdmin bool) error {
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
func (s *StoreImpl) CreateEnrollment(ctx context.Context, moduleID int64, userID int64) (*Enrollment, error) {
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
func (s *StoreImpl) DeleteEnrollment(ctx context.Context, moduleID int64, userID int64) error {
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

// UpdateEnrollmentGrade sets a student's module grade (red/yellow/green) and the mentor's module feedback. A new grade
// replaces the old grade and the old feedback.
func (s *StoreImpl) UpdateEnrollmentGrade(ctx context.Context, moduleID int64, userID int64, grade *string, feedback string, gradedBy int64) (*Enrollment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	q := WithTx(tx)

	var e Enrollment
	err = q.UpdateEnrollmentGrade(ctx, moduleID, userID, grade, feedback, gradedBy).Scan(&e.ID, &e.ModuleID, &e.UserID, &e.Grade, &e.GradeFeedback, &e.GradedAt, &e.GradedBy, &e.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotEnrolled
		}
		return nil, err
	}

	var moduleTitle string
	if err := q.QueryRowContext(ctx,
		`SELECT title FROM modules WHERE id = $1 AND deleted_at IS NULL`, moduleID,
	).Scan(&moduleTitle); err != nil {
		return nil, err
	}
	studentName, studentEmail, err := q.userNameEmail(ctx, userID)
	if err != nil {
		return nil, err
	}
	graderName, _, err := q.userNameEmail(ctx, gradedBy)
	if err != nil {
		return nil, err
	}
	gradeValue := ""
	if grade != nil {
		gradeValue = *grade
	}
	payload, err := enrollmentGradedData(moduleID, moduleTitle, userID, studentName, gradeValue, gradedBy, graderName,
		[]recipientSnapshot{{ID: userID, Role: "student", Name: studentName, Email: studentEmail}})
	if err != nil {
		return nil, err
	}
	if err := q.InsertOutboxEvent(ctx, EnrollmentGradedEventType, payload); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &e, nil
}

// GetEnrollmentsForModule returns all enrollments for a module with user details.
func (s *StoreImpl) GetEnrollmentsForModule(ctx context.Context, moduleID int64) ([]Enrollment, error) {
	rows, err := s.DB().GetEnrollmentsForModule(ctx, moduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var enrollments []Enrollment
	for rows.Next() {
		var e Enrollment
		if err := rows.Scan(&e.ID, &e.ModuleID, &e.UserID, &e.CreatedAt, &e.Grade, &e.GradeFeedback, &e.GradedAt, &e.GradedBy, &e.Email, &e.Name, pq.Array(&e.Roles)); err != nil {
			return nil, err
		}
		enrollments = append(enrollments, e)
	}
	return enrollments, nil
}

// IsUserEnrolled checks if a user is enrolled in a module.
func (s *StoreImpl) IsUserEnrolled(ctx context.Context, moduleID int64, userID int64) (bool, error) {
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
func (s *StoreImpl) IsUserTeacherOfModule(ctx context.Context, moduleID int64, userID int64) (bool, error) {
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

// GetAllModuleIDs returns all non-deleted module IDs.
func (s *StoreImpl) GetAllModuleIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.DB().GetAllModuleIDs(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetActiveStudentIDs returns all active student IDs.
func (s *StoreImpl) GetActiveStudentIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.DB().GetActiveStudentIDs(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// EnrollStudentsInModule bulk-enrolls students into a module.
func (s *StoreImpl) EnrollStudentsInModule(ctx context.Context, moduleID int64, studentIDs []int64) error {
	if len(studentIDs) == 0 {
		return nil
	}
	_, err := s.DB().EnrollStudentsInModule(ctx, moduleID, studentIDs)
	return err
}
