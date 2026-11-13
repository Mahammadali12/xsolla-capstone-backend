package dto

// GoogleCalendarEventResponse is one event read from the caller's own Google
// Calendar (GET /google/calendar/events). It is deliberately separate from
// EventResponse: these rows are not LMS events and cannot be edited here.
type GoogleCalendarEventResponse struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
	Location    *string `json:"location,omitempty"`
	// StartsAt is RFC 3339 for timed events, YYYY-MM-DD for all-day ones.
	StartsAt string `json:"starts_at"`
	// EndsAt is RFC 3339, or the last covered day (inclusive) for all-day events.
	EndsAt     string  `json:"ends_at"`
	AllDay     bool    `json:"all_day"`
	MeetingURL *string `json:"meeting_url,omitempty"`
	HTMLLink   *string `json:"html_link,omitempty"`
}
