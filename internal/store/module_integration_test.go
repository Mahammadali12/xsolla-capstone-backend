package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestUpdateEnrollmentGradeStoresGradedBy(t *testing.T) {
	db := testDatabase(t)
	st := NewStore(db)
	ctx := context.Background()
	tag := time.Now().UnixNano()

	mentor := insertTestUser(t, db, fmt.Sprintf("grade-mentor-%d@example.com", tag), "Grade Mentor")
	student := insertTestUser(t, db, fmt.Sprintf("grade-student-%d@example.com", tag), "Grade Student")
	moduleID := lfdbID(t, db, `INSERT INTO modules (title, description, created_by, opens_at, closes_at)
		VALUES ('Graded week', '', $1, NOW(), NOW() + INTERVAL '7 days') RETURNING id`, mentor)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM modules WHERE id = $1`, moduleID) })
	lfdbExec(t, db, `INSERT INTO enrollments (module_id, user_id) VALUES ($1, $2)`, moduleID, student)

	grade := "green"
	e, err := st.UpdateEnrollmentGrade(ctx, moduleID, student, &grade, "Solid week", mentor)
	if err != nil {
		t.Fatal(err)
	}
	if e.GradedBy == nil || *e.GradedBy != mentor {
		t.Fatalf("graded_by = %v, want %d", e.GradedBy, mentor)
	}

	var stored *int64
	if err := db.QueryRow(`SELECT graded_by FROM enrollments WHERE module_id = $1 AND user_id = $2`,
		moduleID, student).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == nil || *stored != mentor {
		t.Fatalf("stored graded_by = %v, want %d", stored, mentor)
	}
}
