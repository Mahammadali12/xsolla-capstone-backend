package dto

import "time"

// EventResponse is the event object returned in responses.
type EventResponse struct {
	ID            int64      `json:"id"`
	Title         string     `json:"title"`
	Kind          string     `json:"kind"`
	StartsAt      time.Time  `json:"starts_at"`
	EndsAt        time.Time  `json:"ends_at"`
	AllDay        bool       `json:"all_day"`
	Location      *string    `json:"location,omitempty"`
	Description   *string    `json:"description,omitempty"`
	MeetingURL    *string    `json:"meeting_url,omitempty"`
	ModuleID      *int64     `json:"module_id,omitempty"`
	GoogleEventID *string    `json:"google_event_id,omitempty"`
	Attendees     []Attendee `json:"attendees"`
	CreatedAt     time.Time  `json:"created_at"`
}

// CreateEventRequest is the payload for POST /events.
type CreateEventRequest struct {
	Title        string `json:"title"`
	Kind         string `json:"kind"`
	StartsAt     string `json:"starts_at"`
	EndsAt       string `json:"ends_at"`
	AllDay       bool   `json:"all_day"`
	Location     string `json:"location,omitempty"`
	Description  string `json:"description,omitempty"`
	MeetingURL   string `json:"meeting_url,omitempty"`
	ModuleID     int64  `json:"module_id,omitempty"`
	SyncToGoogle bool   `json:"sync_to_google"`
}

// UpdateEventRequest is the payload for PATCH /events/:id.
type UpdateEventRequest struct {
	Title       *string `json:"title,omitempty"`
	Kind        *string `json:"kind,omitempty"`
	StartsAt    *string `json:"starts_at,omitempty"`
	EndsAt      *string `json:"ends_at,omitempty"`
	AllDay      *bool   `json:"all_day,omitempty"`
	Location    *string `json:"location,omitempty"`
	Description *string `json:"description,omitempty"`
	MeetingURL  *string `json:"meeting_url,omitempty"`
	ModuleID    *int64  `json:"module_id,omitempty"`
}
