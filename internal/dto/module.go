package dto

import "time"

// CreateModuleRequest is the payload for POST /modules.
type CreateModuleRequest struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	OpensAt     time.Time `json:"opens_at"`
	ClosesAt    time.Time `json:"closes_at"`
}

// UpdateModuleRequest is the payload for PATCH /modules/:id.
type UpdateModuleRequest struct {
	Title       *string    `json:"title,omitempty"`
	Description string     `json:"description,omitempty"`
	OpensAt     time.Time  `json:"opens_at"`
	ClosesAt    time.Time  `json:"closes_at"`
}

// ModuleResponse is the module object returned in responses.
type ModuleResponse struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	CreatedBy   int64      `json:"created_by"`
	Grade       *string    `json:"grade,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	OpensAt     time.Time  `json:"opens_at"`
	ClosesAt    time.Time  `json:"closes_at"`

	// Lecturers who teach the week (on GET /modules and GET /modules/{id}); a student cannot read the enrollments.
	Lecturers []ModuleLecturerResponse `json:"lecturers,omitempty"`
}

// ModuleLecturerResponse is a lecturer of a module: id and name only.
type ModuleLecturerResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// GradeModuleRequest is the payload for PATCH /modules/:id/grade.
type GradeModuleRequest struct {
	Grade string `json:"grade"`
}

// EnrollRequest is the payload for POST /modules/:moduleId/enrollments.
type EnrollRequest struct {
	UserID int64 `json:"user_id"`
}

// EnrollResponse is the enrollment object returned in responses.
type EnrollmentResponse struct {
	ID        int64     `json:"id"`
	ModuleID  int64     `json:"module_id"`
	UserID    int64     `json:"user_id"`
	Grade     *string   `json:"grade,omitempty"`
	GradedBy  *int64    `json:"graded_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`

	// Feedback is the mentor's module feedback that came with the grade, GradedAt when the grade was last written
	// (both left out for a week graded before they were kept, and Feedback for other students' enrollments).
	Feedback *string    `json:"feedback,omitempty"`
	GradedAt *time.Time `json:"graded_at,omitempty"`

	// Who is enrolled (on GET /modules/{id}/enrollments): an enrolled user with the lecturer role teaches the week.
	Name  string   `json:"name,omitempty"`
	Email string   `json:"email,omitempty"`
	Roles []string `json:"roles,omitempty"`
}

// GradeEnrollmentRequest is the payload for PATCH /modules/:moduleId/enrollments/:userId/grade.
type GradeEnrollmentRequest struct {
	Grade    string `json:"grade"`
	Feedback string `json:"feedback"` // the mentor's module feedback: required, 1 to 1000 characters
}
