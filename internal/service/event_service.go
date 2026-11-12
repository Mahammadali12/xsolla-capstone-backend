package service

import (
	"context"
	"errors"
	"log"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/service/google"
	"xsolla-capstone-api/internal/store"
)

// EventService coordinates event operations.
type EventService struct {
	store     store.Store
	googleSvc google.Service
}

// NewEventService creates an EventService backed by the given store.
func NewEventService(s store.Store) *EventService {
	return &EventService{store: s}
}

// NewEventServiceWithGoogle creates an EventService with Google Calendar integration.
func NewEventServiceWithGoogle(s store.Store, g google.Service) *EventService {
	return &EventService{store: s, googleSvc: g}
}

// ErrEventNotFound is returned when an event does not exist or is deleted.
var ErrEventNotFound = store.ErrEventNotFound

// ErrOnlyStaffCanCreateEvent is returned when a non-staff user tries to create an event.
var ErrOnlyStaffCanCreateEvent = errors.New("only lecturers and admins can create events")

// ErrEndsBeforeStarts is returned when ends_at is before starts_at.
var ErrEndsBeforeStarts = errors.New("ends_at must be after starts_at")

// ErrModuleNotFound is returned when module_id references a non-existent content item.
var ErrModuleNotFound = errors.New("module not found")

// ErrNotTeacherOfModule is returned when the caller is not the lecturer of the module.
var ErrNotTeacherOfModule = errors.New("not the lecturer of this module")

// Event represents a calendar event.
type Event struct {
	ID            int64
	Title         string
	Kind          string
	StartsAt      time.Time
	EndsAt        time.Time
	AllDay        bool
	Location      *string
	Description   *string
	ModuleID      *int64
	GoogleEventID *string
	Attendees     []Attendee
	CreatedAt     time.Time
}

func eventFromStore(e store.Event) Event {
	return Event{
		ID:            e.ID,
		Title:         e.Title,
		Kind:          e.Kind,
		StartsAt:      e.StartsAt,
		EndsAt:        e.EndsAt,
		AllDay:        e.AllDay,
		Location:      e.Location,
		Description:   e.Description,
		ModuleID:      e.ModuleID,
		GoogleEventID: e.GoogleEventID,
		CreatedAt:     e.CreatedAt,
	}
}

// ListEvents returns events in the given date range.
// Students see only events from modules in courses they're enrolled in.
// Lecturers see events for modules they create.
// Admins see all events.
func (s *EventService) ListEvents(ctx context.Context, from time.Time, to time.Time, userID int64, roles []string) ([]Event, error) {
	storeEvents, err := s.store.ListEventsByRange(ctx, from, to, userID, roles)
	if err != nil {
		return nil, err
	}
	events := make([]Event, len(storeEvents))
	attendeeCache := make(map[int64][]Attendee)
	for i, e := range storeEvents {
		ev := eventFromStore(e)
		if e.ModuleID != nil {
			attendees, ok := attendeeCache[*e.ModuleID]
			if !ok {
				var err error
				attendees, err = s.attendeesForModule(ctx, *e.ModuleID)
				if err != nil {
					return nil, err
				}
				attendeeCache[*e.ModuleID] = attendees
			}
			ev.Attendees = attendees
		}
		events[i] = ev
	}
	return events, nil
}

// attendeesForModule returns the module creator plus every enrolled user.
func (s *EventService) attendeesForModule(ctx context.Context, moduleID int64) ([]Attendee, error) {
	m, err := s.store.GetModuleByID(ctx, moduleID)
	if err != nil {
		return nil, err
	}
	creator, err := s.store.GetUserByID(ctx, m.CreatedBy)
	if err != nil {
		return nil, err
	}
	enrollments, err := s.store.GetEnrollmentsForModule(ctx, moduleID)
	if err != nil {
		return nil, err
	}
	return moduleAttendees(Attendee{ID: creator.ID, Name: creator.Name, Email: creator.Email}, enrollments), nil
}

// GetEventByID returns an event by ID with authorization check.
func (s *EventService) GetEventByID(ctx context.Context, userID int64, roles []string, id int64) (*Event, error) {
	e, err := s.store.GetEventByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if !auth.HasRole(roles, "admin") && e.ModuleID != nil {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, *e.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, errors.New("not the teacher of this module")
		}
	}

	ev := eventFromStorePtr(e)
	if e.ModuleID != nil {
		attendees, err := s.attendeesForModule(ctx, *e.ModuleID)
		if err != nil {
			return nil, err
		}
		ev.Attendees = attendees
	}
	return ev, nil
}

func eventFromStorePtr(e *store.Event) *Event {
	return &Event{
		ID:            e.ID,
		Title:         e.Title,
		Kind:          e.Kind,
		StartsAt:      e.StartsAt,
		EndsAt:        e.EndsAt,
		AllDay:        e.AllDay,
		Location:      e.Location,
		Description:   e.Description,
		ModuleID:      e.ModuleID,
		GoogleEventID: e.GoogleEventID,
		CreatedAt:     e.CreatedAt,
	}
}

// CreateEvent creates a new calendar event.
// Only lecturers and admins can create events. module_id must reference a
// module the caller teaches (or be nil).
// If syncToGoogle is true and moduleID is set, the event is also pushed to
// Google Calendar with enrolled students and the lecturer as attendees.
func (s *EventService) CreateEvent(ctx context.Context, userID int64, roles []string, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64, syncToGoogle bool) (*Event, error) {
	if !auth.HasRole(roles, "lecturer") && !auth.HasRole(roles, "admin") {
		return nil, ErrOnlyStaffCanCreateEvent
	}

	if endsAt.Before(startsAt) {
		return nil, ErrEndsBeforeStarts
	}

	// Validate module_id references a module the caller teaches
	if moduleID != nil {
		_, err := s.store.GetModuleByID(ctx, *moduleID)
		if err != nil {
			if errors.Is(err, store.ErrModuleNotFound) {
				return nil, ErrModuleNotFound
			}
			return nil, err
		}
		if !auth.HasRole(roles, "admin") {
			teacher, err := s.store.IsUserTeacherOfModule(ctx, *moduleID, userID)
			if err != nil {
				return nil, err
			}
			if !teacher {
				return nil, ErrNotTeacherOfModule
			}
		}
	}

	storeEvent, err := s.store.CreateEvent(ctx, title, kind, startsAt, endsAt, allDay, location, description, moduleID)
	if err != nil {
		return nil, err
	}
	ev := eventFromStore(*storeEvent)

	if ev.ModuleID != nil {
		attendees, err := s.attendeesForModule(ctx, *ev.ModuleID)
		if err != nil {
			return nil, err
		}
		ev.Attendees = attendees
	}

	// Push to Google Calendar if requested
	if syncToGoogle && s.googleSvc != nil {
		if err := s.pushEventToGoogle(ctx, userID, ev); err != nil {
			// Non-fatal: log and continue, event is still created in LMS
			log.Printf("event service: Google Calendar sync failed for event %d: %v", ev.ID, err)
		}
	}

	return &ev, nil
}

// pushEventToGoogle creates the event in Google Calendar and stores the google_event_id.
// No attendees, no module required — just the creator's own calendar.
func (s *EventService) pushEventToGoogle(ctx context.Context, userID int64, ev Event) error {
	// Get a valid access token for the user
	accessToken, err := s.googleSvc.GetValidAccessToken(ctx, userID)
	if err != nil {
		return err
	}

	// Create a Google API client with auto-refresh
	client := google.NewClient(userID, s.googleSvc, accessToken)

	googleEvent := google.CalendarEvent{
		Summary:     ev.Title,
		Description: orEmpty(ev.Description),
		Start: google.EventTime{
			DateTime: ev.StartsAt.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		End: google.EventTime{
			DateTime: ev.EndsAt.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		Attendees: googleAttendees(ev.Attendees),
	}

	created, err := client.CreateCalendarEvent(ctx, googleEvent)
	if err != nil {
		return err
	}

	// Store the google_event_id
	if created.ID != "" {
		if err := s.store.UpdateEventGoogleID(ctx, ev.ID, created.ID); err != nil {
			return err
		}
		ev.GoogleEventID = &created.ID
	}

	return nil
}

// patchEventToGoogle updates an existing Google Calendar event.
func (s *EventService) patchEventToGoogle(ctx context.Context, userID int64, eventID string, ev Event) error {
	// Get a valid access token for the user
	accessToken, err := s.googleSvc.GetValidAccessToken(ctx, userID)
	if err != nil {
		return err
	}

	// Create a Google API client with auto-refresh
	client := google.NewClient(userID, s.googleSvc, accessToken)

	googleEvent := google.CalendarEvent{
		Summary:     ev.Title,
		Description: orEmpty(ev.Description),
		Start: google.EventTime{
			DateTime: ev.StartsAt.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		End: google.EventTime{
			DateTime: ev.EndsAt.Format(time.RFC3339),
			TimeZone: "UTC",
		},
		Attendees: googleAttendees(ev.Attendees),
	}

	_, err = client.UpdateCalendarEvent(ctx, eventID, googleEvent)
	if err != nil {
		return err
	}

	return nil
}

// deleteEventFromGoogle deletes a Google Calendar event.
func (s *EventService) deleteEventFromGoogle(ctx context.Context, userID int64, eventID string) error {
	// Get a valid access token for the user
	accessToken, err := s.googleSvc.GetValidAccessToken(ctx, userID)
	if err != nil {
		return err
	}

	// Create a Google API client with auto-refresh
	client := google.NewClient(userID, s.googleSvc, accessToken)

	return client.DeleteCalendarEvent(ctx, eventID)
}

// orEmpty returns the string value of a *string pointer, or "" if nil.
func orEmpty(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

// UpdateEvent updates an event.
// Only the creator (via module ownership) or admin can update.
func (s *EventService) UpdateEvent(ctx context.Context, userID int64, roles []string, id int64, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64) (*Event, error) {
	if !auth.HasRole(roles, "lecturer") && !auth.HasRole(roles, "admin") {
		return nil, ErrOnlyStaffCanCreateEvent
	}

	if endsAt.Before(startsAt) {
		return nil, ErrEndsBeforeStarts
	}

	// Verify the event exists and the caller is authorized
	existing, err := s.store.GetEventByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if !auth.HasRole(roles, "admin") && existing.ModuleID != nil {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, *existing.ModuleID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, ErrNotTeacherOfModule
		}
	}

	storeEvent, err := s.store.UpdateEvent(ctx, id, title, kind, startsAt, endsAt, allDay, location, description, moduleID, userID, auth.HasRole(roles, "admin"))
	if err != nil {
		return nil, err
	}
	ev := eventFromStore(*storeEvent)

	if ev.ModuleID != nil {
		attendees, err := s.attendeesForModule(ctx, *ev.ModuleID)
		if err != nil {
			return nil, err
		}
		ev.Attendees = attendees
	}

	// Push update to Google Calendar if the event was previously synced
	if ev.GoogleEventID != nil && *ev.GoogleEventID != "" && s.googleSvc != nil {
		if err := s.patchEventToGoogle(ctx, userID, *ev.GoogleEventID, ev); err != nil {
			// Non-fatal: log and continue, event is still updated in LMS
			log.Printf("event service: Google Calendar update failed for event %d: %v", ev.ID, err)
		}
	}

	return &ev, nil
}

// DeleteEvent soft-deletes an event.
// Only the creator or admin can delete.
func (s *EventService) DeleteEvent(ctx context.Context, userID int64, roles []string, id int64) error {
	if !auth.HasRole(roles, "lecturer") && !auth.HasRole(roles, "admin") {
		return ErrOnlyStaffCanCreateEvent
	}

	// Verify the event exists and the caller is authorized
	existing, err := s.store.GetEventByID(ctx, id)
	if err != nil {
		return err
	}

	if !auth.HasRole(roles, "admin") && existing.ModuleID != nil {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, *existing.ModuleID, userID)
		if err != nil {
			return err
		}
		if !teacher {
			return ErrNotTeacherOfModule
		}
	}

	// Delete from Google Calendar if the event was previously synced
	if existing.GoogleEventID != nil && *existing.GoogleEventID != "" && s.googleSvc != nil {
		if err := s.deleteEventFromGoogle(ctx, userID, *existing.GoogleEventID); err != nil {
			// Non-fatal: log and continue, event is still deleted in LMS
			log.Printf("event service: Google Calendar delete failed for event %d: %v", id, err)
		}
	}

	return s.store.DeleteEvent(ctx, id, userID, auth.HasRole(roles, "admin"))
}
