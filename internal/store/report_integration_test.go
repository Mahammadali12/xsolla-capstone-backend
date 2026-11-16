package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestReportsLifecycle(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	ctx := context.Background()
	tag := time.Now().UnixNano()
	mentor := insertTestUser(t, database, fmt.Sprintf("rpt-mentor-%d@example.com", tag), "RPT Mentor")
	student := insertTestUser(t, database, fmt.Sprintf("rpt-student-%d@example.com", tag), "RPT Student")
	admin := insertTestUser(t, database, fmt.Sprintf("rpt-admin-%d@example.com", tag), "RPT Admin")
	for id, role := range map[int64]string{mentor: "mentor", student: "student", admin: "admin"} {
		if _, err := database.Exec(`INSERT INTO user_roles (user_id, role) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, role); err != nil {
			t.Fatal(err)
		}
	}
	var ids []int64
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = database.Exec(`DELETE FROM reports WHERE id = $1`, id)
		}
	})

	a, err := st.CreateReport(ctx, mentor, "Student X is rude", fmt.Sprintf("He interrupts every lecture %d", tag))
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, a.ID)
	b, err := st.CreateReport(ctx, student, "Projector broken", "Room 2 projector does not turn on (100% sure)")
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, b.ID)
	if a.Status != "open" || a.Reporter == nil || a.Reporter.Name != "RPT Mentor" || len(a.Roles) != 1 || a.Roles[0] != "mentor" ||
		a.ResolvedBy != nil || a.AdminNote != nil {
		t.Fatalf("new report: %+v", a)
	}

	byMentors, err := st.ListReports(ctx, ReportFilter{Role: "mentor", ReporterID: mentor})
	if err != nil || len(byMentors) != 1 || byMentors[0].ID != a.ID {
		t.Fatalf("mentor's reports: %+v, %v", byMentors, err)
	}
	found, err := st.ListReports(ctx, ReportFilter{Query: fmt.Sprint(tag)})
	if err != nil || len(found) != 1 || found[0].ID != a.ID {
		t.Fatalf("search: %+v, %v", found, err)
	}
	if pct, _ := st.ListReports(ctx, ReportFilter{Query: "100%", ReporterID: student}); len(pct) != 1 {
		t.Errorf("a search with %% must match it literally: %d found", len(pct))
	}
	if none, _ := st.ListReports(ctx, ReportFilter{Query: "0%", ReporterID: mentor}); len(none) != 0 {
		t.Errorf("%% must not be a wildcard: %d found", len(none))
	}

	resolved, note := "resolved", "Talked to him."
	r, err := st.UpdateReport(ctx, a.ID, &resolved, &note, admin)
	if err != nil || r.Status != "resolved" || r.ResolvedBy == nil || r.ResolvedBy.ID != admin || r.ResolvedAt == nil || *r.AdminNote != note {
		t.Fatalf("resolve: %+v, %v", r, err)
	}
	firstAt := *r.ResolvedAt
	other := insertTestUser(t, database, fmt.Sprintf("rpt-admin2-%d@example.com", tag), "RPT Admin Two")
	r, _ = st.UpdateReport(ctx, a.ID, &resolved, nil, other)
	if r.ResolvedBy.ID != admin || !r.ResolvedAt.Equal(firstAt) || *r.AdminNote != note {
		t.Errorf("resolving again must keep who and when and the note: %+v", r)
	}
	empty, open := "", "open"
	r, _ = st.UpdateReport(ctx, a.ID, &open, &empty, admin)
	if r.Status != "open" || r.ResolvedBy != nil || r.ResolvedAt != nil || r.AdminNote != nil {
		t.Errorf("reopen and clear the note: %+v", r)
	}

	openN, resolvedN, err := st.CountReports(ctx)
	if err != nil || openN < 2 {
		t.Errorf("counts: %d open, %d resolved, %v", openN, resolvedN, err)
	}

	if err := st.DeleteReport(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetReport(ctx, b.ID); !errors.Is(err, ErrReportNotFound) {
		t.Errorf("deleted report: %v", err)
	}
	if err := st.DeleteReport(ctx, b.ID); !errors.Is(err, ErrReportNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
	if _, err := st.UpdateReport(ctx, b.ID, &open, nil, admin); !errors.Is(err, ErrReportNotFound) {
		t.Errorf("updating a deleted report: %v", err)
	}

	// the report stays when its sender's account is deleted
	if _, err := database.Exec(`DELETE FROM users WHERE id = $1`, mentor); err != nil {
		t.Fatal(err)
	}
	r, err = st.GetReport(ctx, a.ID)
	if err != nil || r.Reporter != nil || r.ReporterID != nil || len(r.Roles) != 0 {
		t.Errorf("after the sender was deleted: %+v, %v", r, err)
	}
}
