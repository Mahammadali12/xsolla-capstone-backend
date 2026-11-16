package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsolla-capstone-api/internal/store"
)

// wgfStore is the part of the store that week grades use.
type wgfStore struct {
	store.Store
	team          []int64 // the mentor's students
	enrolled      bool
	enrollments   []store.Enrollment
	savedFeedback string
	saved         bool
}

func (f *wgfStore) GetMentorTeamDetail(ctx context.Context, mentorID int64) (*store.TeamDetail, error) {
	if f.team == nil {
		return nil, store.ErrTeamNotFound
	}
	d := &store.TeamDetail{}
	for _, id := range f.team {
		d.Members = append(d.Members, store.TeamMember{ID: id})
	}
	return d, nil
}

func (f *wgfStore) UpdateEnrollmentGrade(ctx context.Context, moduleID, userID int64, grade *string, feedback string, gradedBy int64) (*store.Enrollment, error) {
	f.saved, f.savedFeedback = true, feedback
	return &store.Enrollment{ModuleID: moduleID, UserID: userID, Grade: grade, GradeFeedback: &feedback}, nil
}

func (f *wgfStore) IsUserEnrolled(ctx context.Context, moduleID, userID int64) (bool, error) {
	return f.enrolled, nil
}

func (f *wgfStore) GetEnrollmentsForModule(ctx context.Context, moduleID int64) ([]store.Enrollment, error) {
	out := make([]store.Enrollment, len(f.enrollments))
	copy(out, f.enrollments)
	return out, nil
}

func TestWeekGradeNeedsModuleFeedback(t *testing.T) {
	mentor := []string{"mentor"}
	cases := []struct {
		name     string
		roles    []string
		student  int64
		grade    string
		feedback string
		want     error
		stored   string
	}{
		{"mentor with feedback", mentor, 31, "yellow", "Routing app was late.", nil, "Routing app was late."},
		{"feedback is trimmed", mentor, 31, "green", "  Good week.\n", nil, "Good week."},
		{"admin grades anyone", []string{"admin"}, 99, "red", "Missed every task.", nil, "Missed every task."},
		{"no feedback", mentor, 31, "green", "", ErrGradeFeedbackRequired, ""},
		{"only spaces", mentor, 31, "green", " \t\n ", ErrGradeFeedbackRequired, ""},
		{"1000 letters (not bytes) are fine", mentor, 31, "green", strings.Repeat("ə", 1000), nil, strings.Repeat("ə", 1000)},
		{"1001 letters", mentor, 31, "green", strings.Repeat("a", 1001), ErrGradeFeedbackTooLong, ""},
		{"a bad colour is still refused first", mentor, 31, "blue", "", ErrInvalidGrade, ""},
		{"not the student's mentor, whatever the feedback", mentor, 77, "green", "", store.ErrNotModuleTeacher, ""},
		{"a student cannot grade", []string{"student"}, 31, "green", "Fine.", store.ErrNotModuleTeacher, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := &wgfStore{team: []int64{31, 32}}
			e, err := NewModuleService(st).UpdateEnrollmentGrade(context.Background(), 5, c.roles, 4, c.student, c.grade, c.feedback)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if c.want != nil {
				if st.saved {
					t.Error("a refused grade reached the store")
				}
				return
			}
			if st.savedFeedback != c.stored || e.GradeFeedback == nil || *e.GradeFeedback != c.stored {
				t.Errorf("stored %q, want %q", st.savedFeedback, c.stored)
			}
		})
	}
}

func TestWeekGradeFeedbackIsForItsStudentOnly(t *testing.T) {
	note := func(s string) *string { return &s }
	rows := []store.Enrollment{
		{UserID: 31, GradeFeedback: note("Yours")},
		{UserID: 32, GradeFeedback: note("Someone else's")},
		{UserID: 33},
	}
	feedbackOf := func(list []store.Enrollment) map[int64]string {
		out := map[int64]string{}
		for _, e := range list {
			if e.GradeFeedback != nil {
				out[e.UserID] = *e.GradeFeedback
			}
		}
		return out
	}

	st := &wgfStore{enrolled: true, enrollments: rows}
	list, err := NewModuleService(st).GetEnrollmentsForModule(context.Background(), 31, []string{"student"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got := feedbackOf(list); len(got) != 1 || got[31] != "Yours" || len(list) != 3 {
		t.Errorf("student sees %v of %d rows, want only their own feedback on all 3 rows", got, len(list))
	}

	for _, role := range []string{"mentor", "admin", "lecturer"} {
		list, err := NewModuleService(st).GetEnrollmentsForModule(context.Background(), 5, []string{role}, 4)
		if err != nil {
			t.Fatal(err)
		}
		if got := feedbackOf(list); len(got) != 2 {
			t.Errorf("%s sees %v, want every student's feedback", role, got)
		}
	}
	if *rows[1].GradeFeedback != "Someone else's" {
		t.Error("hiding the feedback changed the store's rows")
	}
}
