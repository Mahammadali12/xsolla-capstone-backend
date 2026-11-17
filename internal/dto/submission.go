package dto

import "time"

// CreateSubmissionRequest is the payload for POST /modules/:moduleId/content-items/:assignmentId/submissions.
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
	Feedback     string    `json:"feedback,omitempty"`
	GradedBy     *int64    `json:"graded_by,omitempty"`
	GradedAt     *time.Time `json:"graded_at,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Files        []FileResponse `json:"files,omitempty"`
	// The student's deadline for the task (08:00 on the day of their 1:1) and whether this hand-in was late. On the
	// newest hand-in, late says the task is late: any of its hand-ins came after the deadline. A late hand-in turns the
	// week red on its own.
	DueAt *time.Time `json:"due_at,omitempty"`
	Late  bool       `json:"late"`
	// On GET .../submissions/me: the earlier hand-ins of the same task, newest first.
	History []SubmissionResponse `json:"history,omitempty"`
}

// FeedbackSubmissionRequest is the payload for POST /submissions/:id/feedback.
type FeedbackSubmissionRequest struct {
	Feedback string `json:"feedback"`
}

// SubmissionRosterEntry is a student's submission summary for the teacher roster.
type SubmissionRosterEntry struct {
	StudentID   int64      `json:"student_id"`
	StudentName string     `json:"student_name"`
	StudentEmail string     `json:"student_email"`
	Submission  *SubmissionResponse `json:"submission,omitempty"`
}
