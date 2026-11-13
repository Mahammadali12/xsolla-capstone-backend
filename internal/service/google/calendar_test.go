package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestCalendarEventRequestsSendUpdatesAll(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotConferenceVersion, gotConferenceType string
	var gotAttendees []Attendee

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("sendUpdates")
		gotConferenceVersion = r.URL.Query().Get("conferenceDataVersion")
		gotConferenceType = ""

		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			var body struct {
				Attendees      []Attendee      `json:"attendees"`
				ConferenceData *ConferenceData `json:"conferenceData"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("%s body is not valid JSON: %v", r.Method, err)
			}
			gotAttendees = body.Attendees
			if body.ConferenceData != nil && body.ConferenceData.CreateRequest != nil {
				gotConferenceType = body.ConferenceData.CreateRequest.ConferenceSolutionKey.Type
			}
		} else {
			gotAttendees = nil
		}

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"evt-1","hangoutLink":"https://meet.google.com/abc-defg-hij"}`)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL}
	ev := CalendarEvent{
		Summary:   "1:1: Mentor & Student",
		Start:     EventTime{DateTime: "2026-01-01T10:00:00Z"},
		End:       EventTime{DateTime: "2026-01-01T10:30:00Z"},
		Attendees: []Attendee{{Email: "student@example.com"}},
	}
	meetEv := ev
	meetEv.ConferenceData = NewMeetConference()

	tests := []struct {
		name       string
		method     string
		path       string
		wantEmails int
		wantMeet   bool
		call       func() error
	}{
		{
			name:       "insert",
			method:     "POST",
			path:       "/calendar/v3/calendars/primary/events",
			wantEmails: 1,
			wantMeet:   true,
			call: func() error {
				created, err := c.CreateCalendarEvent(context.Background(), meetEv)
				if err == nil && created.ID != "evt-1" {
					t.Errorf("expected created event id evt-1, got %q", created.ID)
				}
				if err == nil && created.MeetLink() != "https://meet.google.com/abc-defg-hij" {
					t.Errorf("expected the Meet link, got %q", created.MeetLink())
				}
				return err
			},
		},
		{
			name:       "patch",
			method:     "PATCH",
			path:       "/calendar/v3/calendars/primary/events/evt-1",
			wantEmails: 1,
			call:       func() error { _, err := c.UpdateCalendarEvent(context.Background(), "evt-1", ev); return err },
		},
		{
			name:   "delete",
			method: "DELETE",
			path:   "/calendar/v3/calendars/primary/events/evt-1",
			call:   func() error { return c.DeleteCalendarEvent(context.Background(), "evt-1") },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotAttendees = nil
			if err := tc.call(); err != nil {
				t.Fatalf("%s failed: %v", tc.name, err)
			}
			if gotMethod != tc.method {
				t.Errorf("expected method %s, got %s", tc.method, gotMethod)
			}
			if gotPath != tc.path {
				t.Errorf("expected path %s, got %s", tc.path, gotPath)
			}
			if gotQuery != "all" {
				t.Errorf("expected sendUpdates=all, got %q", gotQuery)
			}
			if len(gotAttendees) != tc.wantEmails {
				t.Errorf("expected %d attendees, got %+v", tc.wantEmails, gotAttendees)
			}
			if tc.wantMeet {
				if gotConferenceVersion != "1" {
					t.Errorf("expected conferenceDataVersion=1, got %q", gotConferenceVersion)
				}
				if gotConferenceType != "hangoutsMeet" {
					t.Errorf("expected a hangoutsMeet conference, got %q", gotConferenceType)
				}
			} else if gotConferenceType != "" {
				t.Errorf("%s must not create a conference, got %q", tc.name, gotConferenceType)
			}
		})
	}
}

func TestMeetLinkFallsBackToEntryPoints(t *testing.T) {
	ev := createdEvent{ConferenceData: &ConferenceData{EntryPoints: []EntryPoint{
		{EntryPointType: "phone", URI: "tel:+123"},
		{EntryPointType: "video", URI: "https://meet.google.com/xyz-abcd-efg"},
	}}}
	if got := ev.MeetLink(); got != "https://meet.google.com/xyz-abcd-efg" {
		t.Fatalf("expected the video entry point, got %q", got)
	}
}

func TestCalendarEventEmptyAttendeesMarshalAsArray(t *testing.T) {
	data, err := marshalEvent(CalendarEvent{Summary: "workshop"})
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	attendees, ok := body["attendees"]
	if !ok {
		t.Fatal("attendees must be present so an update can clear previous invitees")
	}
	if arr, ok := attendees.([]any); !ok || len(arr) != 0 {
		t.Fatalf("expected an empty attendees array, got %#v", attendees)
	}
}

func TestListCalendarEventsFollowsPages(t *testing.T) {
	var queries []url.Values
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query())
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if len(queries) == 1 {
			io.WriteString(w, `{"items":[{"id":"a","summary":"Talk","start":{"dateTime":"2026-11-20T10:00:00Z"},"end":{"dateTime":"2026-11-20T11:00:00Z"},"hangoutLink":"https://meet.google.com/abc-defg-hij"}],"nextPageToken":"p2"}`)
			return
		}
		io.WriteString(w, `{"items":[{"id":"b","summary":"Lunch","start":{"dateTime":"2026-11-20T12:00:00Z"},"end":{"dateTime":"2026-11-20T13:00:00Z"}}]}`)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL}
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	events, err := c.ListCalendarEvents(context.Background(), from, to)
	if err != nil {
		t.Fatalf("ListCalendarEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected both pages, got %d events", len(events))
	}
	if events[0].MeetLink() != "https://meet.google.com/abc-defg-hij" {
		t.Errorf("expected the hangout link, got %q", events[0].MeetLink())
	}
	if len(queries) != 2 {
		t.Fatalf("expected two requests (one page token), got %d", len(queries))
	}
	for i, q := range queries {
		if paths[i] != "/calendar/v3/calendars/primary/events" {
			t.Errorf("request %d: expected the primary events path, got %q", i, paths[i])
		}
		if q.Get("singleEvents") != "true" || q.Get("orderBy") != "startTime" || q.Get("showDeleted") != "false" {
			t.Errorf("request %d: unexpected query %v", i, q)
		}
		if q.Get("timeMin") != from.Format(time.RFC3339) || q.Get("timeMax") != to.Format(time.RFC3339) {
			t.Errorf("request %d: unexpected range %v", i, q)
		}
	}
	if queries[0].Get("pageToken") != "" {
		t.Errorf("first request must not carry a page token, got %q", queries[0].Get("pageToken"))
	}
	if queries[1].Get("pageToken") != "p2" {
		t.Errorf("second request must carry the page token, got %q", queries[1].Get("pageToken"))
	}
}
