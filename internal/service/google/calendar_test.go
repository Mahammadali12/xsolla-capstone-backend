package google

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCalendarEventRequestsSendUpdatesAll(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotAttendees []Attendee

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("sendUpdates")

		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			var body struct {
				Attendees []Attendee `json:"attendees"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("%s body is not valid JSON: %v", r.Method, err)
			}
			gotAttendees = body.Attendees
		} else {
			gotAttendees = nil
		}

		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"evt-1"}`)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client(), baseURL: srv.URL}
	ev := CalendarEvent{
		Summary:   "1:1: Mentor & Student",
		Start:     EventTime{DateTime: "2026-01-01T10:00:00Z"},
		End:       EventTime{DateTime: "2026-01-01T10:30:00Z"},
		Attendees: []Attendee{{Email: "student@example.com"}},
	}

	tests := []struct {
		name       string
		method     string
		path       string
		wantEmails int
		call       func() error
	}{
		{
			name:       "insert",
			method:     "POST",
			path:       "/calendar/v3/calendars/primary/events",
			wantEmails: 1,
			call: func() error {
				created, err := c.CreateCalendarEvent(context.Background(), ev)
				if err == nil && created.ID != "evt-1" {
					t.Errorf("expected created event id evt-1, got %q", created.ID)
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
		})
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
