package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"xsolla-capstone-api/internal/store"
)

// ooAt is a school-time moment in November 2026 (Monday the 16th, Friday the 20th, Monday the 23rd).
func ooAt(day, hour, min int) time.Time {
	return time.Date(2026, time.November, day, hour, min, 0, 0, SchoolLocation)
}

// slot is a 30-minute booked 1:1.
func slot(at time.Time) store.BookedSlot {
	return store.BookedSlot{StartsAt: at, EndsAt: at.Add(30 * time.Minute)}
}

// the module of these tests opens on Monday the 16th: its 1:1 weekend is Friday the 20th to Monday the 23rd
var ooOpens = ooAt(16, 0, 0)

func TestStudentDeadline(t *testing.T) {
	cases := []struct {
		name    string
		opens   time.Time
		booked  []store.BookedSlot
		want    time.Time
		wantWhy string
	}{
		{"1:1 on Friday 12:30 -> Friday 08:00", ooOpens, []store.BookedSlot{slot(ooAt(20, 12, 30))}, ooAt(20, 8, 0), DueFromOneOnOne},
		{"1:1 on Monday 14:30 -> Monday 08:00", ooOpens, []store.BookedSlot{slot(ooAt(23, 14, 30))}, ooAt(23, 8, 0), DueFromOneOnOne},
		{"no 1:1 yet -> Monday 08:00", ooOpens, nil, ooAt(23, 8, 0), DueFromNoOneOnOne},
		{"two 1:1s -> the earlier day", ooOpens, []store.BookedSlot{slot(ooAt(23, 12, 0)), slot(ooAt(20, 10, 0))}, ooAt(20, 8, 0), DueFromOneOnOne},
		{"a 1:1 on Saturday -> Saturday 08:00", ooOpens, []store.BookedSlot{slot(ooAt(21, 11, 0))}, ooAt(21, 8, 0), DueFromOneOnOne},
		{"1:1s of other weeks do not count", ooOpens, []store.BookedSlot{slot(ooAt(13, 12, 0)), slot(ooAt(24, 9, 0)), slot(ooAt(27, 12, 0))}, ooAt(23, 8, 0), DueFromNoOneOnOne},
		{"Friday 01:00 school time is still Friday (Thursday in UTC)", ooOpens, []store.BookedSlot{slot(ooAt(20, 1, 0).UTC())}, ooAt(20, 8, 0), DueFromOneOnOne},
		{"a module that opens on the Friday has that Friday's weekend", ooAt(20, 0, 0), nil, ooAt(23, 8, 0), DueFromNoOneOnOne},
		{"a module that opens on Saturday has the next Friday's weekend", ooAt(21, 9, 0), []store.BookedSlot{slot(ooAt(27, 12, 0))}, ooAt(27, 8, 0), DueFromOneOnOne},
		{"a module that opens on Thursday evening (school time) has the next day's weekend", ooAt(19, 23, 0), nil, ooAt(23, 8, 0), DueFromNoOneOnOne},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := StudentDeadline(c.opens, c.booked)
			if !got.DueAt.Equal(c.want) || got.From != c.wantWhy {
				t.Errorf("got %v (%q), want %v (%q)", got.DueAt.In(SchoolLocation), got.From, c.want, c.wantWhy)
			}
		})
	}
}

func TestMeetingEnd(t *testing.T) {
	booked := StudentDeadline(ooOpens, []store.BookedSlot{slot(ooAt(20, 12, 0))})
	if !booked.MeetingEnd.Equal(ooAt(20, 12, 30)) {
		t.Errorf("with a 1:1 the meeting ends at its end: %v", booked.MeetingEnd.In(SchoolLocation))
	}
	none := StudentDeadline(ooOpens, nil)
	if !none.MeetingEnd.Equal(ooAt(24, 0, 0)) {
		t.Errorf("with no 1:1 the meeting ends with the weekend (Tuesday 00:00): %v", none.MeetingEnd.In(SchoolLocation))
	}
}

func TestHandInLateness(t *testing.T) {
	// a Friday 12:00 1:1: due Friday 08:00, over at 12:30
	d := StudentDeadline(ooOpens, []store.BookedSlot{slot(ooAt(20, 12, 0))})
	cases := []struct {
		name     string
		handIns  []time.Time
		perHand  []bool
		taskLate bool
	}{
		{"nothing handed in", nil, []bool{}, false},
		{"on time", []time.Time{ooAt(20, 7, 59)}, []bool{false}, false},
		{"exactly at 08:00 is on time", []time.Time{ooAt(20, 8, 0)}, []bool{false}, false},
		{"first hand-in after 08:00", []time.Time{ooAt(20, 8, 1)}, []bool{true}, true},
		{"first hand-in late, even after the 1:1", []time.Time{ooAt(22, 10, 0)}, []bool{true}, true},
		{"on time, then a change after 08:00 before the 1:1 ends: late", []time.Time{ooAt(19, 20, 0), ooAt(20, 10, 0)}, []bool{false, true}, true},
		{"on time, then a change after the 1:1 (after the feedback): not late", []time.Time{ooAt(19, 20, 0), ooAt(20, 13, 0)}, []bool{false, false}, false},
		{"on time, then a change the next day: not late", []time.Time{ooAt(19, 20, 0), ooAt(21, 10, 0)}, []bool{false, false}, false},
		{"on time, a late change, then another after the 1:1: the task stays late", []time.Time{ooAt(19, 20, 0), ooAt(20, 9, 0), ooAt(21, 10, 0)}, []bool{false, true, false}, true},
		{"two changes before the deadline: not late", []time.Time{ooAt(18, 10, 0), ooAt(19, 10, 0)}, []bool{false, false}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			per, task := d.HandInLateness(c.handIns)
			if task != c.taskLate || len(per) != len(c.perHand) {
				t.Fatalf("task late %v (per hand-in %v), want %v (%v)", task, per, c.taskLate, c.perHand)
			}
			for i := range per {
				if per[i] != c.perHand[i] {
					t.Errorf("hand-in %d: late %v, want %v", i+1, per[i], c.perHand[i])
				}
			}
		})
	}

	// with no 1:1 booked the weekend runs to Tuesday 00:00: a change on Monday after 08:00 is late, on Tuesday it is not
	none := StudentDeadline(ooOpens, nil)
	if _, late := none.HandInLateness([]time.Time{ooAt(21, 10, 0), ooAt(23, 15, 0)}); !late {
		t.Error("a Monday afternoon change with no 1:1 booked should be late")
	}
	if _, late := none.HandInLateness([]time.Time{ooAt(21, 10, 0), ooAt(24, 10, 0)}); late {
		t.Error("a Tuesday change with no 1:1 booked should not be late")
	}
}

// ooStore is the part of the store the content reads use.
type ooStore struct {
	store.Store
	items    []store.ContentItem
	opens    time.Time
	booked   []store.BookedSlot
	calls    int
	from, to time.Time
	enrolled bool
	teacher  bool
}

func (f *ooStore) GetContentItemsForModule(_ context.Context, _ int64, _ bool) ([]store.ContentItem, error) {
	out := make([]store.ContentItem, len(f.items))
	copy(out, f.items)
	return out, nil
}

func (f *ooStore) GetContentItemByID(_ context.Context, id int64) (*store.ContentItem, error) {
	for _, ci := range f.items {
		if ci.ID == id {
			c := ci
			return &c, nil
		}
	}
	return nil, store.ErrContentItemNotFound
}

func (f *ooStore) GetModuleByID(_ context.Context, id int64) (*store.Module, error) {
	return &store.Module{ID: id, OpensAt: f.opens}, nil
}

func (f *ooStore) IsUserEnrolled(context.Context, int64, int64) (bool, error) { return f.enrolled, nil }
func (f *ooStore) IsUserTeacherOfModule(context.Context, int64, int64) (bool, error) {
	return f.teacher, nil
}

func (f *ooStore) ListBookedSlots(_ context.Context, _ int64, from, to time.Time) ([]store.BookedSlot, error) {
	f.calls++
	f.from, f.to = from, to
	return f.booked, nil
}

func ooItems() []store.ContentItem {
	old := ooAt(18, 23, 59) // a deadline an old task still carries
	return []store.ContentItem{
		{ID: 1, ModuleID: 4, Type: "assignment", IsVisible: true},
		{ID: 2, ModuleID: 4, Type: "assignment", IsVisible: true, DueAt: &old},
		{ID: 3, ModuleID: 4, Type: "file", IsVisible: true},
	}
}

func TestStudentsGetTheirOwnDeadlineOnEveryAssignment(t *testing.T) {
	st := &ooStore{items: ooItems(), opens: ooOpens, booked: []store.BookedSlot{slot(ooAt(23, 14, 30))}, enrolled: true}
	items, err := NewContentService(st).GetContentItemsForModule(context.Background(), 31, []string{"student"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if st.calls != 1 || !st.from.Equal(ooAt(20, 0, 0)) || !st.to.Equal(ooAt(24, 0, 0)) {
		t.Fatalf("looked up 1:1s %d time(s) in [%v, %v), want once for Friday to Tuesday", st.calls, st.from, st.to)
	}
	for _, i := range []int{0, 1} {
		a := items[i]
		if a.DueAt == nil || !a.DueAt.Equal(ooAt(23, 8, 0)) || a.DueFrom != DueFromOneOnOne {
			t.Errorf("task %d: due %v from %q; want Monday 08:00 after a Monday 1:1", a.ID, a.DueAt, a.DueFrom)
		}
	}
	if items[0].TaskDueAt != nil {
		t.Errorf("a task with no deadline of its own has a task_due_at: %v", items[0].TaskDueAt)
	}
	if items[1].TaskDueAt == nil || !items[1].TaskDueAt.Equal(ooAt(18, 23, 59)) {
		t.Errorf("the stored deadline is not kept as task_due_at: %v", items[1].TaskDueAt)
	}
	if file := items[2]; file.DueAt != nil || file.DueFrom != "" {
		t.Errorf("a file got a deadline: %+v", file)
	}
	if st.items[1].DueAt == nil || !st.items[1].DueAt.Equal(ooAt(18, 23, 59)) {
		t.Error("the store's own row was changed")
	}

	one, err := NewContentService(st).GetContentItemByID(context.Background(), 31, []string{"student"}, 1)
	if err != nil || one.DueAt == nil || !one.DueAt.Equal(ooAt(23, 8, 0)) || one.DueFrom != DueFromOneOnOne {
		t.Errorf("single item: %+v, %v", one, err)
	}
}

func TestStaffSeeNoStudentDeadline(t *testing.T) {
	for _, roles := range [][]string{{"admin"}, {"mentor"}, {"lecturer"}} {
		st := &ooStore{items: ooItems(), opens: ooOpens, booked: []store.BookedSlot{slot(ooAt(23, 14, 30))}}
		items, err := NewContentService(st).GetContentItemsForModule(context.Background(), 5, roles, 4)
		if err != nil {
			t.Fatal(err)
		}
		if st.calls != 0 || items[0].DueAt != nil || items[0].DueFrom != "" {
			t.Errorf("%v: due %v from %q after %d lookups, want no student deadline", roles, items[0].DueAt, items[0].DueFrom, st.calls)
		}
	}
}

func TestNoLookupWithoutAnAssignment(t *testing.T) {
	st := &ooStore{items: []store.ContentItem{{ID: 3, ModuleID: 4, Type: "file", IsVisible: true}}, opens: ooOpens, enrolled: true}
	if _, err := NewContentService(st).GetContentItemsForModule(context.Background(), 31, []string{"student"}, 4); err != nil || st.calls != 0 {
		t.Errorf("err %v, %d lookups; want none", err, st.calls)
	}
}

// ---------- hand-ins ----------

// subStore is the part of the store that hand-ins use.
type subStore struct {
	store.Store
	opens     time.Time
	booked    []store.BookedSlot
	existing  []store.Submission // the student's earlier hand-ins, newest first
	created   store.Submission
	redModule int64
	redCalls  int
}

func (f *subStore) GetContentItemByID(context.Context, int64) (*store.ContentItem, error) {
	return &store.ContentItem{ID: 9, ModuleID: 4, Type: "assignment", IsVisible: true}, nil
}
func (f *subStore) IsUserEnrolled(context.Context, int64, int64) (bool, error) { return true, nil }
func (f *subStore) GetModuleByID(_ context.Context, id int64) (*store.Module, error) {
	return &store.Module{ID: id, OpensAt: f.opens}, nil
}
func (f *subStore) ListBookedSlots(context.Context, int64, time.Time, time.Time) ([]store.BookedSlot, error) {
	return f.booked, nil
}
func (f *subStore) CreateSubmission(_ context.Context, itemID, studentID int64, text string) (*store.Submission, error) {
	s := f.created
	s.ContentItemID, s.StudentID, s.TextBody = itemID, studentID, text
	return &s, nil
}
func (f *subStore) GetSubmissionFiles(context.Context, int64) ([]store.File, error) { return nil, nil }
func (f *subStore) GetSubmissionsByStudentAndAssignment(context.Context, int64, int64) ([]store.Submission, error) {
	return append([]store.Submission{f.created}, f.existing...), nil
}
func (f *subStore) MarkEnrollmentRed(_ context.Context, moduleID, _ int64) error {
	f.redModule = moduleID
	f.redCalls++
	return nil
}

func newSubStore(opens time.Time, at time.Time, earlier ...time.Time) *subStore {
	st := &subStore{opens: opens, created: store.Submission{ID: 100, SubmittedAt: at}}
	for i, e := range earlier { // earlier hand-ins, newest first
		st.existing = append(st.existing, store.Submission{ID: int64(50 - i), SubmittedAt: e})
	}
	return st
}

func TestALateHandInTurnsTheWeekRed(t *testing.T) {
	// no 1:1 booked: due Monday 08:00. The service reads the real clock for "open", so use a module that opened long ago.
	opens := time.Now().AddDate(-1, 0, 0)
	from, _ := oneOnOneWeekend(opens)
	due := at8(from.AddDate(0, 0, 3))

	t.Run("late first hand-in", func(t *testing.T) {
		st := newSubStore(opens, due.Add(time.Hour))
		sub, err := NewSubmissionService(st).CreateSubmission(context.Background(), 31, 4, 9, "work", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !sub.Late || sub.DueAt == nil || !sub.DueAt.Equal(due) || st.redCalls != 1 || st.redModule != 4 {
			t.Errorf("late %v, due %v, red calls %d (module %d); want a late hand-in that turns week 4 red", sub.Late, sub.DueAt, st.redCalls, st.redModule)
		}
	})
	t.Run("on time", func(t *testing.T) {
		st := newSubStore(opens, due.Add(-time.Hour))
		sub, err := NewSubmissionService(st).CreateSubmission(context.Background(), 31, 4, 9, "work", nil)
		if err != nil || sub.Late || st.redCalls != 0 {
			t.Errorf("err %v, late %v, red calls %d; want on time and no colour change", err, sub.Late, st.redCalls)
		}
	})
	t.Run("a change after the deadline, before the 1:1 is over, is late", func(t *testing.T) {
		st := newSubStore(opens, due.Add(time.Hour), due.Add(-24*time.Hour))
		sub, err := NewSubmissionService(st).CreateSubmission(context.Background(), 31, 4, 9, "work", nil)
		if err != nil || !sub.Late || st.redCalls != 1 {
			t.Errorf("err %v, late %v, red calls %d; want late and red", err, sub.Late, st.redCalls)
		}
	})
	t.Run("a change after the weekend is not late", func(t *testing.T) {
		st := newSubStore(opens, due.Add(48*time.Hour), due.Add(-24*time.Hour))
		sub, err := NewSubmissionService(st).CreateSubmission(context.Background(), 31, 4, 9, "work", nil)
		if err != nil || sub.Late || st.redCalls != 0 {
			t.Errorf("err %v, late %v, red calls %d; want not late", err, sub.Late, st.redCalls)
		}
	})
	t.Run("a change after a late first hand-in does not turn the week red again", func(t *testing.T) {
		// the mentor may have set the week back to yellow; only a new late hand-in turns it red
		st := newSubStore(opens, due.Add(72*time.Hour), due.Add(time.Hour))
		sub, err := NewSubmissionService(st).CreateSubmission(context.Background(), 31, 4, 9, "work", nil)
		if err != nil || !sub.Late || st.redCalls != 0 {
			t.Errorf("err %v, late %v, red calls %d; want the task still late but no new red", err, sub.Late, st.redCalls)
		}
	})
}

func TestNoHandInBeforeTheModuleOpens(t *testing.T) {
	st := newSubStore(time.Now().Add(48*time.Hour), time.Now())
	_, err := NewSubmissionService(st).CreateSubmission(context.Background(), 31, 4, 9, "work", nil)
	if !errors.Is(err, ErrModuleNotOpen) {
		t.Errorf("err = %v, want ErrModuleNotOpen", err)
	}
}

func TestSlotsOnlyOnFridayAndMonday(t *testing.T) {
	// two weeks ahead so the "already passed" rule does not apply
	base := time.Now().In(SchoolLocation).AddDate(0, 0, 14)
	next := func(wd time.Weekday) time.Time {
		d := base
		for d.Weekday() != wd {
			d = d.AddDate(0, 0, 1)
		}
		return time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, SchoolLocation)
	}
	for _, wd := range []time.Weekday{time.Friday, time.Monday} {
		start := next(wd)
		if _, _, err := parseWindow(start, start.Add(2*time.Hour)); err != nil {
			t.Errorf("%v 16:00-18:00 refused: %v", wd, err)
		}
	}
	for _, wd := range []time.Weekday{time.Tuesday, time.Wednesday, time.Thursday, time.Saturday, time.Sunday} {
		start := next(wd)
		if _, _, err := parseWindow(start, start.Add(2*time.Hour)); err == nil {
			t.Errorf("%v accepted, want only Friday and Monday", wd)
		}
	}
	fri := next(time.Friday)
	if _, _, err := parseWindow(fri.Add(7*time.Hour), fri.Add(10*time.Hour)); err == nil { // 23:00 to 02:00
		t.Error("a window that runs past midnight was accepted")
	}
	// the day is the school's (Baku): Friday 01:00 in Baku is still Thursday in UTC
	early := time.Date(fri.Year(), fri.Month(), fri.Day(), 1, 0, 0, 0, SchoolLocation).UTC()
	if _, _, err := parseWindow(early, early.Add(time.Hour)); err != nil {
		t.Errorf("Friday 01:00 Baku time refused: %v", err)
	}
}
