package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"xsolla-capstone-api/internal/service/google"
	"xsolla-capstone-api/internal/store"
)

func TestMapListedEvent(t *testing.T) {
	timed := google.ListedEvent{
		ID:          "evt-1",
		Summary:     "Talk",
		Description: "A talk",
		Location:    "Hall",
		Start:       google.EventTime{DateTime: "2026-11-20T10:00:00+04:00"},
		End:         google.EventTime{DateTime: "2026-11-20T11:00:00+04:00"},
		HangoutLink: "https://meet.google.com/abc-defg-hij",
		HTMLLink:    "https://calendar.google.com/event?eid=1",
	}

	allDay := google.ListedEvent{
		ID:      "evt-2",
		Summary: "Offsite",
		Start:   google.EventTime{Date: "2026-11-20"},
		End:     google.EventTime{Date: "2026-11-23"}, // exclusive
	}

	tests := []struct {
		name    string
		event   google.ListedEvent
		ok      bool
		title   string
		starts  string
		ends    string
		allDay  bool
		meeting string
		html    string
	}{
		{name: "timed", event: timed, ok: true, title: "Talk", starts: "2026-11-20T10:00:00+04:00", ends: "2026-11-20T11:00:00+04:00", meeting: "https://meet.google.com/abc-defg-hij", html: "https://calendar.google.com/event?eid=1"},
		{name: "all day end is inclusive", event: allDay, ok: true, title: "Offsite", starts: "2026-11-20", ends: "2026-11-22", allDay: true},
		{name: "cancelled", event: google.ListedEvent{ID: "x", Status: "cancelled", Start: google.EventTime{DateTime: "2026-11-20T10:00:00Z"}}, ok: false},
		{name: "no start", event: google.ListedEvent{ID: "x", Summary: "No start"}, ok: false},
		{name: "bad all-day date", event: google.ListedEvent{ID: "x", Start: google.EventTime{Date: "20-11-2026"}}, ok: false},
		{name: "empty title", event: google.ListedEvent{ID: "x", Start: google.EventTime{DateTime: "2026-11-20T10:00:00Z"}}, ok: true, title: "(No title)", starts: "2026-11-20T10:00:00Z", ends: "2026-11-20T10:00:00Z"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := mapListedEvent(tc.event)
			if ok != tc.ok {
				t.Fatalf("expected ok=%v, got %v", tc.ok, ok)
			}
			if !ok {
				return
			}
			if got.Title != tc.title {
				t.Errorf("title: expected %q, got %q", tc.title, got.Title)
			}
			if got.StartsAt != tc.starts {
				t.Errorf("starts: expected %q, got %q", tc.starts, got.StartsAt)
			}
			if got.EndsAt != tc.ends {
				t.Errorf("ends: expected %q, got %q", tc.ends, got.EndsAt)
			}
			if got.AllDay != tc.allDay {
				t.Errorf("all_day: expected %v, got %v", tc.allDay, got.AllDay)
			}
			if tc.meeting == "" && got.MeetingURL != nil {
				t.Errorf("expected no meeting link, got %q", *got.MeetingURL)
			}
			if tc.meeting != "" && (got.MeetingURL == nil || *got.MeetingURL != tc.meeting) {
				t.Errorf("meeting link: expected %q, got %v", tc.meeting, got.MeetingURL)
			}
			if tc.html == "" && got.HTMLLink != nil {
				t.Errorf("expected no html link, got %q", *got.HTMLLink)
			}
			if tc.html != "" && (got.HTMLLink == nil || *got.HTMLLink != tc.html) {
				t.Errorf("html link: expected %q, got %v", tc.html, got.HTMLLink)
			}
		})
	}
}

func TestMapListedEventKeepsDescriptionAndLocation(t *testing.T) {
	ev, ok := mapListedEvent(google.ListedEvent{
		ID:          "evt-1",
		Summary:     "Talk",
		Description: "A talk",
		Location:    "Hall",
		Start:       google.EventTime{DateTime: "2026-11-20T10:00:00Z"},
	})
	if !ok {
		t.Fatal("expected the event to map")
	}
	if ev.Description == nil || *ev.Description != "A talk" {
		t.Errorf("description not mapped: %v", ev.Description)
	}
	if ev.Location == nil || *ev.Location != "Hall" {
		t.Errorf("location not mapped: %v", ev.Location)
	}
	if ev.MeetingURL != nil || ev.HTMLLink != nil {
		t.Errorf("expected no links, got %v / %v", ev.MeetingURL, ev.HTMLLink)
	}
}

// stubGoogleTokenService lets the tests exercise the error mapping without a server.
type stubGoogleTokenService struct {
	token string
	err   error
}

func (s stubGoogleTokenService) GetValidAccessToken(context.Context, int64) (string, error) {
	return s.token, s.err
}

func TestGoogleCalendarServiceTokenErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "never linked", err: store.ErrRefreshTokenNotFound, want: ErrGoogleNotConnected},
		{name: "revoked or unreadable", err: errors.New("google token refresh failed: 400"), want: ErrGoogleReconnect},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewGoogleCalendarService(stubGoogleTokenService{err: tc.err})
			_, err := svc.ListEvents(context.Background(), 1, time.Time{}, time.Time{})
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}
