package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ModuleStanding is one week's (module's) categorical standing counts. Total counts only students with a
// recorded grade, so Total = Green + Yellow + Red.
type ModuleStanding struct {
	ModuleID    int64
	ModuleTitle string
	OpensAt     time.Time
	Green       int
	Yellow      int
	Red         int
	Total       int
}

// AssignmentStatus is the deadline classification of a student's eligible assignments.
type AssignmentStatus struct {
	OnTime   int
	Late     int
	Missing  int
	Eligible int
}

// ModuleQuizAccuracy is the sum of score and total across a student's attempts in one module.
type ModuleQuizAccuracy struct {
	ModuleID      int64
	ModuleTitle   string
	OpensAt       time.Time
	ScoreTotal    int
	PossibleTotal int
	AttemptCount  int
}

// DimensionAverage is one rated dimension's mean and the number of responses behind it.
type DimensionAverage struct {
	Average *float64
	Count   int
}

// MentorFeedback holds the four student-rated dimensions.
type MentorFeedback struct {
	Clarity    DimensionAverage
	Engagement DimensionAverage
	Comfort    DimensionAverage
	Time       DimensionAverage
}

// AnalyticsPerson is a student or mentor in the analytics selectors and headers.
type AnalyticsPerson struct {
	ID    int64
	Name  string
	Email string
}

// WeeklyStanding counts each module's enrollment grades. A nil mentorID counts every student; otherwise only the
// students on that mentor's current team are counted. Modules without a recorded grade come back with zero counts.
// shortcut: one enrollment grade per module is treated as that week's standing; a module reused by a second cohort
// would need a grade history table.
func (s *StoreImpl) WeeklyStanding(ctx context.Context, mentorID *int64, from, to time.Time) ([]ModuleStanding, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.title, m.opens_at,
		       COUNT(e.user_id) FILTER (WHERE e.grade = 'green'),
		       COUNT(e.user_id) FILTER (WHERE e.grade = 'yellow'),
		       COUNT(e.user_id) FILTER (WHERE e.grade = 'red')
		  FROM modules m
		  LEFT JOIN enrollments e
		    ON e.module_id = m.id
		   AND e.grade IS NOT NULL
		   AND ($1::bigint IS NULL OR e.user_id IN (
		         SELECT tm.student_id
		           FROM team_members tm
		           JOIN teams t ON t.id = tm.team_id
		          WHERE t.mentor_id = $1))
		 WHERE m.deleted_at IS NULL
		   AND m.opens_at >= $2
		   AND m.opens_at < $3
		 GROUP BY m.id, m.title, m.opens_at
		 ORDER BY m.opens_at, m.id`,
		mentorID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ModuleStanding{}
	for rows.Next() {
		var m ModuleStanding
		if err := rows.Scan(&m.ModuleID, &m.ModuleTitle, &m.OpensAt, &m.Green, &m.Yellow, &m.Red); err != nil {
			return nil, err
		}
		m.Total = m.Green + m.Yellow + m.Red
		out = append(out, m)
	}
	return out, rows.Err()
}

// StudentAssignmentStatus classifies the student's assignments whose deadline is in [from, cutoff). Only
// assignments in modules the student is enrolled in count. Compliance uses the first submission before cutoff.
func (s *StoreImpl) StudentAssignmentStatus(ctx context.Context, studentID int64, from, cutoff time.Time, moduleID *int64) (AssignmentStatus, error) {
	var st AssignmentStatus
	err := s.db.QueryRowContext(ctx, `
		WITH eligible AS (
		    SELECT ci.id, a.due_at
		      FROM content_items ci
		      JOIN assignments a ON a.content_item_id = ci.id AND a.deleted_at IS NULL
		      JOIN enrollments en ON en.module_id = ci.module_id AND en.user_id = $1
		     WHERE ci.deleted_at IS NULL
		       AND ci.type = 'assignment'
		       AND a.due_at >= $2
		       AND a.due_at < $3
		       AND ($4::bigint IS NULL OR ci.module_id = $4)
		), first_sub AS (
		    SELECT content_item_id, MIN(submitted_at) AS submitted_at
		      FROM submissions
		     WHERE student_id = $1 AND deleted_at IS NULL AND submitted_at < $3
		     GROUP BY content_item_id
		)
		SELECT COUNT(*) FILTER (WHERE fs.submitted_at <= el.due_at),
		       COUNT(*) FILTER (WHERE fs.submitted_at > el.due_at),
		       COUNT(*) FILTER (WHERE fs.submitted_at IS NULL),
		       COUNT(*)
		  FROM eligible el
		  LEFT JOIN first_sub fs ON fs.content_item_id = el.id`,
		studentID, from, cutoff, moduleID,
	).Scan(&st.OnTime, &st.Late, &st.Missing, &st.Eligible)
	return st, err
}

// StudentQuizAccuracy sums score and total across the student's attempts per module, for modules opening in
// [from, to). Modules with no attempts are absent.
func (s *StoreImpl) StudentQuizAccuracy(ctx context.Context, studentID int64, from, to time.Time, moduleID *int64) ([]ModuleQuizAccuracy, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.title, m.opens_at,
		       COALESCE(SUM(qa.score), 0), COALESCE(SUM(qa.total), 0), COUNT(*)
		  FROM quiz_attempts qa
		  JOIN quizzes q ON q.id = qa.quiz_id AND q.deleted_at IS NULL
		  JOIN modules m ON m.id = q.module_id AND m.deleted_at IS NULL
		 WHERE qa.student_id = $1
		   AND m.opens_at >= $2
		   AND m.opens_at < $3
		   AND ($4::bigint IS NULL OR m.id = $4)
		 GROUP BY m.id, m.title, m.opens_at
		 ORDER BY m.opens_at, m.id`,
		studentID, from, to, moduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ModuleQuizAccuracy{}
	for rows.Next() {
		var a ModuleQuizAccuracy
		if err := rows.Scan(&a.ModuleID, &a.ModuleTitle, &a.OpensAt, &a.ScoreTotal, &a.PossibleTotal, &a.AttemptCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MentorFeedback averages the four dimensions of reviews about the mentor submitted in [from, to).
func (s *StoreImpl) MentorFeedback(ctx context.Context, mentorID int64, from, to time.Time) (MentorFeedback, error) {
	var f MentorFeedback
	var clarity, engagement, comfort, timeRating sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
		SELECT AVG(mr.rating_clarity), COUNT(mr.rating_clarity),
		       AVG(mr.rating_engagement), COUNT(mr.rating_engagement),
		       AVG(mr.rating_comfort), COUNT(mr.rating_comfort),
		       AVG(mr.rating_time), COUNT(mr.rating_time)
		  FROM mentor_reviews mr
		  JOIN modules m ON m.id = mr.module_id AND m.deleted_at IS NULL
		 WHERE mr.mentor_id = $1
		   AND mr.had_session
		   AND mr.submitted_at >= $2
		   AND mr.submitted_at < $3`,
		mentorID, from, to,
	).Scan(&clarity, &f.Clarity.Count, &engagement, &f.Engagement.Count,
		&comfort, &f.Comfort.Count, &timeRating, &f.Time.Count)
	if err != nil {
		return MentorFeedback{}, err
	}
	f.Clarity.Average = nullableFloat(clarity)
	f.Engagement.Average = nullableFloat(engagement)
	f.Comfort.Average = nullableFloat(comfort)
	f.Time.Average = nullableFloat(timeRating)
	return f, nil
}

func nullableFloat(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

// SearchAnalyticsPeople returns active users holding the role whose name or email match q, ordered by name.
func (s *StoreImpl) SearchAnalyticsPeople(ctx context.Context, role, q string, limit, offset int) ([]AnalyticsPerson, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.name, u.email::text
		  FROM users u
		 WHERE u.deleted_at IS NULL
		   AND u.left_at IS NULL
		   AND EXISTS (
		       SELECT 1 FROM user_roles r
		        WHERE r.user_id = u.id
		          AND r.role = $1
		          AND (r.expires_at IS NULL OR r.expires_at > NOW()))
		   AND ($2 = '' OR u.name ILIKE '%' || $2 || '%' OR u.email::text ILIKE '%' || $2 || '%')
		 ORDER BY u.name, u.id
		 LIMIT $3 OFFSET $4`,
		role, q, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AnalyticsPerson{}
	for rows.Next() {
		var p AnalyticsPerson
		if err := rows.Scan(&p.ID, &p.Name, &p.Email); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetAnalyticsPerson returns a live user holding the role, or ErrUserNotFound. Left users are still found: their
// historical analytics remain readable.
func (s *StoreImpl) GetAnalyticsPerson(ctx context.Context, id int64, role string) (*AnalyticsPerson, error) {
	var p AnalyticsPerson
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.name, u.email::text
		  FROM users u
		 WHERE u.id = $1
		   AND u.deleted_at IS NULL
		   AND EXISTS (
		       SELECT 1 FROM user_roles r
		        WHERE r.user_id = u.id
		          AND r.role = $2
		          AND (r.expires_at IS NULL OR r.expires_at > NOW()))`,
		id, role,
	).Scan(&p.ID, &p.Name, &p.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get analytics person: %w", err)
	}
	return &p, nil
}
