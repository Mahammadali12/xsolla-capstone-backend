package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

// StudentFeedbackRecord is one piece of written feedback a student received, from a week grade or a graded
// assignment submission. Source is "week_grade" or "assignment".
type StudentFeedbackRecord struct {
	Source      string
	ModuleTitle string
	Assignment  string
	Feedback    string
	GradedAt    time.Time
}

// MentorReviewRecord is one student review about a mentor. The four dimension ratings are nil when the student
// reported no session (the schema stores them only for reviews with a session).
type MentorReviewRecord struct {
	ModuleTitle     string
	SubmittedAt     time.Time
	HadSession      bool
	NoSessionReason *string
	Topics          []string
	OtherTopic      *string
	Clarity         *int
	Engagement      *int
	Comfort         *int
	Time            *int
	Overall         int
	Comment         *string
}

// SummaryCacheKey identifies one cached summary: subject, period, source fingerprint and prompt version.
// Period dates are YYYY-MM-DD.
type SummaryCacheKey struct {
	SubjectType   string
	SubjectID     int64
	PeriodFrom    string
	PeriodTo      string
	Fingerprint   string
	PromptVersion string
}

// SummaryCacheRow is a stored summary and its non-sensitive provenance.
type SummaryCacheRow struct {
	SummaryText string
	SourceCount int
	Provider    string
	Model       string
	GeneratedAt time.Time
}

// StudentFeedbackHistory returns the student's non-empty week-grade feedback and assignment feedback in
// [from, upper), oldest first. No identifiers of the student or the grader are selected.
// shortcut: submissions.feedback and enrollments.grade_feedback hold only the latest text, so older versions
// cannot be summarized; a feedback history table would be needed to keep them.
func (s *StoreImpl) StudentFeedbackHistory(ctx context.Context, studentID int64, from, upper time.Time) ([]StudentFeedbackRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT 'week_grade' AS source, m.title AS module_title, ''::text AS assignment,
		       e.grade_feedback AS feedback, e.graded_at
		  FROM enrollments e
		  JOIN modules m ON m.id = e.module_id AND m.deleted_at IS NULL
		 WHERE e.user_id = $1
		   AND e.graded_at >= $2
		   AND e.graded_at < $3
		   AND e.grade_feedback IS NOT NULL
		   AND btrim(e.grade_feedback) <> ''
		UNION ALL
		SELECT 'assignment', m.title, ci.title, s.feedback, s.graded_at
		  FROM submissions s
		  JOIN content_items ci ON ci.id = s.content_item_id AND ci.deleted_at IS NULL
		  JOIN modules m ON m.id = ci.module_id AND m.deleted_at IS NULL
		 WHERE s.student_id = $1
		   AND s.deleted_at IS NULL
		   AND s.graded_at >= $2
		   AND s.graded_at < $3
		   AND s.feedback IS NOT NULL
		   AND btrim(s.feedback) <> ''
		 ORDER BY graded_at, source, module_title, assignment`,
		studentID, from, upper)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []StudentFeedbackRecord{}
	for rows.Next() {
		var r StudentFeedbackRecord
		if err := rows.Scan(&r.Source, &r.ModuleTitle, &r.Assignment, &r.Feedback, &r.GradedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MentorReviewHistory returns every review about the mentor submitted in [from, upper), oldest first.
// Student identifiers are not selected.
func (s *StoreImpl) MentorReviewHistory(ctx context.Context, mentorID int64, from, upper time.Time) ([]MentorReviewRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.title, mr.submitted_at, mr.had_session, mr.no_session_reason, mr.topics, mr.other_topic,
		       mr.rating_clarity, mr.rating_engagement, mr.rating_comfort, mr.rating_time, mr.overall, mr.comment
		  FROM mentor_reviews mr
		  JOIN modules m ON m.id = mr.module_id AND m.deleted_at IS NULL
		 WHERE mr.mentor_id = $1
		   AND mr.submitted_at >= $2
		   AND mr.submitted_at < $3
		 ORDER BY mr.submitted_at, mr.id`,
		mentorID, from, upper)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MentorReviewRecord{}
	for rows.Next() {
		var r MentorReviewRecord
		var clarity, engagement, comfort, timeRating sql.NullInt16
		if err := rows.Scan(&r.ModuleTitle, &r.SubmittedAt, &r.HadSession, &r.NoSessionReason,
			pq.Array(&r.Topics), &r.OtherTopic,
			&clarity, &engagement, &comfort, &timeRating, &r.Overall, &r.Comment); err != nil {
			return nil, err
		}
		r.Clarity = nullableInt(clarity)
		r.Engagement = nullableInt(engagement)
		r.Comfort = nullableInt(comfort)
		r.Time = nullableInt(timeRating)
		if r.Topics == nil {
			r.Topics = []string{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func nullableInt(v sql.NullInt16) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int16)
	return &n
}

// GetSummaryCache returns the cached summary for the key, or (nil, nil) when there is none.
func (s *StoreImpl) GetSummaryCache(ctx context.Context, key SummaryCacheKey) (*SummaryCacheRow, error) {
	var row SummaryCacheRow
	err := s.db.QueryRowContext(ctx, `
		SELECT summary_text, source_count, provider, model, generated_at
		  FROM analytics_ai_summaries
		 WHERE subject_type = $1
		   AND subject_id = $2
		   AND period_from = $3::date
		   AND period_to = $4::date
		   AND source_fingerprint = $5
		   AND prompt_version = $6`,
		key.SubjectType, key.SubjectID, key.PeriodFrom, key.PeriodTo, key.Fingerprint, key.PromptVersion,
	).Scan(&row.SummaryText, &row.SourceCount, &row.Provider, &row.Model, &row.GeneratedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SaveSummaryCache inserts or refreshes the summary for the key.
func (s *StoreImpl) SaveSummaryCache(ctx context.Context, key SummaryCacheKey, row SummaryCacheRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO analytics_ai_summaries
		    (subject_type, subject_id, period_from, period_to, source_fingerprint, prompt_version,
		     provider, model, summary_text, source_count)
		VALUES ($1, $2, $3::date, $4::date, $5, $6, $7, $8, $9, $10)
		ON CONFLICT ON CONSTRAINT analytics_ai_summaries_key
		DO UPDATE SET provider = EXCLUDED.provider,
		              model = EXCLUDED.model,
		              summary_text = EXCLUDED.summary_text,
		              source_count = EXCLUDED.source_count,
		              generated_at = NOW()`,
		key.SubjectType, key.SubjectID, key.PeriodFrom, key.PeriodTo, key.Fingerprint, key.PromptVersion,
		row.Provider, row.Model, row.SummaryText, row.SourceCount)
	return err
}
