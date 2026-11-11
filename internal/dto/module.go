package dto

import "time"

// CreateModuleRequest is the payload for POST /modules.
type CreateModuleRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// UpdateModuleRequest is the payload for PATCH /modules/:id.
type UpdateModuleRequest struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// ModuleResponse is the module object returned in responses.
type ModuleResponse struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedBy   int64     `json:"created_by"`
	Grade       *string   `json:"grade,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
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
	CreatedAt time.Time `json:"created_at"`
}

// GradeEnrollmentRequest is the payload for PATCH /modules/:moduleId/enrollments/:userId/grade.
type GradeEnrollmentRequest struct {
	Grade string `json:"grade"`
}
