package dto

import "time"

// MentorSlotResponse is a single bookable 1:1 slot returned to the client.
type MentorSlotResponse struct {
	ID           string   `json:"id"`
	MentorID     int64    `json:"mentor_id"`
	MentorName   string   `json:"mentor_name"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	Booked       bool     `json:"booked"`
	BookedBy     *int64   `json:"booked_by,omitempty"`
	BookedByName *string  `json:"booked_by_name,omitempty"`
}

// CreateAvailabilityRequest is the payload for POST /mentor-slots.
type CreateAvailabilityRequest struct {
	Date  string `json:"date"`
	Start string `json:"start"`
	End   string `json:"end"`
}
