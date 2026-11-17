package service

import (
	"context"
	"time"

	"xsolla-capstone-api/internal/store"
)

// A student's work for a week is due by 08:00 (school time) on the day of their 1:1 with the mentor, so the mentor can
// look at it before the meeting. A task has no deadline of its own: lecturers do not set one.
//
// The 1:1 weekend belongs to the module: it starts on the first Friday on or after the day the module opens, and ends
// on the Tuesday after (00:00). Mentors open their 1:1 slots on Friday and Monday.
//
//   - 1:1 booked in that weekend: 08:00 on the day of the 1:1 (the earliest one, if there are several);
//   - no 1:1 booked yet: Monday 08:00, the latest a 1:1 can be, so nobody is late only because they have not booked.
//
// It is worked out on every read, so booking, cancelling or moving the 1:1 moves the deadline at once.
//
// Late work: the work is accepted at any time, but a hand-in after the deadline is late, and a late hand-in turns the
// student's grade for that week red (a mentor or admin can change it afterwards, for an exceptional case).
//
//   - the first hand-in is late when it comes after the deadline;
//   - a later hand-in (a change) is late when it comes after the deadline and before the 1:1 is over (the mentor may
//     already have read the earlier work). A change after the 1:1, for example after the feedback, is not late;
//   - a task is late when any of its hand-ins is late.
const (
	// DueFromOneOnOne: the deadline is 08:00 on the day of the student's booked 1:1.
	DueFromOneOnOne = "one_on_one"
	// DueFromNoOneOnOne: no 1:1 booked for that weekend yet, so the deadline is Monday 08:00.
	DueFromNoOneOnOne = "no_one_on_one"

	oneOnOneDueHour = 8
)

// oneOnOneWeekend returns the 1:1 weekend of a module that opens at opensAt: from Friday 00:00 (the first Friday on or
// after the opening day) to the Tuesday after at 00:00, school time.
func oneOnOneWeekend(opensAt time.Time) (from, to time.Time) {
	l := opensAt.In(SchoolLocation)
	days := (int(time.Friday) - int(l.Weekday()) + 7) % 7
	from = time.Date(l.Year(), l.Month(), l.Day()+days, 0, 0, 0, 0, SchoolLocation)
	return from, from.AddDate(0, 0, 4)
}

// at8 is 08:00 school time on t's day.
func at8(t time.Time) time.Time {
	l := t.In(SchoolLocation)
	return time.Date(l.Year(), l.Month(), l.Day(), oneOnOneDueHour, 0, 0, 0, SchoolLocation)
}

// Deadline is a student's deadline for a week's work.
type Deadline struct {
	// DueAt is when the work is due.
	DueAt time.Time
	// From is DueFromOneOnOne or DueFromNoOneOnOne.
	From string
	// MeetingEnd is when the 1:1 is over: its end, or the end of the weekend while none is booked. A change handed in
	// between DueAt and MeetingEnd is late.
	MeetingEnd time.Time
}

// StudentDeadline works out a student's deadline for a module that opens at opensAt, given their booked 1:1s (only the
// ones in the module's 1:1 weekend count).
func StudentDeadline(opensAt time.Time, booked []store.BookedSlot) Deadline {
	from, to := oneOnOneWeekend(opensAt)
	var first *store.BookedSlot
	for i := range booked {
		b := &booked[i]
		if b.StartsAt.Before(from) || !b.StartsAt.Before(to) {
			continue
		}
		if first == nil || b.StartsAt.Before(first.StartsAt) {
			first = b
		}
	}
	if first != nil {
		return Deadline{DueAt: at8(first.StartsAt), From: DueFromOneOnOne, MeetingEnd: first.EndsAt}
	}
	return Deadline{DueAt: at8(from.AddDate(0, 0, 3)), From: DueFromNoOneOnOne, MeetingEnd: to} // Monday
}

// HandInLateness says which of a student's hand-ins of one task are late, and whether the task is. handedInAt is the
// time of each hand-in, oldest first; the first result is per hand-in, in the same order.
func (d Deadline) HandInLateness(handedInAt []time.Time) (perHandIn []bool, task bool) {
	perHandIn = make([]bool, len(handedInAt))
	for i, at := range handedInAt {
		late := at.After(d.DueAt)
		if i > 0 {
			late = late && at.Before(d.MeetingEnd)
		}
		perHandIn[i] = late
		task = task || late
	}
	return perHandIn, task
}

// deadlineStore is what working out a student's deadline needs from the store.
type deadlineStore interface {
	GetModuleByID(ctx context.Context, id int64) (*store.Module, error)
	ListBookedSlots(ctx context.Context, studentID int64, from, to time.Time) ([]store.BookedSlot, error)
}

// weekendDeadlines looks a student's booked 1:1s up once per module.
type weekendDeadlines struct {
	store     deadlineStore
	studentID int64
	cache     map[int64]*Deadline
}

func newWeekendDeadlines(s deadlineStore, studentID int64) *weekendDeadlines {
	return &weekendDeadlines{store: s, studentID: studentID, cache: map[int64]*Deadline{}}
}

// forModule is the student's deadline for the module.
func (w *weekendDeadlines) forModule(ctx context.Context, moduleID int64) (*Deadline, error) {
	if d, ok := w.cache[moduleID]; ok {
		return d, nil
	}
	m, err := w.store.GetModuleByID(ctx, moduleID)
	if err != nil {
		return nil, err
	}
	return w.forOpening(ctx, moduleID, m.OpensAt)
}

// forOpening is the same when the caller already knows when the module opens.
func (w *weekendDeadlines) forOpening(ctx context.Context, moduleID int64, opensAt time.Time) (*Deadline, error) {
	if d, ok := w.cache[moduleID]; ok {
		return d, nil
	}
	from, to := oneOnOneWeekend(opensAt)
	booked, err := w.store.ListBookedSlots(ctx, w.studentID, from, to)
	if err != nil {
		return nil, err
	}
	d := StudentDeadline(opensAt, booked)
	w.cache[moduleID] = &d
	return &d, nil
}

// personalizeDeadlines gives each assignment the student's own deadline (see StudentDeadline). TaskDueAt keeps the
// deadline stored on the task, if any (lecturers no longer set one), and DueFrom says where the deadline comes from.
func (s *ContentService) personalizeDeadlines(ctx context.Context, studentID int64, items []store.ContentItem) error {
	deadlines := newWeekendDeadlines(s.store, studentID)
	for i := range items {
		ci := &items[i]
		if ci.Type != "assignment" {
			continue
		}
		d, err := deadlines.forModule(ctx, ci.ModuleID)
		if err != nil {
			return err
		}
		due := d.DueAt
		if ci.DueAt != nil {
			task := *ci.DueAt
			ci.TaskDueAt = &task
		}
		ci.DueAt, ci.DueFrom = &due, d.From
	}
	return nil
}
