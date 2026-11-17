package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/service/google"
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
	MeetingURL   *string
	Attendees    []Attendee
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
		MeetingURL:   s.MeetingURL,
		CreatedAt:    s.CreatedAt,
	}
}

// MentorSlotService coordinates mentor slot operations.
type MentorSlotService struct {
	store     store.Store
	googleSvc google.Service
}

func NewMentorSlotService(s store.Store) *MentorSlotService {
	return &MentorSlotService{store: s}
}

// NewMentorSlotServiceWithGoogle adds Google Calendar invitations for bookings.
func NewMentorSlotServiceWithGoogle(s store.Store, g google.Service) *MentorSlotService {
	return &MentorSlotService{store: s, googleSvc: g}
}

// ListSlots returns slots visible to the caller.
// Mentor: their own slots. Student: their team mentor's slots. Admin: all slots.
func (s *MentorSlotService) ListSlots(ctx context.Context, userID int64, roles []string) ([]MentorSlot, error) {
	storeSlots, err := s.store.ListMentorSlots(ctx, userID, roles)
	if err != nil {
		return nil, err
	}
	slots := make([]MentorSlot, len(storeSlots))
	for i, slot := range storeSlots {
		slots[i] = *mentorSlotFromStore(slot)
	}
	if err := s.attachAttendees(ctx, slots, userID, roles); err != nil {
		return nil, err
	}
	// Students never see another student's booking identity, but the slot stays "booked".
	if auth.HasRole(roles, "student") {
		for i := range slots {
			if slots[i].BookedBy != nil && *slots[i].BookedBy != userID {
				slots[i].BookedBy = nil
				slots[i].BookedByName = nil
				slots[i].MeetingURL = nil
			}
		}
	}
	return slots, nil
}

// attachAttendees fills every slot's attendee list with one user lookup.
func (s *MentorSlotService) attachAttendees(ctx context.Context, slots []MentorSlot, viewerID int64, roles []string) error {
	ids := make([]int64, 0, len(slots)*2)
	for _, slot := range slots {
		ids = append(ids, slot.MentorID)
		if slot.BookedBy != nil {
			ids = append(ids, *slot.BookedBy)
		}
	}
	briefs, err := s.store.GetActiveUsersByIDs(ctx, ids)
	if err != nil {
		return err
	}
	byID := make(map[int64]Attendee, len(briefs))
	for _, b := range briefs {
		byID[b.ID] = Attendee{ID: b.ID, Name: b.Name, Email: b.Email}
	}
	for i := range slots {
		mentor, ok := byID[slots[i].MentorID]
		if !ok {
			// Fall back to the slot's joined name if the mentor is inactive.
			mentor = Attendee{ID: slots[i].MentorID, Name: slots[i].MentorName}
		}
		var student *Attendee
		if slots[i].BookedBy != nil {
			if a, ok := byID[*slots[i].BookedBy]; ok {
				student = &a
			}
		}
		slots[i].Attendees = slotAttendees(mentor, student, viewerID, roles)
	}
	return nil
}

// CreateAvailability cuts a time window into 30-minute slots for the caller (mentor or admin).
func (s *MentorSlotService) CreateAvailability(ctx context.Context, userID int64, roles []string, startsAt, endsAt time.Time) ([]MentorSlot, error) {
	if !auth.HasRole(roles, "mentor") && !auth.HasRole(roles, "admin") {
		return nil, ErrOnlyMentorCanManageSlots
	}

	// Validate the timezone-bearing instants supplied by the caller.
	startsAt, endsAt, err := parseWindow(startsAt, endsAt)
	if err != nil {
		return nil, err
	}

	storeSlots, err := s.store.CreateMentorAvailability(ctx, userID, startsAt, endsAt)
	if err != nil {
		return nil, err
	}
	slots := make([]MentorSlot, len(storeSlots))
	for i, slot := range storeSlots {
		slots[i] = *mentorSlotFromStore(slot)
	}
	if err := s.attachAttendees(ctx, slots, userID, roles); err != nil {
		return nil, err
	}
	return slots, nil
}

// parseWindow validates a timezone-bearing availability range.
func parseWindow(startsAt, endsAt time.Time) (time.Time, time.Time, error) {
	if startsAt.IsZero() {
		return time.Time{}, time.Time{}, errors.New("starts_at is required")
	}
	if endsAt.IsZero() {
		return time.Time{}, time.Time{}, errors.New("ends_at is required")
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
	if s.googleSvc != nil {
		if meet, err := s.createBookingCalendarEvent(ctx, *slot); err != nil {
			// Non-fatal: the booking still stands in the LMS.
			log.Printf("mentor slot service: Google Calendar sync failed for slot %d: %v", slot.ID, err)
		} else {
			slot.MeetingURL = meet
		}
	}
	slots := []MentorSlot{*mentorSlotFromStore(*slot)}
	if err := s.attachAttendees(ctx, slots, userID, roles); err != nil {
		return nil, err
	}
	return &slots[0], nil
}

// CancelBooking clears a booking if the caller is authorized and the slot hasn't started.
func (s *MentorSlotService) CancelBooking(ctx context.Context, userID int64, roles []string, slotID int64) (*MentorSlot, error) {
	// Read the Google event id before the cancel clears it.
	existing, err := s.store.GetMentorSlotByID(ctx, slotID)
	if err != nil {
		return nil, err
	}
	slot, err := s.store.CancelMentorSlotBooking(ctx, userID, roles, slotID)
	if err != nil {
		return nil, err
	}
	if s.googleSvc != nil && existing.GoogleEventID != nil && *existing.GoogleEventID != "" {
		if err := s.deleteBookingCalendarEvent(ctx, existing.MentorID, *existing.GoogleEventID); err != nil {
			// Non-fatal: the booking is already cancelled in the LMS.
			log.Printf("mentor slot service: Google Calendar delete failed for slot %d: %v", slotID, err)
		}
	}
	slots := []MentorSlot{*mentorSlotFromStore(*slot)}
	if err := s.attachAttendees(ctx, slots, userID, roles); err != nil {
		return nil, err
	}
	return &slots[0], nil
}

// CancelSlotsForMentor deletes every slot a mentor offers and its Google event.
// Used when the mentor stops leading a team.
func (s *MentorSlotService) CancelSlotsForMentor(ctx context.Context, mentorID int64) error {
	slots, err := s.store.DeleteMentorSlotsByMentor(ctx, mentorID)
	if err != nil {
		return err
	}
	if s.googleSvc == nil {
		return nil
	}
	for _, slot := range slots {
		if slot.GoogleEventID == nil || *slot.GoogleEventID == "" {
			continue
		}
		if err := s.deleteBookingCalendarEvent(ctx, mentorID, *slot.GoogleEventID); err != nil {
			// Non-fatal: the slots are already deleted in the LMS.
			log.Printf("mentor slot service: Google Calendar delete failed for slot %d: %v", slot.ID, err)
		}
	}
	return nil
}

// createBookingCalendarEvent puts the booking on the mentor's calendar, invites the student,
// and returns the Google Meet link Google created for it.
func (s *MentorSlotService) createBookingCalendarEvent(ctx context.Context, slot store.MentorSlot) (*string, error) {
	if slot.BookedBy == nil {
		return nil, nil
	}
	briefs, err := s.store.GetActiveUsersByIDs(ctx, []int64{slot.MentorID, *slot.BookedBy})
	if err != nil {
		return nil, err
	}
	var mentorName, studentName, studentEmail string
	for _, b := range briefs {
		switch b.ID {
		case slot.MentorID:
			mentorName = b.Name
		case *slot.BookedBy:
			studentName = b.Name
			studentEmail = b.Email
		}
	}
	if studentEmail == "" {
		return nil, errors.New("booked student has no email")
	}

	accessToken, err := s.googleSvc.GetValidAccessToken(ctx, slot.MentorID)
	if err != nil {
		return nil, err
	}
	client := google.NewClient(slot.MentorID, s.googleSvc, accessToken)

	created, err := client.CreateCalendarEvent(ctx, google.CalendarEvent{
		Summary:        fmt.Sprintf("1:1: %s & %s", mentorName, studentName),
		Description:    "Booked through Xsolla LMS",
		Start:          google.EventTime{DateTime: slot.StartsAt.Format(time.RFC3339), TimeZone: "UTC"},
		End:            google.EventTime{DateTime: slot.EndsAt.Format(time.RFC3339), TimeZone: "UTC"},
		Attendees:      []google.Attendee{{Email: studentEmail}},
		ConferenceData: google.NewMeetConference(),
	})
	if err != nil {
		return nil, err
	}
	if created.ID == "" {
		return nil, nil
	}
	var meet *string
	if link := created.MeetLink(); link != "" {
		meet = &link
	}
	if err := s.store.UpdateMentorSlotGoogle(ctx, slot.ID, &created.ID, meet); err != nil {
		return nil, err
	}
	return meet, nil
}

// deleteBookingCalendarEvent removes the booking from the mentor's calendar.
func (s *MentorSlotService) deleteBookingCalendarEvent(ctx context.Context, mentorID int64, googleEventID string) error {
	accessToken, err := s.googleSvc.GetValidAccessToken(ctx, mentorID)
	if err != nil {
		return err
	}
	client := google.NewClient(mentorID, s.googleSvc, accessToken)
	return client.DeleteCalendarEvent(ctx, googleEventID)
}
