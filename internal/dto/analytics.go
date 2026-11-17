package dto

import "time"

// AnalyticsPeriodResponse is the inclusive reporting period; dates are YYYY-MM-DD.
type AnalyticsPeriodResponse struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// AnalyticsWeekResponse is one week's standing counts. TotalCount counts only recorded grades.
type AnalyticsWeekResponse struct {
	ModuleID    int64     `json:"module_id"`
	ModuleTitle string    `json:"module_title"`
	OpensAt     time.Time `json:"opens_at"`
	GreenCount  int       `json:"green_count"`
	YellowCount int       `json:"yellow_count"`
	RedCount    int       `json:"red_count"`
	TotalCount  int       `json:"total_count"`
}

// AnalyticsOverviewResponse is GET /analytics/overview.
type AnalyticsOverviewResponse struct {
	Period      AnalyticsPeriodResponse `json:"period"`
	GeneratedAt time.Time               `json:"generated_at"`
	Weeks       []AnalyticsWeekResponse `json:"weeks"`
}

// AnalyticsPersonResponse is a student or mentor in analytics responses. Email appears in the selectors only.
type AnalyticsPersonResponse struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// AssignmentStatusResponse is the deadline classification of a student's assignments.
type AssignmentStatusResponse struct {
	OnTimeCount   int `json:"on_time_count"`
	LateCount     int `json:"late_count"`
	MissingCount  int `json:"missing_count"`
	EligibleCount int `json:"eligible_count"`
}

// QuizAccuracyResponse is one module's weighted quiz accuracy. Accuracy is null when no attempt had points.
type QuizAccuracyResponse struct {
	ModuleID      int64     `json:"module_id"`
	ModuleTitle   string    `json:"module_title"`
	OpensAt       time.Time `json:"opens_at"`
	ScoreTotal    int       `json:"score_total"`
	PossibleTotal int       `json:"possible_total"`
	Accuracy      *float64  `json:"accuracy"`
	AttemptCount  int       `json:"attempt_count"`
}

// StudentAnalyticsResponse is GET /analytics/students/{id}.
type StudentAnalyticsResponse struct {
	Student              AnalyticsPersonResponse  `json:"student"`
	Period               AnalyticsPeriodResponse  `json:"period"`
	GeneratedAt          time.Time                `json:"generated_at"`
	AssignmentStatus     AssignmentStatusResponse `json:"assignment_status"`
	QuizAccuracyByModule []QuizAccuracyResponse   `json:"quiz_accuracy_by_module"`
}

// FeedbackDimensionResponse is one rated dimension's average; null when nobody rated it.
type FeedbackDimensionResponse struct {
	Dimension string   `json:"dimension"`
	Average   *float64 `json:"average"`
	Count     int      `json:"count"`
}

// MentorAnalyticsResponse is GET /analytics/mentors/{id}. StandingAttribution is "current_roster" for now.
type MentorAnalyticsResponse struct {
	Mentor                AnalyticsPersonResponse     `json:"mentor"`
	Period                AnalyticsPeriodResponse     `json:"period"`
	GeneratedAt           time.Time                   `json:"generated_at"`
	FeedbackByDimension   []FeedbackDimensionResponse `json:"feedback_by_dimension"`
	StudentStandingByWeek []AnalyticsWeekResponse     `json:"student_standing_by_week"`
	StandingAttribution   string                      `json:"standing_attribution"`
}
