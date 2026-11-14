package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func lfdbExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}

func lfdbID(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(query, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return id
}

func TestLectureFeedbackStore(t *testing.T) {
	db := testDatabase(t)
	st := NewStore(db)
	ctx := context.Background()
	now := time.Now()
	tag := now.UnixNano()

	person := func(name string, roles ...string) int64 {
		id := insertTestUser(t, db, fmt.Sprintf("lf-%s-%d@example.com", name, tag), name)
		for _, r := range roles {
			lfdbExec(t, db, `INSERT INTO user_roles (user_id, role) VALUES ($1, $2)`, id, r)
		}
		return id
	}
	adminID := person("Admin", "admin")
	lecturer := person("Leyla", "lecturer")
	s1, s2 := person("S1", "student"), person("S2", "student")
	notEnrolled := person("S3", "student")
	staff := person("S4", "student", "mentor")
	left := person("S5", "student")
	lfdbExec(t, db, `UPDATE users SET left_at = NOW() WHERE id = $1`, left)

	week := lfdbID(t, db, `INSERT INTO modules (title, description, created_by, opens_at, closes_at) VALUES ('LF week', '', $1, $2, $3) RETURNING id`,
		adminID, now.Add(-7*24*time.Hour), now.Add(7*24*time.Hour))
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM modules WHERE id = $1`, week) })
	for _, u := range []int64{lecturer, s1, s2, staff, left} {
		lfdbExec(t, db, `INSERT INTO enrollments (module_id, user_id) VALUES ($1, $2)`, week, u)
	}
	var events []int64
	event := func(title, kind string, module *int64, ends time.Time, deleted bool) int64 {
		id := lfdbID(t, db, `INSERT INTO events (title, kind, starts_at, ends_at, module_id, deleted_at)
			VALUES ($1, $2, $3, $4, $5, CASE WHEN $6 THEN NOW() END) RETURNING id`, title, kind, ends.Add(-time.Hour), ends, module, deleted)
		events = append(events, id)
		return id
	}
	t.Cleanup(func() {
		for _, id := range events {
			_, _ = db.Exec(`DELETE FROM events WHERE id = $1`, id)
		}
	})
	lectureA := event("Goroutines", "lecture", &week, now.Add(-time.Hour), false)    // open
	lectureB := event("Open talk", "lecture", nil, now.Add(time.Hour), false)        // not open yet, no week
	meeting := event("Stand-up", "meeting", &week, now.Add(-time.Hour), false)       // not a lecture
	old := event("Old lecture", "lecture", &week, now.Add(-8*24*time.Hour), false)   // closed
	deleted := event("Deleted lecture", "lecture", &week, now.Add(-time.Hour), true) // deleted

	l, err := st.GetLecture(ctx, lectureA)
	if err != nil || l.Title != "Goroutines" || l.ModuleID == nil || *l.ModuleID != week || l.ModuleTitle == nil || *l.ModuleTitle != "LF week" {
		t.Fatalf("GetLecture: %+v, %v", l, err)
	}
	for name, id := range map[string]int64{"meeting": meeting, "deleted": deleted, "unknown": 999999999} {
		if _, err := st.GetLecture(ctx, id); !errors.Is(err, ErrLectureNotFound) {
			t.Errorf("GetLecture %s: %v, want ErrLectureNotFound", name, err)
		}
	}

	improve := "slower please"
	form := LectureFeedbackInput{EventID: lectureA, StudentID: s1, Overall: 4, Clarity: 5, Practice: 3, Confidence: 2, Pace: "too_fast", Improve: &improve}
	sent, err := st.CreateLectureFeedback(ctx, form)
	if err != nil || sent.EventID != lectureA || sent.SubmittedAt.IsZero() {
		t.Fatalf("create: %+v, %v", sent, err)
	}
	if _, err := st.CreateLectureFeedback(ctx, form); !errors.Is(err, ErrLectureFeedbackExists) {
		t.Errorf("second form: %v, want ErrLectureFeedbackExists", err)
	}
	if ok, _ := st.HasLectureFeedback(ctx, lectureA, s1); !ok {
		t.Error("HasLectureFeedback = false after sending")
	}
	bad := form
	bad.StudentID, bad.Overall = s2, 6
	if _, err := st.CreateLectureFeedback(ctx, bad); err == nil {
		t.Error("the table accepted overall 6")
	}
	if got, _ := st.ListSentLectureFeedback(ctx, s1); len(got) != 1 || got[0].EventID != lectureA {
		t.Errorf("sent: %+v", got)
	}
	if got, _ := st.ListSentLectureFeedback(ctx, s2); got == nil || len(got) != 0 {
		t.Errorf("sent by nobody: %v, want an empty list", got)
	}

	pending := func(student int64, at time.Time) []int64 {
		t.Helper()
		ls, err := st.ListPendingLectures(ctx, student, at, 10*time.Minute, 7*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, l := range ls {
			ids = append(ids, l.ID)
		}
		return ids
	}
	if got := pending(s2, now); fmt.Sprint(got) != fmt.Sprint([]int64{lectureA}) {
		t.Errorf("S2 now: %v, want only lecture A (B not open yet, old one closed)", got)
	}
	if got := pending(s1, now); len(got) != 0 {
		t.Errorf("S1 already sent A: %v", got)
	}
	if got := pending(notEnrolled, now); len(got) != 0 {
		t.Errorf("S3 is not in the week: %v", got)
	}
	nearEndOfB := now.Add(55 * time.Minute)
	if got := pending(s2, nearEndOfB); fmt.Sprint(got) != fmt.Sprint([]int64{lectureA, lectureB}) {
		t.Errorf("S2 five minutes before B ends: %v, want A and B", got)
	}
	if got := pending(notEnrolled, nearEndOfB); fmt.Sprint(got) != fmt.Sprint([]int64{lectureB}) {
		t.Errorf("S3 and a lecture without a week: %v, want B", got)
	}

	all, err := st.ListLecturesWithAudience(ctx, LectureFeedbackFilter{ModuleID: &week})
	if err != nil || len(all) != 2 || all[0].Lecture.ID != old || all[1].Lecture.ID != lectureA {
		t.Fatalf("lectures of the week: %+v, %v (meeting and deleted lecture must be left out)", all, err)
	}
	if all[1].Expected != 2 {
		t.Errorf("expected for lecture A: %d, want 2 (S1, S2; not the lecturer, staff, someone who left, or someone not enrolled)", all[1].Expected)
	}
	var students int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users u WHERE u.left_at IS NULL AND u.deleted_at IS NULL
		AND EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = u.id AND r.role = 'student')
		AND NOT EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = u.id AND r.role IN ('mentor', 'lecturer', 'admin'))`).Scan(&students); err != nil {
		t.Fatal(err)
	}
	if b, _ := st.ListLecturesWithAudience(ctx, LectureFeedbackFilter{EventID: &lectureB}); len(b) != 1 || b[0].Expected != students {
		t.Errorf("lecture without a week: %+v, want every active student (%d)", b, students)
	}

	rows, err := st.ListLectureFeedback(ctx, LectureFeedbackFilter{EventID: &lectureA})
	if err != nil || len(rows) != 1 || rows[0].StudentName != "S1" || rows[0].Pace != "too_fast" || rows[0].Liked != nil ||
		rows[0].Improve == nil || *rows[0].Improve != improve || rows[0].Lecture.ModuleTitle == nil {
		t.Errorf("list: %+v, %v", rows, err)
	}
	later := now.Add(24 * time.Hour)
	if rows, _ := st.ListLectureFeedback(ctx, LectureFeedbackFilter{From: &later}); len(rows) != 0 {
		t.Errorf("from tomorrow: %d rows", len(rows))
	}

	lecturers, err := st.ListWeekLecturers(ctx, []int64{week, week + 1000000})
	if err != nil || len(lecturers[week]) != 1 || lecturers[week][0].ID != lecturer || lecturers[week][0].Name != "Leyla" {
		t.Errorf("lecturers: %+v, %v", lecturers, err)
	}
	if none, _ := st.ListWeekLecturers(ctx, nil); none == nil || len(none) != 0 {
		t.Errorf("lecturers of no week: %v", none)
	}
}
