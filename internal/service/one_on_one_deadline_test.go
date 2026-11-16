package service

import (
	"context"
	"testing"
	"time"

	"xsolla-capstone-api/internal/store"
)

// ooAt is a school-time moment in November 2026 (Friday the 20th, Monday the 23rd).
func ooAt(day, hour, min int) time.Time {
	return time.Date(2026, time.November, day, hour, min, 0, 0, SchoolLocation)
}

func TestOneOnOneDueAt(t *testing.T) {
	friday8 := ooAt(20, 8, 0)
	cases := []struct {
		name    string
		task    time.Time
		booked  []time.Time
		want    time.Time
		wantWhy string
	}{
		{"1:1 on Friday 12:30 -> Friday 08:00", friday8, []time.Time{ooAt(20, 12, 30)}, friday8, DueFromOneOnOne},
		{"1:1 on Monday 14:30 -> Monday 08:00", friday8, []time.Time{ooAt(23, 14, 30)}, ooAt(23, 8, 0), DueFromOneOnOne},
		{"no 1:1 yet -> Monday 08:00", friday8, nil, ooAt(23, 8, 0), DueFromNoOneOnOne},
		{"two 1:1s -> the earlier day", friday8, []time.Time{ooAt(23, 12, 0), ooAt(20, 10, 0)}, friday8, DueFromOneOnOne},
		{"a 1:1 on Saturday -> Saturday 08:00", friday8, []time.Time{ooAt(21, 11, 0)}, ooAt(21, 8, 0), DueFromOneOnOne},
		{"1:1s of other weeks do not count", friday8, []time.Time{ooAt(16, 12, 0), ooAt(24, 9, 0), ooAt(27, 12, 0)}, ooAt(23, 8, 0), DueFromNoOneOnOne},
		{"a task due on Monday follows a Friday 1:1 too", ooAt(23, 8, 0), []time.Time{ooAt(20, 15, 0)}, friday8, DueFromOneOnOne},
		{"a task due Saturday evening belongs to the same weekend", ooAt(21, 21, 34), []time.Time{ooAt(23, 12, 0)}, ooAt(23, 8, 0), DueFromOneOnOne},
		{"Friday 01:00 school time is still Friday (Thursday in UTC)", friday8, []time.Time{ooAt(20, 1, 0).UTC()}, friday8, DueFromOneOnOne},
		{"a task due on Wednesday keeps its deadline", ooAt(18, 23, 59), []time.Time{ooAt(20, 12, 0)}, ooAt(18, 23, 59), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, why := OneOnOneDueAt(c.task, c.booked)
			if !got.Equal(c.want) || why != c.wantWhy {
				t.Errorf("got %v (%q), want %v (%q)", got.In(SchoolLocation), why, c.want, c.wantWhy)
			}
		})
	}
}

// ooStore is the part of the store the content reads use.
type ooStore struct {
	store.Store
	items      []store.ContentItem
	booked     []time.Time
	calls      int
	from, to   time.Time
	enrolled   bool
	teacher    bool
	listedItem *store.ContentItem
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

func (f *ooStore) IsUserEnrolled(context.Context, int64, int64) (bool, error) { return f.enrolled, nil }
func (f *ooStore) IsUserTeacherOfModule(context.Context, int64, int64) (bool, error) {
	return f.teacher, nil
}

func (f *ooStore) ListBookedSlotStarts(_ context.Context, _ int64, from, to time.Time) ([]time.Time, error) {
	f.calls++
	f.from, f.to = from, to
	return f.booked, nil
}

func ooItems() []store.ContentItem {
	fri, wed := ooAt(20, 8, 0), ooAt(18, 23, 59)
	return []store.ContentItem{
		{ID: 1, Type: "assignment", IsVisible: true, DueAt: &fri},
		{ID: 2, Type: "assignment", IsVisible: true, DueAt: &wed},
		{ID: 3, Type: "file", IsVisible: true},
	}
}

func TestStudentsGetTheirOwnDeadline(t *testing.T) {
	st := &ooStore{items: ooItems(), booked: []time.Time{ooAt(23, 14, 30)}, enrolled: true}
	items, err := NewContentService(st).GetContentItemsForModule(context.Background(), 31, []string{"student"}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if st.calls != 1 || !st.from.Equal(ooAt(20, 0, 0)) || !st.to.Equal(ooAt(24, 0, 0)) {
		t.Fatalf("looked up 1:1s %d time(s) in [%v, %v), want once for Friday to Tuesday", st.calls, st.from, st.to)
	}
	fri := items[0]
	if !fri.DueAt.Equal(ooAt(23, 8, 0)) || !fri.TaskDueAt.Equal(ooAt(20, 8, 0)) || fri.DueFrom != DueFromOneOnOne {
		t.Errorf("Friday task: due %v, task %v, from %q; want Monday 08:00 after a Monday 1:1", fri.DueAt, fri.TaskDueAt, fri.DueFrom)
	}
	if wed := items[1]; !wed.DueAt.Equal(ooAt(18, 23, 59)) || wed.TaskDueAt != nil || wed.DueFrom != "" {
		t.Errorf("Wednesday task changed: %+v", wed)
	}
	if file := items[2]; file.DueAt != nil || file.DueFrom != "" {
		t.Errorf("a file got a deadline: %+v", file)
	}
	if !st.items[0].DueAt.Equal(ooAt(20, 8, 0)) {
		t.Error("the store's own row was changed")
	}

	one, err := NewContentService(st).GetContentItemByID(context.Background(), 31, []string{"student"}, 1)
	if err != nil || !one.DueAt.Equal(ooAt(23, 8, 0)) || one.DueFrom != DueFromOneOnOne {
		t.Errorf("single item: %+v, %v", one, err)
	}
}

func TestStaffSeeTheLecturersDeadline(t *testing.T) {
	for _, roles := range [][]string{{"admin"}, {"mentor"}, {"lecturer"}} {
		st := &ooStore{items: ooItems(), booked: []time.Time{ooAt(23, 14, 30)}}
		items, err := NewContentService(st).GetContentItemsForModule(context.Background(), 5, roles, 4)
		if err != nil {
			t.Fatal(err)
		}
		if st.calls != 0 || !items[0].DueAt.Equal(ooAt(20, 8, 0)) || items[0].DueFrom != "" {
			t.Errorf("%v: due %v from %q after %d lookups, want the task's own deadline", roles, items[0].DueAt, items[0].DueFrom, st.calls)
		}
	}
	// the module's teacher who is not enrolled also sees the task's deadline
	st := &ooStore{items: ooItems(), teacher: true}
	items, _ := NewContentService(st).GetContentItemsForModule(context.Background(), 5, []string{"student"}, 4)
	if st.calls != 0 || items[0].DueFrom != "" {
		t.Errorf("teacher: %d lookups, from %q", st.calls, items[0].DueFrom)
	}
}

func TestNoLookupWithoutAWeekendTask(t *testing.T) {
	wed := ooAt(18, 23, 59)
	st := &ooStore{items: []store.ContentItem{{ID: 2, Type: "assignment", IsVisible: true, DueAt: &wed}}, enrolled: true}
	if _, err := NewContentService(st).GetContentItemsForModule(context.Background(), 31, []string{"student"}, 4); err != nil || st.calls != 0 {
		t.Errorf("err %v, %d lookups; want none", err, st.calls)
	}
}
