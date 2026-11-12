package service

import (
	"testing"

	"xsolla-capstone-api/internal/store"
)

func TestModuleAttendees(t *testing.T) {
	creator := Attendee{ID: 1, Name: "Lecturer", Email: "lecturer@example.com"}
	enrollments := []store.Enrollment{
		{UserID: 1, Name: "Lecturer", Email: "lecturer@example.com"}, // creator is normally enrolled
		{UserID: 2, Name: "Student A", Email: "a@example.com"},
		{UserID: 2, Name: "Student A", Email: "a@example.com"}, // duplicate enrollment row
		{UserID: 3, Name: "Student B", Email: "b@example.com"},
	}

	got := moduleAttendees(creator, enrollments)

	if len(got) != 3 {
		t.Fatalf("expected 3 unique attendees, got %d: %+v", len(got), got)
	}
	if got[0].ID != 1 {
		t.Errorf("creator must come first, got %+v", got[0])
	}
	seen := map[int64]bool{}
	for _, a := range got {
		if seen[a.ID] {
			t.Errorf("duplicate attendee %d", a.ID)
		}
		seen[a.ID] = true
	}
}

func TestModuleAttendeesCreatorNotEnrolled(t *testing.T) {
	creator := Attendee{ID: 7, Name: "Lecturer", Email: "lecturer@example.com"}
	got := moduleAttendees(creator, []store.Enrollment{{UserID: 2, Name: "Student", Email: "s@example.com"}})

	if len(got) != 2 || got[0].ID != 7 || got[1].ID != 2 {
		t.Fatalf("expected creator + student, got %+v", got)
	}
}

func TestSlotAttendeesPrivacy(t *testing.T) {
	mentor := Attendee{ID: 10, Name: "Mentor", Email: "mentor@example.com"}
	student := Attendee{ID: 20, Name: "Student", Email: "student@example.com"}

	tests := []struct {
		name     string
		student  *Attendee
		viewerID int64
		roles    []string
		wantIDs  []int64
	}{
		{"free slot", nil, 20, []string{"student"}, []int64{10}},
		{"mentor sees student", &student, 10, []string{"mentor"}, []int64{10, 20}},
		{"booked student sees self", &student, 20, []string{"student"}, []int64{10, 20}},
		{"admin sees student", &student, 1, []string{"admin"}, []int64{10, 20}},
		{"other student hidden", &student, 21, []string{"student"}, []int64{10}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := slotAttendees(mentor, tc.student, tc.viewerID, tc.roles)
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("expected %d attendees, got %+v", len(tc.wantIDs), got)
			}
			for i, id := range tc.wantIDs {
				if got[i].ID != id {
					t.Errorf("attendee %d: expected id %d, got %d", i, id, got[i].ID)
				}
			}
		})
	}
}
