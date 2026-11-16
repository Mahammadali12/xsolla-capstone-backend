package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEnrollmentGradeFeedbackIsStored(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	ctx := context.Background()
	tag := time.Now().UnixNano()
	mentor := insertTestUser(t, database, fmt.Sprintf("wgf-mentor-%d@example.com", tag), "WGF Mentor")
	student := insertTestUser(t, database, fmt.Sprintf("wgf-student-%d@example.com", tag), "WGF Student")

	var moduleID int64
	if err := database.QueryRow(
		`INSERT INTO modules (title, description, created_by, opens_at, closes_at)
		 VALUES ('WGF week', '', $1, NOW() - INTERVAL '7 days', NOW()) RETURNING id`, mentor,
	).Scan(&moduleID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(`DELETE FROM outbox_events WHERE payload->>'module_id' = $1`, fmt.Sprint(moduleID))
		_, _ = database.Exec(`DELETE FROM modules WHERE id = $1`, moduleID)
	})
	if _, err := database.Exec(`INSERT INTO enrollments (module_id, user_id) VALUES ($1, $2)`, moduleID, student); err != nil {
		t.Fatal(err)
	}

	list, err := st.GetEnrollmentsForModule(ctx, moduleID)
	if err != nil || len(list) != 1 || list[0].GradeFeedback != nil || list[0].GradedAt != nil {
		t.Fatalf("an ungraded week: %+v, %v (want no feedback and no graded_at)", list, err)
	}

	yellow, green := "yellow", "green"
	e, err := st.UpdateEnrollmentGrade(ctx, moduleID, student, &yellow, "Routing app was late.", mentor)
	if err != nil {
		t.Fatal(err)
	}
	if e.GradeFeedback == nil || *e.GradeFeedback != "Routing app was late." || e.GradedAt == nil || time.Since(*e.GradedAt) > time.Minute {
		t.Errorf("returned enrollment: feedback %v, graded_at %v", e.GradeFeedback, e.GradedAt)
	}

	if _, err := st.UpdateEnrollmentGrade(ctx, moduleID, student, &green, "Caught up, well done.", mentor); err != nil {
		t.Fatal(err)
	}
	list, err = st.GetEnrollmentsForModule(ctx, moduleID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if got := list[0]; *got.Grade != "green" || got.GradeFeedback == nil || *got.GradeFeedback != "Caught up, well done." || got.GradedAt == nil {
		t.Errorf("the new grade must replace the grade and the feedback: grade %v, feedback %v, graded_at %v", *got.Grade, got.GradeFeedback, got.GradedAt)
	}

	// the column itself refuses empty or over-long feedback
	for _, bad := range []string{"", strings.Repeat("a", 1001)} {
		if _, err := database.Exec(`UPDATE enrollments SET grade_feedback = $3 WHERE module_id = $1 AND user_id = $2`, moduleID, student, bad); err == nil {
			t.Errorf("the database kept feedback of %d characters", len(bad))
		}
	}
}
