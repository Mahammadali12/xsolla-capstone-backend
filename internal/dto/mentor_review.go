package dto

import "time"

// MentorReviewRequest is the payload for POST /mentor-reviews. Fields that do not apply are left out.
type MentorReviewRequest struct {
	ModuleID        *int64         `json:"module_id"`
	HadSession      *bool          `json:"had_session"`
	NoSessionReason *string        `json:"no_session_reason,omitempty"`
	Topics          []string       `json:"topics,omitempty"`
	OtherTopic      *string        `json:"other_topic,omitempty"`
	Ratings         map[string]int `json:"ratings,omitempty"`
	Overall         *int           `json:"overall"`
	Comment         *string        `json:"comment,omitempty"`
}

// ReviewedWeekResponse is one week the student has reviewed (GET /mentor-reviews/me, POST /mentor-reviews).
type ReviewedWeekResponse struct {
	ModuleID    int64     `json:"module_id"`
	SubmittedAt time.Time `json:"submitted_at"`
}
