package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAnalyticsStore(t *testing.T) {
	db := testDatabase(t)
	st := NewStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	tag := now.UnixNano()

	person := func(name string, roles ...string) int64 {
		id := insertTestUser(t, db, fmt.Sprintf("an-%s-%d@example.com", name, tag), name)
		for _, r := range roles {
			lfdbExec(t, db, `INSERT INTO user_roles (user_id, role) VALUES ($1, $2)`, id, r)
		}
		return id
	}
	admin := person("Analytics Admin", "admin")
	student1 := person("Analytics Student One", "student")
	student2 := person("Analytics Student Two", "student")
	mentor1 := person("Analytics Mentor One", "mentor")
	mentor2 := person("Analytics Mentor Two", "mentor")

	module := func(title string, opensAt time.Time) int64 {
		id := lfdbID(t, db, `INSERT INTO modules (title, description, created_by, opens_at, closes_at)
			VALUES ($1, '', $2, $3, $4) RETURNING id`,
			title, admin, opensAt, opensAt.Add(7*24*time.Hour))
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM modules WHERE id = $1`, id) })
		return id
	}
	w1Open := now.Add(-14 * 24 * time.Hour)
	w2Open := now.Add(-7 * 24 * time.Hour)
	w1, w2, w3 := module("Analytics Week 1", w1Open), module("Analytics Week 2", w2Open), module("Analytics Week 3", now)

	grade := func(g string) *string { return &g }
	for _, e := range []struct {
		module, user int64
		grade        *string
	}{
		{w1, student1, grade("green")},
		{w1, student2, grade("red")},
		{w2, student1, grade("yellow")},
		// student2 is not enrolled in week 2; week 3 has no grades.
	} {
		lfdbExec(t, db, `INSERT INTO enrollments (module_id, user_id, grade) VALUES ($1, $2, $3)`, e.module, e.user, e.grade)
	}

	team := func(name string, mentorID int64) int64 {
		id := lfdbID(t, db, `INSERT INTO teams (name, mentor_id) VALUES ($1, $2) RETURNING id`, name, mentorID)
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM teams WHERE id = $1`, id) })
		return id
	}
	teamA, teamB := team("Analytics A", mentor1), team("Analytics B", mentor2)
	lfdbExec(t, db, `INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`, teamA, student1)
	lfdbExec(t, db, `INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`, teamB, student2)

	// ---- weekly standing -------------------------------------------------------------------------------------

	weeks, err := st.WeeklyStanding(ctx, nil, w1Open, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 2 {
		t.Fatalf("standing has %d weeks, want 2: %+v", len(weeks), weeks)
	}
	if weeks[0].ModuleID != w1 || weeks[0].Green != 1 || weeks[0].Yellow != 0 || weeks[0].Red != 1 || weeks[0].Total != 2 {
		t.Errorf("week 1 = %+v", weeks[0])
	}
	if weeks[1].ModuleID != w2 || weeks[1].Green != 0 || weeks[1].Yellow != 1 || weeks[1].Red != 0 || weeks[1].Total != 1 {
		t.Errorf("week 2 = %+v", weeks[1])
	}

	// Week 3 has no grades at all and must still appear with zero counts.
	weeks, err = st.WeeklyStanding(ctx, nil, w2Open, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 2 || weeks[1].ModuleID != w3 || weeks[1].Total != 0 {
		t.Errorf("empty week = %+v", weeks)
	}

	weeks, err = st.WeeklyStanding(ctx, &mentor1, w1Open, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 2 || weeks[0].Green != 1 || weeks[0].Total != 1 || weeks[1].Yellow != 1 || weeks[1].Total != 1 {
		t.Errorf("mentor 1 standing = %+v", weeks)
	}
	weeks, err = st.WeeklyStanding(ctx, &mentor2, w1Open, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 2 || weeks[0].Red != 1 || weeks[0].Total != 1 || weeks[1].Total != 0 {
		t.Errorf("mentor 2 standing = %+v", weeks)
	}

	// ---- assignment status -----------------------------------------------------------------------------------

	past := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	future := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	assignment := func(moduleID int64, title string, dueAt *time.Time) int64 {
		id := lfdbID(t, db, `INSERT INTO content_items (module_id, type, title, created_by)
			VALUES ($1, 'assignment', $2, $3) RETURNING id`, moduleID, title, admin)
		lfdbExec(t, db, `INSERT INTO assignments (content_item_id, due_at) VALUES ($1, $2)`, id, dueAt)
		return id
	}
	onTime := assignment(w1, "On time", past(10*24*time.Hour))
	late := assignment(w1, "Late", past(9*24*time.Hour))
	missing := assignment(w2, "Missing", past(6*24*time.Hour))
	assignment(w2, "Future deadline", future(7*24*time.Hour))
	assignment(w1, "No deadline", nil)
	assignment(w3, "Not enrolled", past(5*24*time.Hour))

	submit := func(item, student int64, at time.Time, deleted bool) {
		lfdbExec(t, db, `INSERT INTO submissions (content_item_id, student_id, text_body, submitted_at, deleted_at)
			VALUES ($1, $2, '', $3, CASE WHEN $4 THEN NOW() END)`, item, student, at, deleted)
	}
	submit(onTime, student1, now.Add(-12*24*time.Hour), false)
	// A later resubmission after the deadline must not turn an on-time first submission into a late one.
	submit(onTime, student1, now.Add(-5*24*time.Hour), false)
	submit(late, student1, now.Add(-8*24*time.Hour), false)
	// A deleted attempt does not exist, and one after the cutoff does not count for this period.
	submit(missing, student1, now.Add(-7*24*time.Hour), true)
	submit(missing, student1, now.Add(time.Hour), false)

	status, err := st.StudentAssignmentStatus(ctx, student1, now.Add(-15*24*time.Hour), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := (AssignmentStatus{OnTime: 1, Late: 1, Missing: 1, Eligible: 3}); status != want {
		t.Errorf("assignment status = %+v, want %+v", status, want)
	}
	status, err = st.StudentAssignmentStatus(ctx, student1, now.Add(-15*24*time.Hour), now, &w1)
	if err != nil {
		t.Fatal(err)
	}
	if want := (AssignmentStatus{OnTime: 1, Late: 1, Missing: 0, Eligible: 2}); status != want {
		t.Errorf("week 1 assignment status = %+v, want %+v", status, want)
	}

	// ---- quiz accuracy ---------------------------------------------------------------------------------------

	quiz := func(moduleID int64, title string) int64 {
		return lfdbID(t, db, `INSERT INTO quizzes (module_id, title, created_by) VALUES ($1, $2, $3) RETURNING id`,
			moduleID, title, admin)
	}
	attempt := func(quizID, studentID int64, score, total int) {
		lfdbExec(t, db, `INSERT INTO quiz_attempts (quiz_id, student_id, answers, score, total)
			VALUES ($1, $2, '[]'::jsonb, $3, $4)`, quizID, studentID, score, total)
	}
	attempt(quiz(w1, "Quiz A"), student1, 8, 10)
	attempt(quiz(w1, "Quiz B"), student1, 2, 5)
	attempt(quiz(w2, "Quiz C"), student1, 0, 0)
	attempt(quiz(w2, "Quiz D"), student2, 3, 4)

	acc, err := st.StudentQuizAccuracy(ctx, student1, w1Open, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(acc) != 2 {
		t.Fatalf("accuracy rows = %+v, want 2 modules", acc)
	}
	if acc[0].ModuleID != w1 || acc[0].ScoreTotal != 10 || acc[0].PossibleTotal != 15 || acc[0].AttemptCount != 2 {
		t.Errorf("week 1 accuracy = %+v", acc[0])
	}
	if acc[1].ModuleID != w2 || acc[1].PossibleTotal != 0 || acc[1].AttemptCount != 1 {
		t.Errorf("week 2 accuracy = %+v", acc[1])
	}

	// ---- mentor feedback -------------------------------------------------------------------------------------

	review := func(moduleID, studentID, mentorID int64, ratings *[4]int, submittedAt time.Time) {
		if ratings == nil {
			lfdbExec(t, db, `INSERT INTO mentor_reviews
				(module_id, student_id, mentor_id, had_session, no_session_reason, topics, overall, submitted_at)
				VALUES ($1, $2, $3, false, 'not-scheduled', '{}', 2, $4)`,
				moduleID, studentID, mentorID, submittedAt)
			return
		}
		lfdbExec(t, db, `INSERT INTO mentor_reviews
			(module_id, student_id, mentor_id, had_session, topics,
			 rating_clarity, rating_engagement, rating_comfort, rating_time, overall, submitted_at)
			VALUES ($1, $2, $3, true, '{goals}', $4, $5, $6, $7, 4, $8)`,
			moduleID, studentID, mentorID, ratings[0], ratings[1], ratings[2], ratings[3], submittedAt)
	}
	review(w1, student1, mentor1, &[4]int{4, 5, 3, 2}, now.Add(-10*24*time.Hour))
	review(w1, student2, mentor1, &[4]int{2, 3, 4, 5}, now.Add(-9*24*time.Hour))
	// No session: no ratings, excluded from the averages.
	review(w2, student2, mentor1, nil, now.Add(-2*24*time.Hour))
	// Submitted outside the period, even though its module is inside.
	review(w2, student1, mentor1, &[4]int{5, 5, 5, 5}, now.Add(-100*24*time.Hour))

	feedback, err := st.MentorFeedback(ctx, mentor1, now.Add(-15*24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if feedback.Clarity.Count != 2 || *feedback.Clarity.Average != 3 ||
		feedback.Engagement.Count != 2 || *feedback.Engagement.Average != 4 ||
		feedback.Comfort.Count != 2 || *feedback.Comfort.Average != 3.5 ||
		feedback.Time.Count != 2 || *feedback.Time.Average != 3.5 {
		t.Errorf("feedback = %+v", feedback)
	}
	empty, err := st.MentorFeedback(ctx, mentor2, now.Add(-15*24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Clarity.Average != nil || empty.Clarity.Count != 0 || empty.Time.Count != 0 {
		t.Errorf("no reviews: %+v", empty)
	}

	// ---- people ----------------------------------------------------------------------------------------------

	people, err := st.SearchAnalyticsPeople(ctx, "student", "Student One", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 || people[0].ID != student1 || people[0].Email == "" {
		t.Errorf("student search = %+v", people)
	}
	people, err = st.SearchAnalyticsPeople(ctx, "mentor", "Analytics", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 2 {
		t.Errorf("mentor search = %+v, want 2", people)
	}
	people, err = st.SearchAnalyticsPeople(ctx, "student", "Analytics", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 || people[0].ID != student2 {
		t.Errorf("paged search = %+v, want the second student", people)
	}

	p, err := st.GetAnalyticsPerson(ctx, student1, "student")
	if err != nil || p.ID != student1 {
		t.Errorf("get student = %+v, %v", p, err)
	}
	if _, err := st.GetAnalyticsPerson(ctx, student1, "mentor"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("student as mentor: %v, want ErrUserNotFound", err)
	}
	if _, err := st.GetAnalyticsPerson(ctx, 999999999, "student"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user: %v, want ErrUserNotFound", err)
	}

	// A left student leaves the selectors but keeps readable analytics.
	lfdbExec(t, db, `UPDATE users SET left_at = NOW() WHERE id = $1`, student2)
	people, err = st.SearchAnalyticsPeople(ctx, "student", "Student Two", 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 0 {
		t.Errorf("left student still listed: %+v", people)
	}
	if _, err := st.GetAnalyticsPerson(ctx, student2, "student"); err != nil {
		t.Errorf("left student's analytics: %v", err)
	}

	// Current-roster attribution: after the student changes teams, the previous mentor's chart loses them.
	lfdbExec(t, db, `DELETE FROM team_members WHERE student_id = $1`, student1)
	lfdbExec(t, db, `INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`, teamB, student1)
	weeks, err = st.WeeklyStanding(ctx, &mentor1, w1Open, now)
	if err != nil {
		t.Fatal(err)
	}
	if weeks[0].Total != 0 {
		t.Errorf("old mentor still counts the moved student: %+v", weeks[0])
	}
	weeks, err = st.WeeklyStanding(ctx, &mentor2, w1Open, now)
	if err != nil {
		t.Fatal(err)
	}
	if weeks[0].Green != 1 || weeks[0].Red != 1 || weeks[0].Total != 2 {
		t.Errorf("new mentor standing = %+v", weeks[0])
	}
}
