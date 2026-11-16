package service

import (
	"context"
	"time"

	"xsolla-capstone-api/internal/store"
)

// A student's task deadline follows their weekly 1:1 with the mentor. The mentor opens slots on Friday and Monday;
// the work must be in by 08:00 (school time) on the day of the student's 1:1, so the mentor can look at it first.
//
// It applies to tasks whose own deadline falls on a 1:1 weekend, Friday to Monday in school time (the task form's
// default is Friday 08:00). Tasks due Tuesday to Thursday keep their own deadline.
//
//   - 1:1 booked that Friday to Monday: 08:00 on the day of the 1:1 (the earliest one, if there are several);
//   - no 1:1 booked yet: Monday 08:00, the latest a 1:1 can be, so nobody is late only because they have not booked.
//
// It is worked out on every read, so booking, cancelling or moving the 1:1 moves the deadline at once.
const (
	// DueFromOneOnOne: the deadline is 08:00 on the day of the student's booked 1:1.
	DueFromOneOnOne = "one_on_one"
	// DueFromNoOneOnOne: no 1:1 booked for that weekend yet, so the deadline is Monday 08:00.
	DueFromNoOneOnOne = "no_one_on_one"

	oneOnOneDueHour = 8
)

// oneOnOneWindow returns the 1:1 weekend a task deadline falls in: Friday 00:00 to Tuesday 00:00, school time.
// ok is false for a deadline from Tuesday to Thursday.
func oneOnOneWindow(taskDue time.Time) (from, to time.Time, ok bool) {
	l := taskDue.In(SchoolLocation)
	back := map[time.Weekday]int{time.Friday: 0, time.Saturday: 1, time.Sunday: 2, time.Monday: 3}
	days, ok := back[l.Weekday()]
	if !ok {
		return time.Time{}, time.Time{}, false
	}
	from = time.Date(l.Year(), l.Month(), l.Day()-days, 0, 0, 0, 0, SchoolLocation)
	return from, from.AddDate(0, 0, 4), true
}

// at8 is 08:00 school time on t's day.
func at8(t time.Time) time.Time {
	l := t.In(SchoolLocation)
	return time.Date(l.Year(), l.Month(), l.Day(), oneOnOneDueHour, 0, 0, 0, SchoolLocation)
}

// OneOnOneDueAt is a student's deadline for a task due at taskDue, given the start times of their booked 1:1s.
// from is "" when the task keeps its own deadline (it is not due on a 1:1 weekend).
func OneOnOneDueAt(taskDue time.Time, booked []time.Time) (due time.Time, from string) {
	start, end, ok := oneOnOneWindow(taskDue)
	if !ok {
		return taskDue, ""
	}
	var first *time.Time
	for i := range booked {
		b := booked[i]
		if !b.Before(start) && b.Before(end) && (first == nil || b.Before(*first)) {
			first = &booked[i]
		}
	}
	if first != nil {
		return at8(*first), DueFromOneOnOne
	}
	return at8(start.AddDate(0, 0, 3)), DueFromNoOneOnOne // Monday
}

// personalizeDeadlines replaces each task's deadline with the student's own (see OneOnOneDueAt). TaskDueAt keeps
// the deadline the lecturer set and DueFrom says why it changed.
func (s *ContentService) personalizeDeadlines(ctx context.Context, studentID int64, items []store.ContentItem) error {
	var from, to time.Time
	for _, ci := range items {
		if ci.Type != "assignment" || ci.DueAt == nil {
			continue
		}
		start, end, ok := oneOnOneWindow(*ci.DueAt)
		if !ok {
			continue
		}
		if from.IsZero() || start.Before(from) {
			from = start
		}
		if end.After(to) {
			to = end
		}
	}
	if from.IsZero() {
		return nil // no task due on a 1:1 weekend
	}
	booked, err := s.store.ListBookedSlotStarts(ctx, studentID, from, to)
	if err != nil {
		return err
	}
	for i := range items {
		ci := &items[i]
		if ci.Type != "assignment" || ci.DueAt == nil {
			continue
		}
		due, why := OneOnOneDueAt(*ci.DueAt, booked)
		if why == "" {
			continue
		}
		task := *ci.DueAt
		ci.TaskDueAt, ci.DueAt, ci.DueFrom = &task, &due, why
	}
	return nil
}
