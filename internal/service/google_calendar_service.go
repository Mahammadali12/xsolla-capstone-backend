package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"xsolla-capstone-api/internal/service/google"
	"xsolla-capstone-api/internal/store"
)

// Sentinels the HTTP layer turns into the same codes the rest of the Google
// integration uses.
var (
	// ErrGoogleNotConnected means the user never linked a Google account.
	ErrGoogleNotConnected = errors.New("google account not connected")
	// ErrGoogleReconnect means the stored Google token can no longer be used.
	ErrGoogleReconnect = errors.New("google account must be reconnected")
	// ErrGoogleCalendarUnavailable means Google could not be reached or refused.
	ErrGoogleCalendarUnavailable = errors.New("google calendar unavailable")
)

// GoogleCalendarEvent is one event read from the user's Google Calendar.
// StartsAt and EndsAt are RFC 3339 for timed events and YYYY-MM-DD for all-day
// ones (EndsAt is the last day, inclusive).
type GoogleCalendarEvent struct {
	ID          string
	Title       string
	Description *string
	Location    *string
	StartsAt    string
	EndsAt      string
	AllDay      bool
	MeetingURL  *string
	HTMLLink    *string
}

// GoogleCalendarService reads events from the signed-in user's Google Calendar.
type GoogleCalendarService struct {
	googleSvc google.Service
}

// NewGoogleCalendarService creates a GoogleCalendarService backed by the given
// Google token service.
func NewGoogleCalendarService(g google.Service) *GoogleCalendarService {
	return &GoogleCalendarService{googleSvc: g}
}

// ListEvents returns the events of the caller's primary Google Calendar in
// [from, to]. It never reaches another user's calendar.
func (s *GoogleCalendarService) ListEvents(ctx context.Context, userID int64, from, to time.Time) ([]GoogleCalendarEvent, error) {
	accessToken, err := s.googleSvc.GetValidAccessToken(ctx, userID)
	if err != nil {
		if errors.Is(err, store.ErrRefreshTokenNotFound) {
			return nil, ErrGoogleNotConnected
		}
		return nil, ErrGoogleReconnect
	}

	client := google.NewClient(userID, s.googleSvc, accessToken)
	items, err := client.ListCalendarEvents(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGoogleCalendarUnavailable, err)
	}

	events := make([]GoogleCalendarEvent, 0, len(items))
	for _, item := range items {
		if ev, ok := mapListedEvent(item); ok {
			events = append(events, ev)
		}
	}
	return events, nil
}

// mapListedEvent converts one Google row. It returns false for rows the LMS
// calendar cannot place: cancelled events and events without a start.
func mapListedEvent(e google.ListedEvent) (GoogleCalendarEvent, bool) {
	if e.Status == "cancelled" {
		return GoogleCalendarEvent{}, false
	}

	title := e.Summary
	if title == "" {
		title = "(No title)"
	}
	ev := GoogleCalendarEvent{ID: e.ID, Title: title}
	if e.Description != "" {
		description := e.Description
		ev.Description = &description
	}
	if e.Location != "" {
		location := e.Location
		ev.Location = &location
	}
	if link := e.MeetLink(); link != "" {
		ev.MeetingURL = &link
	}
	if e.HTMLLink != "" {
		htmlLink := e.HTMLLink
		ev.HTMLLink = &htmlLink
	}

	switch {
	case e.Start.DateTime != "":
		ev.StartsAt = e.Start.DateTime
		if e.End.DateTime != "" {
			ev.EndsAt = e.End.DateTime
		} else {
			ev.EndsAt = ev.StartsAt
		}
	case e.Start.Date != "":
		start, err := time.Parse("2006-01-02", e.Start.Date)
		if err != nil {
			return GoogleCalendarEvent{}, false
		}
		ev.AllDay = true
		ev.StartsAt = start.Format("2006-01-02")
		// Google's end date is exclusive: an event on the 20th ends with
		// end.date = the 21st. Store the last day the event covers.
		end := start
		if e.End.Date != "" {
			if parsed, err := time.Parse("2006-01-02", e.End.Date); err == nil && parsed.After(start) {
				end = parsed.AddDate(0, 0, -1)
			}
		}
		ev.EndsAt = end.Format("2006-01-02")
	default:
		return GoogleCalendarEvent{}, false
	}

	return ev, true
}
