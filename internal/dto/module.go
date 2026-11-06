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
	CreatedAt   time.Time `json:"created_at"`
}

// EnrollRequest is the payload for POST /modules/:moduleId/enrollments.
type EnrollRequest struct {
	UserID int64 `json:"user_id"`
}

// EnrollmentResponse is the enrollment object returned in responses.
type EnrollmentResponse struct {
	ID        int64     `json:"id"`
	ModuleID  int64     `json:"module_id"`
	UserID    int64     `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}
