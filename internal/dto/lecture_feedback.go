package dto

import "time"

// LectureFeedbackRequest is the payload of POST /lecture-feedback. Text answers may be left out.
type LectureFeedbackRequest struct {
	EventID    *int64  `json:"event_id"`
	Overall    *int    `json:"overall"`
	Clarity    *int    `json:"clarity"`
	Practice   *int    `json:"practice"`
	Pace       *string `json:"pace"`
	Confidence *int    `json:"confidence"`
	Liked      *string `json:"liked,omitempty"`
	Improve    *string `json:"improve,omitempty"`
}

// SentLectureFeedbackResponse is a lecture the student has given feedback on (POST answer, GET /lecture-feedback/me).
type SentLectureFeedbackResponse struct {
	EventID     int64     `json:"event_id"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// LectureRef is the lecture a feedback is about.
type LectureRef struct {
	EventID     int64     `json:"event_id"`
	Title       string    `json:"title"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
	ModuleID    *int64    `json:"module_id"`
	ModuleTitle *string   `json:"module_title"`
}

// PendingLectureResponse is a lecture whose form the student can fill now (GET /lecture-feedback/pending).
type PendingLectureResponse struct {
	LectureRef
	ClosesAt time.Time `json:"closes_at"`
}

// LectureFeedbackPersonResponse is a student or a lecturer in the admin routes.
type LectureFeedbackPersonResponse struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// LectureFeedbackResponse is one feedback with every answer (GET /lecture-feedback, admin).
type LectureFeedbackResponse struct {
	ID          int64                           `json:"id"`
	Lecture     LectureRef                      `json:"lecture"`
	Lecturers   []LectureFeedbackPersonResponse `json:"lecturers"`
	Student     LectureFeedbackPersonResponse   `json:"student"`
	Overall     int                             `json:"overall"`
	Clarity     int                             `json:"clarity"`
	Practice    int                             `json:"practice"`
	Pace        string                          `json:"pace"`
	Confidence  int                             `json:"confidence"`
	Liked       *string                         `json:"liked"`
	Improve     *string                         `json:"improve"`
	SubmittedAt time.Time                       `json:"submitted_at"`
}

// LectureFeedbackSummaryResponse is the numbers of one lecture (GET /lecture-feedback/summary, admin).
type LectureFeedbackSummaryResponse struct {
	Lecture      LectureRef                      `json:"lecture"`
	Lecturers    []LectureFeedbackPersonResponse `json:"lecturers"`
	Responses    int                             `json:"responses"`
	Expected     int                             `json:"expected"`
	Averages     map[string]*float64             `json:"averages"`
	Pace         map[string]int                  `json:"pace"`
	WithComments int                             `json:"with_comments"`
}
