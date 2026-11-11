package service

import (
	"context"
	"errors"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/store"
)

var (
	// ErrOnlyMentorCanManageSlots is returned when a non-mentor tries to create or delete slots.
	ErrOnlyMentorCanManageSlots = errors.New("only mentors can manage 1:1 slots")
	// ErrOnlyStudentsCanBook is returned when a non-student tries to book a slot.
	ErrOnlyStudentsCanBook = errors.New("only students can book a 1:1 slot")
	// ErrSlotNotBookable is returned when a slot is already booked or has started.
	ErrSlotNotBookable = store.ErrSlotNotBookable
	// ErrStudentHasBooking is returned when a student already has an upcoming booking.
	ErrStudentHasBooking = store.ErrStudentHasBooking
	// ErrMentorSlotNotFound is returned when a mentor slot does not exist or is deleted.
	ErrMentorSlotNotFound = store.ErrMentorSlotNotFound
)

// MentorSlot is a bookable 1:1 time offered by a mentor.
type MentorSlot struct {
	ID           int64
	MentorID     int64
	MentorName   string
	StartsAt     time.Time
	EndsAt       time.Time
	Booked       bool
	BookedBy     *int64
	BookedByName *string
	CreatedAt    time.Time
}

func mentorSlotFromStore(s store.MentorSlot) *MentorSlot {
	return &MentorSlot{
		ID:           s.ID,
		MentorID:     s.MentorID,
		MentorName:   s.MentorName,
		StartsAt:     s.StartsAt,
		EndsAt:       s.EndsAt,
		Booked:       s.BookedBy != nil,
		BookedBy:     s.BookedBy,
		BookedByName: s.BookedByName,
		CreatedAt:    s.CreatedAt,
	}
}

// MentorSlotService coordinates mentor slot operations.
type MentorSlotService struct {
	store store.Store
}

func NewMentorSlotService(s store.Store) *MentorSlotService {
	return &MentorSlotService{store: s}
}

// ListSlots returns slots visible to the caller.
// Mentor: their own slots. Student: their team mentor's slots. Admin: all slots.
func (s *MentorSlotService) ListSlots(ctx context.Context, userID int64, roles []string) ([]MentorSlot, error) {
	storeSlots, err := s.store.ListMentorSlots(ctx, userID, roles)
	if err != nil {
		return nil, err
	}
	slots := make([]MentorSlot, len(storeSlots))
	isStudent := auth.HasRole(roles, "student")
	for i, slot := range storeSlots {
		booked := slot.BookedBy != nil // capture before privacy filter
		// Students never see another student's booking identity, but the slot stays "booked".
		if isStudent && slot.BookedBy != nil && *slot.BookedBy != userID {
			slot.BookedBy = nil
			slot.BookedByName = nil
		}
		ms := mentorSlotFromStore(slot)
		ms.Booked = booked
		slots[i] = *ms
	}
	return slots, nil
}

// CreateAvailability cuts a time window into 30-minute slots for the caller (mentor or admin).
func (s *MentorSlotService) CreateAvailability(ctx context.Context, userID int64, roles []string, date, start, end string) ([]MentorSlot, error) {
	if !auth.HasRole(roles, "mentor") && !auth.HasRole(roles, "admin") {
		return nil, ErrOnlyMentorCanManageSlots
	}

	// Parse local date + HH:MM into UTC timestamps.
	startsAt, endsAt, err := parseWindow(date, start, end)
	if err != nil {
		return nil, err
	}

	var slots []MentorSlot
	t := startsAt
	for !t.Add(30 * time.Minute).After(endsAt) {
		slotEnd := t.Add(30 * time.Minute)
		// Skip slots that clash with an existing one.
		overlap, err := s.store.MentorSlotsOverlap(ctx, userID, t, slotEnd)
		if err != nil {
			return nil, err
		}
		if overlap > 0 {
			t = slotEnd
			continue
		}
		slot, err := s.store.CreateMentorSlot(ctx, userID, t, slotEnd)
		if err != nil {
			return nil, err
		}
		slots = append(slots, *mentorSlotFromStore(*slot))
		t = slotEnd
	}
	return slots, nil
}

// parseWindow converts a local date + HH:MM range into UTC time.Time values.
// The backend stores whatever ISO it receives; timezone interpretation is the caller's concern.
func parseWindow(date, start, end string) (time.Time, time.Time, error) {
	layout := "2006-01-02 15:04"
	startsAt, err := time.Parse(layout, date+" "+start)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid date or start time")
	}
	endsAt, err := time.Parse(layout, date+" "+end)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("invalid date or end time")
	}
	if !endsAt.After(startsAt) {
		return time.Time{}, time.Time{}, errors.New("end time must be after start time")
	}
	if endsAt.Sub(startsAt) < 30*time.Minute {
		return time.Time{}, time.Time{}, errors.New("window must be at least 30 minutes long")
	}
	if !startsAt.After(time.Now()) {
		return time.Time{}, time.Time{}, errors.New("that time has already passed")
	}
	return startsAt, endsAt, nil
}

// RemoveSlot deletes a free slot if the caller is authorized.
func (s *MentorSlotService) RemoveSlot(ctx context.Context, userID int64, roles []string, id int64) error {
	if !auth.HasRole(roles, "mentor") && !auth.HasRole(roles, "admin") {
		return ErrOnlyMentorCanManageSlots
	}
	return s.store.DeleteMentorSlot(ctx, userID, roles, id)
}

// BookSlot books a free slot for a student.
func (s *MentorSlotService) BookSlot(ctx context.Context, userID int64, roles []string, slotID int64) (*MentorSlot, error) {
	if !auth.HasRole(roles, "student") {
		return nil, ErrOnlyStudentsCanBook
	}
	slot, err := s.store.BookMentorSlot(ctx, userID, slotID)
	if err != nil {
		return nil, err
	}
	return mentorSlotFromStore(*slot), nil
}

// CancelBooking clears a booking if the caller is authorized and the slot hasn't started.
func (s *MentorSlotService) CancelBooking(ctx context.Context, userID int64, roles []string, slotID int64) (*MentorSlot, error) {
	slot, err := s.store.CancelMentorSlotBooking(ctx, userID, roles, slotID)
	if err != nil {
		return nil, err
	}
	return mentorSlotFromStore(*slot), nil
}
