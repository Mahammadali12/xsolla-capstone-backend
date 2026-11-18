package dto

import "time"

// MentorSlotResponse is a single bookable 1:1 slot returned to the client.
type MentorSlotResponse struct {
	ID           string     `json:"id"`
	MentorID     int64      `json:"mentor_id"`
	MentorName   string     `json:"mentor_name"`
	StartsAt     time.Time  `json:"starts_at"`
	EndsAt       time.Time  `json:"ends_at"`
	Booked       bool       `json:"booked"`
	BookedBy     *int64     `json:"booked_by,omitempty"`
	BookedByName *string    `json:"booked_by_name,omitempty"`
	MeetingURL   *string    `json:"meeting_url,omitempty"`
	Attendees    []Attendee `json:"attendees"`
}

// CreateAvailabilityRequest is the payload for POST /mentor-slots.
type CreateAvailabilityRequest struct {
	StartsAt *time.Time `json:"starts_at"`
	EndsAt   *time.Time `json:"ends_at"`
}

// DiscussionResponse is a talk a mentor set up with a student (GET /mentor-discussions). It is not a 1:1: it moves no deadline.
// The rows made together share batch_id (the mentor sees one talk with several students).
type DiscussionResponse struct {
	ID          string    `json:"id"`
	BatchID     string    `json:"batch_id"`
	MentorID    int64     `json:"mentor_id"`
	MentorName  string    `json:"mentor_name"`
	StudentID   int64     `json:"student_id"`
	StudentName string    `json:"student_name"`
	Title       string    `json:"title"`
	Note        string    `json:"note,omitempty"`
	StartsAt    time.Time `json:"starts_at"`
	EndsAt      time.Time `json:"ends_at"`
}

// CreateDiscussionRequest is the payload for POST /mentor-discussions: the students are listed in student_ids,
// or whole_team invites the mentor's whole team.
type CreateDiscussionRequest struct {
	Title      string     `json:"title"`
	Note       string     `json:"note,omitempty"`
	StartsAt   *time.Time `json:"starts_at"`
	EndsAt     *time.Time `json:"ends_at"`
	StudentIDs []int64    `json:"student_ids,omitempty"`
	WholeTeam  bool       `json:"whole_team,omitempty"`
}
