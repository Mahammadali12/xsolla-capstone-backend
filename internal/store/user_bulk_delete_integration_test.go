package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// bdUser inserts a user with the given roles (none for nil).
func bdUser(t *testing.T, st *StoreImpl, label string, roles ...string) int64 {
	t.Helper()
	id := insertTestUser(t, st.db, fmt.Sprintf("bd-%s-%d@example.com", label, time.Now().UnixNano()), "BD "+label)
	for _, r := range roles {
		if _, err := st.db.Exec(`INSERT INTO user_roles (user_id, role) VALUES ($1, $2)`, id, r); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func bdExists(t *testing.T, st *StoreImpl, id int64) bool {
	t.Helper()
	var n int
	if err := st.db.QueryRow(`SELECT count(*) FROM users WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func bdOne(t *testing.T, st *StoreImpl, query string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := st.db.QueryRow(query, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return id
}

// Every other user in the scratch schema is removed by these tests too, so they run only against TEST_DATABASE_URL.
func TestDeleteNonAdminUsers(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	ctx := context.Background()

	actor := bdUser(t, st, "actor", "admin")
	otherAdmin := bdUser(t, st, "admin-mentor", "admin", "mentor")
	mentor := bdUser(t, st, "mentor", "mentor")
	lecturer := bdUser(t, st, "lecturer", "lecturer")
	student := bdUser(t, st, "student", "student")
	twoRoles := bdUser(t, st, "mentor-lecturer", "mentor", "lecturer")
	noRole := bdUser(t, st, "none")

	// course content made by the people who will be deleted
	module := bdOne(t, st, `INSERT INTO modules (title, description, created_by, opens_at, closes_at)
		VALUES ('BD week', '', $1, now(), now() + interval '7 days') RETURNING id`, lecturer)
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM modules WHERE id = $1`, module) })
	file := bdOne(t, st, `INSERT INTO files (uploaded_by, original_name, storage_key, mime_type, size_bytes)
		VALUES ($1, 'slides.pdf', 'bd/slides.pdf', 'application/pdf', 10) RETURNING id`, lecturer)
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM files WHERE id = $1`, file) })
	task := bdOne(t, st, `INSERT INTO content_items (module_id, type, title, created_by) VALUES ($1, 'assignment', 'BD task', $2) RETURNING id`, module, lecturer)
	quiz := bdOne(t, st, `INSERT INTO quizzes (module_id, title, created_by) VALUES ($1, 'BD quiz', $2) RETURNING id`, module, lecturer)
	sub := bdOne(t, st, `INSERT INTO submissions (content_item_id, student_id, graded_by) VALUES ($1, $2, $3) RETURNING id`, task, student, mentor)
	slot := bdOne(t, st, `INSERT INTO mentor_slots (mentor_id, starts_at, ends_at, booked_by)
		VALUES ($1, now() + interval '2 days', now() + interval '2 days 30 minutes', $2) RETURNING id`, mentor, student)
	adminSlot := bdOne(t, st, `INSERT INTO mentor_slots (mentor_id, starts_at, ends_at, booked_by)
		VALUES ($1, now() + interval '3 days', now() + interval '3 days 30 minutes', $2) RETURNING id`, otherAdmin, student)
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM mentor_slots WHERE id = ANY(ARRAY[$1, $2]::bigint[])`, slot, adminSlot)
	})

	// 1. only the students
	n, byRole, err := st.DeleteNonAdminUsers(ctx, []string{"student"}, actor, actor)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 || byRole["student"] != n || bdExists(t, st, student) || !bdExists(t, st, mentor) || !bdExists(t, st, lecturer) {
		t.Fatalf("students only: %d deleted, %v; student left %v, mentor %v", n, byRole, bdExists(t, st, student), bdExists(t, st, mentor))
	}
	var subs int
	_ = database.QueryRow(`SELECT count(*) FROM submissions WHERE id = $1`, sub).Scan(&subs)
	var bookedBy *int64
	_ = database.QueryRow(`SELECT booked_by FROM mentor_slots WHERE id = $1`, adminSlot).Scan(&bookedBy)
	if subs != 0 || bookedBy != nil {
		t.Errorf("the student's hand-in (%d left) and booking (%v) must go with them", subs, bookedBy)
	}

	// 2. everyone who is not an admin
	n, byRole, err = st.DeleteNonAdminUsers(ctx, nil, actor, actor)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{mentor, lecturer, twoRoles, noRole} {
		if bdExists(t, st, id) {
			t.Errorf("user %d is still there", id)
		}
	}
	if !bdExists(t, st, actor) || !bdExists(t, st, otherAdmin) {
		t.Fatal("an admin was deleted")
	}
	if n < 4 || byRole["mentor"] < 2 || byRole["lecturer"] < 2 || byRole["none"] < 1 || byRole["admin"] != 0 {
		t.Errorf("deleted %d, by role %v", n, byRole)
	}

	// the course content stays, handed to the acting admin
	var moduleOwner, fileOwner, quizOwner int64
	var taskOwner *int64
	_ = database.QueryRow(`SELECT created_by FROM modules WHERE id = $1`, module).Scan(&moduleOwner)
	_ = database.QueryRow(`SELECT uploaded_by FROM files WHERE id = $1`, file).Scan(&fileOwner)
	_ = database.QueryRow(`SELECT created_by FROM quizzes WHERE id = $1`, quiz).Scan(&quizOwner)
	if err := database.QueryRow(`SELECT created_by FROM content_items WHERE id = $1`, task).Scan(&taskOwner); err != nil {
		t.Fatalf("the task is gone: %v", err)
	}
	if moduleOwner != actor || fileOwner != actor || quizOwner != actor || taskOwner != nil {
		t.Errorf("module %d, file %d, quiz %d, task %v; want the actor %d (task: no owner)", moduleOwner, fileOwner, quizOwner, taskOwner, actor)
	}
	var slots int
	_ = database.QueryRow(`SELECT count(*) FROM mentor_slots WHERE id = $1`, slot).Scan(&slots)
	if slots != 0 {
		t.Error("the deleted mentor's 1:1 slot is still there")
	}

	// 3. nothing left to delete
	if n, _, err := st.DeleteNonAdminUsers(ctx, nil, actor, actor); err != nil || n != 0 {
		t.Errorf("second run: %d, %v", n, err)
	}
}

// HardDeleteUser used to fail for a lecturer who had made a quiz (quizzes.created_by has no ON DELETE).
func TestHardDeleteUserWithAQuiz(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	admin := bdUser(t, st, "quiz-admin", "admin")
	lecturer := bdUser(t, st, "quiz-lecturer", "lecturer")
	module := bdOne(t, st, `INSERT INTO modules (title, description, created_by, opens_at, closes_at)
		VALUES ('BD quiz week', '', $1, now(), now() + interval '7 days') RETURNING id`, admin)
	t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM modules WHERE id = $1`, module) })
	quiz := bdOne(t, st, `INSERT INTO quizzes (module_id, title, created_by) VALUES ($1, 'BD quiz', $2) RETURNING id`, module, lecturer)

	if err := st.HardDeleteUser(context.Background(), lecturer, admin); err != nil {
		t.Fatalf("delete a lecturer with a quiz: %v", err)
	}
	var owner int64
	_ = database.QueryRow(`SELECT created_by FROM quizzes WHERE id = $1`, quiz).Scan(&owner)
	if owner != admin || bdExists(t, st, lecturer) {
		t.Errorf("quiz owner %d (want %d), lecturer still there: %v", owner, admin, bdExists(t, st, lecturer))
	}
}
