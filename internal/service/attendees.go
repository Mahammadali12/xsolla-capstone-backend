package service

import (
	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/service/google"
	"xsolla-capstone-api/internal/store"
)

// Attendee is a participant of an event or a 1:1 mentor slot.
type Attendee struct {
	ID    int64
	Name  string
	Email string
}

// moduleAttendees returns the module creator followed by every enrolled user,
// de-duplicated by user ID (the creator is normally enrolled too).
func moduleAttendees(creator Attendee, enrollments []store.Enrollment) []Attendee {
	seen := make(map[int64]bool, len(enrollments)+1)
	out := make([]Attendee, 0, len(enrollments)+1)
	add := func(a Attendee) {
		if a.ID == 0 || seen[a.ID] {
			return
		}
		seen[a.ID] = true
		out = append(out, a)
	}
	add(creator)
	for _, e := range enrollments {
		add(Attendee{ID: e.UserID, Name: e.Name, Email: e.Email})
	}
	return out
}

// slotAttendees returns the mentor plus the booked student. The student is
// hidden from everyone except the mentor, an admin, and the student themselves.
func slotAttendees(mentor Attendee, student *Attendee, viewerID int64, roles []string) []Attendee {
	out := []Attendee{mentor}
	if student == nil {
		return out
	}
	if viewerID == mentor.ID || student.ID == viewerID || auth.HasRole(roles, "admin") {
		out = append(out, *student)
	}
	return out
}

// googleAttendees converts attendee emails into Google Calendar invitees.
func googleAttendees(attendees []Attendee) []google.Attendee {
	out := make([]google.Attendee, 0, len(attendees))
	for _, a := range attendees {
		if a.Email != "" {
			out = append(out, google.Attendee{Email: a.Email})
		}
	}
	return out
}
