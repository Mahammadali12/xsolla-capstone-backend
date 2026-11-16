package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestListBookedSlotStarts(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	ctx := context.Background()
	tag := time.Now().UnixNano()
	mentor := insertTestUser(t, database, fmt.Sprintf("oo-mentor-%d@example.com", tag), "OO Mentor")
	student := insertTestUser(t, database, fmt.Sprintf("oo-student-%d@example.com", tag), "OO Student")
	other := insertTestUser(t, database, fmt.Sprintf("oo-other-%d@example.com", tag), "OO Other")

	base := time.Date(2030, 3, 1, 10, 0, 0, 0, time.UTC) // a Friday far from any real slot
	slot := func(at time.Time, bookedBy *int64, deleted bool) {
		t.Helper()
		var del any
		if deleted {
			del = time.Now()
		}
		var id int64
		if err := database.QueryRow(
			`INSERT INTO mentor_slots (mentor_id, starts_at, ends_at, booked_by, deleted_at) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			mentor, at, at.Add(30*time.Minute), bookedBy, del,
		).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM mentor_slots WHERE id = $1`, id) })
	}
	slot(base.Add(72*time.Hour), &student, false)   // Monday: counts
	slot(base, &student, false)                     // Friday: counts, listed first
	slot(base.Add(2*time.Hour), &other, false)      // someone else's
	slot(base.Add(4*time.Hour), nil, false)         // free
	slot(base.Add(24*time.Hour), &student, true)    // cancelled (deleted)
	slot(base.Add(7*24*time.Hour), &student, false) // next week: outside the window

	got, err := st.ListBookedSlotStarts(ctx, student, base.Add(-10*time.Hour), base.Add(96*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Equal(base) || !got[1].Equal(base.Add(72*time.Hour)) {
		t.Fatalf("got %v, want the Friday and Monday 1:1s in order", got)
	}
}
