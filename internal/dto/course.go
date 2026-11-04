package dto

import "time"

// CreateCourseRequest is the payload for POST /courses.
type CreateCourseRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// UpdateCourseRequest is the payload for PATCH /courses/:id.
type UpdateCourseRequest struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// CourseResponse is the course object returned in responses.
type CourseResponse struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedBy   int64     `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// EnrollRequest is the payload for POST /courses/:courseId/enrollments.
type EnrollRequest struct {
	UserID int64 `json:"user_id"`
}

// EnrollmentResponse is the enrollment object returned in responses.
type EnrollmentResponse struct {
	ID        int64     `json:"id"`
	CourseID  int64     `json:"course_id"`
	UserID    int64     `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}
