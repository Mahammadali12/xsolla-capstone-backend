package service

import (
	"context"
	"errors"
	"time"

	"xsolla-capstone-api/internal/store"
)

// EventService coordinates event operations.
type EventService struct {
	store *store.Store
}

// NewEventService creates an EventService backed by the given store.
func NewEventService(s *store.Store) *EventService {
	return &EventService{store: s}
}

// ErrEventNotFound is returned when an event does not exist or is deleted.
var ErrEventNotFound = store.ErrEventNotFound

// ErrOnlyStaffCanCreateEvent is returned when a non-staff user tries to create an event.
var ErrOnlyStaffCanCreateEvent = errors.New("only teachers and admins can create events")

// ErrEndsBeforeStarts is returned when ends_at is before starts_at.
var ErrEndsBeforeStarts = errors.New("ends_at must be after starts_at")

// ErrModuleNotFound is returned when module_id references a non-existent content item.
var ErrModuleNotFound = errors.New("module not found")

// ErrNotTeacherOfCourse is returned when the caller is not the teacher of the course.
var ErrNotTeacherOfCourse = errors.New("not the teacher of this course")

// Event represents a calendar event.
type Event struct {
	ID          int64
	Title       string
	Kind        string
	StartsAt    time.Time
	EndsAt      time.Time
	AllDay      bool
	Location    *string
	Description *string
	ModuleID    *int64
	CreatedAt   time.Time
}

func eventFromStore(e store.Event) Event {
	return Event{
		ID:          e.ID,
		Title:       e.Title,
		Kind:        e.Kind,
		StartsAt:    e.StartsAt,
		EndsAt:      e.EndsAt,
		AllDay:      e.AllDay,
		Location:    e.Location,
		Description: e.Description,
		ModuleID:    e.ModuleID,
		CreatedAt:   e.CreatedAt,
	}
}

// ListEvents returns events in the given date range.
// Students see only events from modules in courses they're enrolled in.
// Teachers see events for courses they create.
// Admins see all events.
func (s *EventService) ListEvents(ctx context.Context, from time.Time, to time.Time, userID int64, role string) ([]Event, error) {
	storeEvents, err := s.store.ListEventsByRange(ctx, from, to, userID, role)
	if err != nil {
		return nil, err
	}
	events := make([]Event, len(storeEvents))
	for i, e := range storeEvents {
		events[i] = eventFromStore(e)
	}
	return events, nil
}

// GetEventByID returns an event by ID with authorization check.
func (s *EventService) GetEventByID(ctx context.Context, userID int64, role string, id int64) (*Event, error) {
	e, err := s.store.GetEventByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if role == "admin" {
		return eventFromStorePtr(e), nil
	}

	if e.ModuleID != nil {
		ci, err := s.store.GetContentItemByID(ctx, *e.ModuleID)
		if err != nil {
			return nil, err
		}
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, errors.New("not the teacher of this course")
		}
	}

	return eventFromStorePtr(e), nil
}

func eventFromStorePtr(e *store.Event) *Event {
	return &Event{
		ID:          e.ID,
		Title:       e.Title,
		Kind:        e.Kind,
		StartsAt:    e.StartsAt,
		EndsAt:      e.EndsAt,
		AllDay:      e.AllDay,
		Location:    e.Location,
		Description: e.Description,
		ModuleID:    e.ModuleID,
		CreatedAt:   e.CreatedAt,
	}
}

// CreateEvent creates a new calendar event.
// Only teachers and admins can create events. module_id must reference a
// content item in a course the caller teaches (or be nil).
func (s *EventService) CreateEvent(ctx context.Context, userID int64, role string, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64) (*Event, error) {
	if role != "teacher" && role != "admin" {
		return nil, ErrOnlyStaffCanCreateEvent
	}

	if endsAt.Before(startsAt) {
		return nil, ErrEndsBeforeStarts
	}

	// Validate module_id references a content item in a course the caller teaches
	if moduleID != nil {
		ci, err := s.store.GetContentItemByID(ctx, *moduleID)
		if err != nil {
			if errors.Is(err, store.ErrContentItemNotFound) {
				return nil, ErrModuleNotFound
			}
			return nil, err
		}
		if role != "admin" {
			teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
			if err != nil {
				return nil, err
			}
			if !teacher {
				return nil, ErrNotTeacherOfCourse
			}
		}
	}

	storeEvent, err := s.store.CreateEvent(ctx, title, kind, startsAt, endsAt, allDay, location, description, moduleID)
	if err != nil {
		return nil, err
	}
	ev := eventFromStore(*storeEvent)
	return &ev, nil
}

// UpdateEvent updates an event.
// Only the creator (via module course ownership) or admin can update.
func (s *EventService) UpdateEvent(ctx context.Context, userID int64, role string, id int64, title string, kind string, startsAt time.Time, endsAt time.Time, allDay bool, location *string, description *string, moduleID *int64) (*Event, error) {
	if role != "teacher" && role != "admin" {
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

	if role != "admin" && existing.ModuleID != nil {
		ci, err := s.store.GetContentItemByID(ctx, *existing.ModuleID)
		if err != nil {
			return nil, err
		}
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
		if err != nil {
			return nil, err
		}
		if !teacher {
			return nil, ErrNotTeacherOfCourse
		}
	}

	storeEvent, err := s.store.UpdateEvent(ctx, id, title, kind, startsAt, endsAt, allDay, location, description, moduleID, userID, role == "admin")
	if err != nil {
		return nil, err
	}
	ev := eventFromStore(*storeEvent)
	return &ev, nil
}

// DeleteEvent soft-deletes an event.
// Only the creator or admin can delete.
func (s *EventService) DeleteEvent(ctx context.Context, userID int64, role string, id int64) error {
	if role != "teacher" && role != "admin" {
		return ErrOnlyStaffCanCreateEvent
	}

	// Verify the event exists and the caller is authorized
	existing, err := s.store.GetEventByID(ctx, id)
	if err != nil {
		return err
	}

	if role != "admin" && existing.ModuleID != nil {
		ci, err := s.store.GetContentItemByID(ctx, *existing.ModuleID)
		if err != nil {
			return err
		}
		teacher, err := s.store.IsUserTeacherOfCourse(ctx, ci.CourseID, userID)
		if err != nil {
			return err
		}
		if !teacher {
			return ErrNotTeacherOfCourse
		}
	}

	return s.store.DeleteEvent(ctx, id, userID, role == "admin")
}
