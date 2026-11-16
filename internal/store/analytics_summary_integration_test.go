package store

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/lib/pq"
)

func TestAnalyticsSummaryStore(t *testing.T) {
	database := testDatabase(t)
	st := NewStore(database)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	tag := now.UnixNano()

	person := func(name string, roles ...string) int64 {
		id := insertTestUser(t, database, fmt.Sprintf("sum-%s-%d@example.com", name, tag), name)
		for _, r := range roles {
			mustExec(t, database, `INSERT INTO user_roles (user_id, role) VALUES ($1, $2)`, id, r)
		}
		return id
	}
	admin := person("Summary Admin", "admin")
	student := person("Summary Student", "student")
	otherStudent := person("Summary Other", "student")
	mentor := person("Summary Mentor", "mentor")
	otherMentor := person("Summary Other Mentor", "mentor")

	module := func(title string, opens time.Time) int64 {
		var id int64
		mustQuery(t, database, &id,
			`INSERT INTO modules (title, description, created_by, opens_at, closes_at) VALUES ($1, '', $2, $3, $4) RETURNING id`,
			title, admin, opens, opens.Add(7*24*time.Hour))
		t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM modules WHERE id = $1`, id) })
		return id
	}
	week4 := module("Summary Week 4", now.Add(-14*24*time.Hour))
	week1 := module("Summary Week 1", now.Add(-35*24*time.Hour))

	enroll := func(moduleID, userID int64, grade, feedback string, gradedAt time.Time) {
		mustExec(t, database,
			`INSERT INTO enrollments (module_id, user_id, grade, grade_feedback, graded_at) VALUES ($1, $2, $3, $4, $5)`,
			moduleID, userID, grade, feedback, gradedAt)
	}
	enroll(week4, student, "green", "Month feedback: solid progress, watch the deadline.", now.Add(-10*24*time.Hour))
	enroll(week4, otherStudent, "red", "Someone else's week feedback.", now.Add(-10*24*time.Hour))
	enroll(week1, student, "yellow", "Outside the requested period.", now.Add(-40*24*time.Hour))

	var itemID int64
	mustQuery(t, database, &itemID,
		`INSERT INTO content_items (module_id, type, title, description, created_by) VALUES ($1, 'assignment', 'Summary REST API', '', $2) RETURNING id`,
		week4, admin)
	submit := func(studentID int64, feedback *string, gradedAt, deletedAt *time.Time) {
		mustExec(t, database,
			`INSERT INTO submissions (content_item_id, student_id, text_body, submitted_at, feedback, graded_at, deleted_at)
			 VALUES ($1, $2, 'work', $3, $4, $5, $6)`,
			itemID, studentID, now.Add(-12*24*time.Hour), feedback, gradedAt, deletedAt)
	}
	graded := now.Add(-9 * 24 * time.Hour)
	assignmentFeedback := "Error handling is incomplete; add tests for the failure paths."
	blank := "   "
	submit(student, &assignmentFeedback, &graded, nil)
	submit(student, &blank, &graded, nil)                   // blank feedback is not a record
	submit(otherStudent, &assignmentFeedback, &graded, nil) // another student's submission
	oldGraded := now.Add(-35 * 24 * time.Hour)
	submit(student, &assignmentFeedback, &oldGraded, nil) // outside the period
	deletedAt := now.Add(-12 * 24 * time.Hour)
	submit(student, &assignmentFeedback, &graded, &deletedAt) // deleted submission

	rating := func(ratings *MentorReviewRatings, i int) sql.NullInt16 {
		if ratings == nil {
			return sql.NullInt16{}
		}
		values := []int{ratings.Clarity, ratings.Engagement, ratings.Comfort, ratings.Time}
		return sql.NullInt16{Int16: int16(values[i]), Valid: true}
	}
	review := func(moduleID, studentID, mentorID int64, hadSession bool, reason *string, topics []string,
		ratings *MentorReviewRatings, overall int, comment *string, submittedAt time.Time,
	) {
		var id int64
		mustQuery(t, database, &id, `
			INSERT INTO mentor_reviews (module_id, student_id, mentor_id, had_session, no_session_reason, topics,
			       rating_clarity, rating_engagement, rating_comfort, rating_time, overall, comment, submitted_at)
			VALUES ($1, $2, $3, $4, $5, $6::text[], $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
			moduleID, studentID, mentorID, hadSession, reason, pq.Array(topics),
			rating(ratings, 0), rating(ratings, 1), rating(ratings, 2), rating(ratings, 3),
			overall, comment, submittedAt)
		t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM mentor_reviews WHERE id = $1`, id) })
	}
	reason := "not-scheduled"
	comment := "Clear explanations and useful code review."
	review(week4, student, mentor, true, nil, []string{"goals", "project"},
		&MentorReviewRatings{Clarity: 5, Engagement: 4, Comfort: 5, Time: 3}, 4, &comment, now.Add(-8*24*time.Hour))
	review(week4, otherStudent, mentor, false, &reason, []string{}, nil, 2, nil, now.Add(-7*24*time.Hour))
	review(week1, student, otherMentor, true, nil, []string{"career"},
		&MentorReviewRatings{Clarity: 1, Engagement: 1, Comfort: 1, Time: 1}, 1, nil, now.Add(-7*24*time.Hour))

	from := now.Add(-30 * 24 * time.Hour)
	upper := now.Add(24 * time.Hour)

	t.Run("student feedback merges both sources and filters", func(t *testing.T) {
		rows, err := st.StudentFeedbackHistory(ctx, student, from, upper)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("rows: %+v", rows)
		}
		if row := rows[0]; row.Source != "week_grade" || row.Assignment != "" ||
			row.Feedback != "Month feedback: solid progress, watch the deadline." {
			t.Errorf("week row: %+v", row)
		}
		if row := rows[1]; row.Source != "assignment" || row.ModuleTitle != "Summary Week 4" ||
			row.Assignment != "Summary REST API" || row.Feedback != assignmentFeedback {
			t.Errorf("assignment row: %+v", row)
		}
	})

	t.Run("mentor reviews are filtered and keep the stored answers", func(t *testing.T) {
		rows, err := st.MentorReviewHistory(ctx, mentor, from, upper)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 {
			t.Fatalf("rows: %+v", rows)
		}
		if !rows[0].HadSession || rows[0].Clarity == nil || *rows[0].Clarity != 5 ||
			rows[0].Comment == nil || *rows[0].Comment != comment || len(rows[0].Topics) != 2 {
			t.Errorf("session review: %+v", rows[0])
		}
		if rows[1].HadSession || rows[1].NoSessionReason == nil || *rows[1].NoSessionReason != reason ||
			rows[1].Clarity != nil || rows[1].Overall != 2 {
			t.Errorf("no-session review: %+v", rows[1])
		}
	})

	t.Run("summary cache round trip and upsert", func(t *testing.T) {
		key := SummaryCacheKey{
			SubjectType: "student", SubjectID: student,
			PeriodFrom: "2026-09-01", PeriodTo: "2026-11-16",
			Fingerprint: "abc123", PromptVersion: "student-feedback-v1",
		}
		if row, err := st.GetSummaryCache(ctx, key); err != nil || row != nil {
			t.Fatalf("miss: %+v, %v", row, err)
		}
		if err := st.SaveSummaryCache(ctx, key, SummaryCacheRow{
			SummaryText: "First summary.", SourceCount: 2, Provider: "gemini", Model: "test",
		}); err != nil {
			t.Fatal(err)
		}
		row, err := st.GetSummaryCache(ctx, key)
		if err != nil || row == nil || row.SummaryText != "First summary." || row.SourceCount != 2 {
			t.Fatalf("hit: %+v, %v", row, err)
		}
		if err := st.SaveSummaryCache(ctx, key, SummaryCacheRow{
			SummaryText: "Second summary.", SourceCount: 3, Provider: "gemini", Model: "test",
		}); err != nil {
			t.Fatal(err)
		}
		row, err = st.GetSummaryCache(ctx, key)
		if err != nil || row == nil || row.SummaryText != "Second summary." || row.SourceCount != 3 {
			t.Fatalf("upsert: %+v, %v", row, err)
		}
		var count int
		if err := database.QueryRow(`SELECT COUNT(*) FROM analytics_ai_summaries WHERE subject_id = $1`, student).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("cache rows: %d", count)
		}
		t.Cleanup(func() { _, _ = database.Exec(`DELETE FROM analytics_ai_summaries WHERE subject_id = $1`, student) })
	})
}
