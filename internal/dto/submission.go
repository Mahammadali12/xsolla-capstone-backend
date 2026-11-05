package dto

import "time"

// CreateSubmissionRequest is the payload for POST /courses/:courseId/content-items/:assignmentId/submissions.
type CreateSubmissionRequest struct {
	TextBody string  `json:"text_body"`
	FileIDs  []int64 `json:"file_ids"`
}

// SubmissionResponse is the submission object returned in responses.
type SubmissionResponse struct {
	ID           int64      `json:"id"`
	ContentItemID int64     `json:"content_item_id"`
	StudentID    int64     `json:"student_id"`
	TextBody     string    `json:"text_body"`
	SubmittedAt  time.Time `json:"submitted_at"`
	Grade        *float64  `json:"grade,omitempty"`
	Feedback     string    `json:"feedback,omitempty"`
	GradedBy     *int64    `json:"graded_by,omitempty"`
	GradedAt     *time.Time `json:"graded_at,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Files        []FileResponse `json:"files,omitempty"`
}

// GradeSubmissionRequest is the payload for POST /submissions/:id/grade.
type GradeSubmissionRequest struct {
	Grade    float64 `json:"grade"`
	Feedback string  `json:"feedback"`
}

// SubmissionRosterEntry is a student's submission summary for the teacher roster.
type SubmissionRosterEntry struct {
	StudentID   int64      `json:"student_id"`
	StudentName string     `json:"student_name"`
	StudentEmail string     `json:"student_email"`
	Submission  *SubmissionResponse `json:"submission,omitempty"`
}
