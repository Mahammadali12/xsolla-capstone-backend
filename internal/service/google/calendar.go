package google

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CalendarEvent represents a Google Calendar event.
type CalendarEvent struct {
	Summary        string          `json:"summary"`
	Description    string          `json:"description,omitempty"`
	Start          EventTime       `json:"start"`
	End            EventTime       `json:"end"`
	Attendees      []Attendee      `json:"attendees"`
	ConferenceData *ConferenceData `json:"conferenceData,omitempty"`
}

// ConferenceData carries the Google Meet conference of an event.
type ConferenceData struct {
	CreateRequest *ConferenceCreateRequest `json:"createRequest,omitempty"`
	EntryPoints   []EntryPoint             `json:"entryPoints,omitempty"`
}

// ConferenceCreateRequest asks Google to create a conference for a new event.
type ConferenceCreateRequest struct {
	RequestID             string                `json:"requestId"`
	ConferenceSolutionKey ConferenceSolutionKey `json:"conferenceSolutionKey"`
}

// ConferenceSolutionKey names the conference provider (hangoutsMeet = Google Meet).
type ConferenceSolutionKey struct {
	Type string `json:"type"`
}

// EntryPoint is one way to join a conference; the video one is the Meet URL.
type EntryPoint struct {
	EntryPointType string `json:"entryPointType"`
	URI            string `json:"uri"`
}

// NewMeetConference asks Google to add a Google Meet link to a new event.
func NewMeetConference() *ConferenceData {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		// A time-based id is unique enough for one request; never fail the event for this.
		return &ConferenceData{CreateRequest: &ConferenceCreateRequest{
			RequestID:             fmt.Sprintf("lms-%d", time.Now().UnixNano()),
			ConferenceSolutionKey: ConferenceSolutionKey{Type: "hangoutsMeet"},
		}}
	}
	return &ConferenceData{CreateRequest: &ConferenceCreateRequest{
		RequestID:             hex.EncodeToString(raw),
		ConferenceSolutionKey: ConferenceSolutionKey{Type: "hangoutsMeet"},
	}}
}

// EventTime represents the start or end time of a calendar event.
type EventTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

// Attendee represents a calendar event attendee.
type Attendee struct {
	Email string `json:"email"`
}

// createdEvent is the response from Google's events.insert endpoint.
type createdEvent struct {
	ID             string          `json:"id"`
	HTMLLink       string          `json:"htmlLink"`
	HangoutLink    string          `json:"hangoutLink"`
	ConferenceData *ConferenceData `json:"conferenceData"`
	Summary        string          `json:"summary"`
	Status         string          `json:"status"`
}

// MeetLink returns the Google Meet URL of the event, if Google created one.
func (e *createdEvent) MeetLink() string {
	if e.HangoutLink != "" {
		return e.HangoutLink
	}
	for _, ep := range e.ConferenceData.EntryPoints {
		if ep.EntryPointType == "video" && ep.URI != "" {
			return ep.URI
		}
	}
	return ""
}

// marshalEvent encodes an event, sending an empty attendees array instead of
// null so a PATCH can clear invitees that were removed.
func marshalEvent(ev CalendarEvent) ([]byte, error) {
	if ev.Attendees == nil {
		ev.Attendees = []Attendee{}
	}
	return json.Marshal(ev)
}

// CreateCalendarEvent creates a Google Calendar event on the user's primary calendar.
// Returns the created event with its Google-assigned ID and HTML link.
func (c *Client) CreateCalendarEvent(ctx context.Context, ev CalendarEvent) (*createdEvent, error) {
	data, err := marshalEvent(ev)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/calendar/v3/calendars/primary/events?sendUpdates=all&conferenceDataVersion=1", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	var result createdEvent
	if err := c.doWithRetry(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UpdateCalendarEvent updates an existing Google Calendar event.
// Returns the updated event.
func (c *Client) UpdateCalendarEvent(ctx context.Context, eventID string, ev CalendarEvent) (*createdEvent, error) {
	data, err := marshalEvent(ev)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", c.baseURL+"/calendar/v3/calendars/primary/events/"+eventID+"?sendUpdates=all", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	var result createdEvent
	if err := c.doWithRetry(req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteCalendarEvent deletes a Google Calendar event.
func (c *Client) DeleteCalendarEvent(ctx context.Context, eventID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.baseURL+"/calendar/v3/calendars/primary/events/"+eventID+"?sendUpdates=all", nil)
	if err != nil {
		return err
	}

	return c.doWithRetry(req, nil)
}
