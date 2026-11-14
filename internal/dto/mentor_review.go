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

// ---- admin read routes ----

// ReviewPerson is a student or mentor in the admin review routes.
type ReviewPerson struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// ReviewTeam is the team a review or a missing student belongs to.
type ReviewTeam struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// MentorReviewRatingsResponse holds the four rated statements, 1 to 5.
type MentorReviewRatingsResponse struct {
	Clarity    int `json:"clarity"`
	Engagement int `json:"engagement"`
	Comfort    int `json:"comfort"`
	Time       int `json:"time"`
}

// MentorReviewResponse is one review with every answer (GET /mentor-reviews, admin).
type MentorReviewResponse struct {
	ID              int64                        `json:"id"`
	ModuleID        int64                        `json:"module_id"`
	ModuleTitle     string                       `json:"module_title"`
	Student         ReviewPerson                 `json:"student"`
	Mentor          ReviewPerson                 `json:"mentor"`
	Team            *ReviewTeam                  `json:"team"`
	HadSession      bool                         `json:"had_session"`
	NoSessionReason *string                      `json:"no_session_reason"`
	Topics          []string                     `json:"topics"`
	OtherTopic      *string                      `json:"other_topic"`
	Ratings         *MentorReviewRatingsResponse `json:"ratings"`
	Overall         int                          `json:"overall"`
	Comment         *string                      `json:"comment"`
	SubmittedAt     time.Time                    `json:"submitted_at"`
}

// MentorReviewSummaryResponse is one mentor's numbers for one week (GET /mentor-reviews/summary, admin).
type MentorReviewSummaryResponse struct {
	ModuleID         int64               `json:"module_id"`
	ModuleTitle      string              `json:"module_title"`
	Mentor           ReviewPerson        `json:"mentor"`
	Reviews          int                 `json:"reviews"`
	Expected         int                 `json:"expected"`
	SessionsHeld     int                 `json:"sessions_held"`
	NoSession        int                 `json:"no_session"`
	Averages         map[string]*float64 `json:"averages"`
	Topics           map[string]int      `json:"topics"`
	NoSessionReasons map[string]int      `json:"no_session_reasons"`
}

// MissingReviewResponse is a student who has not sent the week's review (GET /mentor-reviews/missing, admin).
type MissingReviewResponse struct {
	Student ReviewPerson `json:"student"`
	Team    ReviewTeam   `json:"team"`
	Mentor  ReviewPerson `json:"mentor"`
}
