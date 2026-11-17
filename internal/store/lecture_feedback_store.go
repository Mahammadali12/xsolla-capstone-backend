package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

var (
	// ErrLectureNotFound is returned for an unknown or deleted event, or an event that is not a lecture.
	ErrLectureNotFound = errors.New("lecture not found")
	// ErrLectureFeedbackExists is returned when the student already sent feedback for the lecture.
	ErrLectureFeedbackExists = errors.New("lecture feedback already sent")
)

// Lecture is the part of an event the feedback needs.
type Lecture struct {
	ID          int64
	Title       string
	StartsAt    time.Time
	EndsAt      time.Time
	ModuleID    *int64
	ModuleTitle *string
}

// LectureFeedbackInput is a validated feedback form.
type LectureFeedbackInput struct {
	EventID    int64
	StudentID  int64
	Overall    int
	Clarity    int
	Practice   int
	Confidence int
	Pace       string
	Liked      *string
	Improve    *string
}

// LectureFeedbackRow is one stored feedback with the names the admin pages show.
type LectureFeedbackRow struct {
	ID           int64
	Lecture      Lecture
	StudentID    int64
	StudentName  string
	StudentEmail string
	Overall      int
	Clarity      int
	Practice     int
	Confidence   int
	Pace         string
	Liked        *string
	Improve      *string
	SubmittedAt  time.Time
}

// SentLectureFeedback is a lecture the student has given feedback on, and when.
type SentLectureFeedback struct {
	EventID     int64
	SubmittedAt time.Time
}

// LectureFeedbackFilter narrows the admin routes; nil fields do not filter. From/To bound the lecture's start.
type LectureFeedbackFilter struct {
	EventID  *int64
	ModuleID *int64
	From     *time.Time // starts_at >= From
	To       *time.Time // starts_at < To
}

// LectureWithAudience is a lecture and how many students may give feedback on it today.
type LectureWithAudience struct {
	Lecture  Lecture
	Expected int
}

// LectureFeedbackPerson is a lecturer of a lecture's week.
type LectureFeedbackPerson struct {
	ID    int64
	Name  string
	Email string
}

// GetLecture returns a lecture (an event of kind 'lecture' that is not deleted), or ErrLectureNotFound.
func (s *StoreImpl) GetLecture(ctx context.Context, eventID int64) (*Lecture, error) {
	var l Lecture
	err := s.db.QueryRowContext(ctx, `
		SELECT e.id, e.title, e.starts_at, e.ends_at, e.module_id, m.title
		  FROM events e LEFT JOIN modules m ON m.id = e.module_id
		 WHERE e.id = $1 AND e.kind = 'lecture' AND e.deleted_at IS NULL`, eventID,
	).Scan(&l.ID, &l.Title, &l.StartsAt, &l.EndsAt, &l.ModuleID, &l.ModuleTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLectureNotFound
	}
	return &l, err
}

// HasLectureFeedback reports whether the student already sent feedback for the lecture.
func (s *StoreImpl) HasLectureFeedback(ctx context.Context, eventID, studentID int64) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM lecture_feedback WHERE event_id = $1 AND student_id = $2)`, eventID, studentID).Scan(&ok)
	return ok, err
}

// CreateLectureFeedback stores the form. Two sends at the same moment: the second gets ErrLectureFeedbackExists.
func (s *StoreImpl) CreateLectureFeedback(ctx context.Context, in LectureFeedbackInput) (*SentLectureFeedback, error) {
	var out SentLectureFeedback
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO lecture_feedback (event_id, student_id, overall, clarity, practice, confidence, pace, liked, improve)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT ON CONSTRAINT lecture_feedback_one_per_lecture DO NOTHING
		RETURNING event_id, submitted_at`,
		in.EventID, in.StudentID, in.Overall, in.Clarity, in.Practice, in.Confidence, in.Pace, in.Liked, in.Improve,
	).Scan(&out.EventID, &out.SubmittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrLectureFeedbackExists
	}
	return &out, err
}

// ListSentLectureFeedback returns the lectures the student has given feedback on, newest first.
func (s *StoreImpl) ListSentLectureFeedback(ctx context.Context, studentID int64) ([]SentLectureFeedback, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT event_id, submitted_at FROM lecture_feedback WHERE student_id = $1 ORDER BY submitted_at DESC, event_id`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SentLectureFeedback{}
	for rows.Next() {
		var f SentLectureFeedback
		if err := rows.Scan(&f.EventID, &f.SubmittedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListPendingLectures returns the lectures the student may give feedback on at `now` and has not yet: the form is
// open from opensBefore before the end to openFor after the end, and a lecture of a week needs the student enrolled.
func (s *StoreImpl) ListPendingLectures(ctx context.Context, studentID int64, now time.Time, opensBefore, openFor time.Duration) ([]Lecture, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.title, e.starts_at, e.ends_at, e.module_id, m.title
		  FROM events e LEFT JOIN modules m ON m.id = e.module_id
		 WHERE e.kind = 'lecture' AND e.deleted_at IS NULL
		   AND $2 >= e.ends_at - make_interval(secs => $3) AND $2 < e.ends_at + make_interval(secs => $4)
		   AND (e.module_id IS NULL OR EXISTS (SELECT 1 FROM enrollments en WHERE en.module_id = e.module_id AND en.user_id = $1))
		   AND NOT EXISTS (SELECT 1 FROM lecture_feedback lf WHERE lf.event_id = e.id AND lf.student_id = $1)
		 ORDER BY e.ends_at, e.id`,
		studentID, now, opensBefore.Seconds(), openFor.Seconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Lecture{}
	for rows.Next() {
		var l Lecture
		if err := rows.Scan(&l.ID, &l.Title, &l.StartsAt, &l.EndsAt, &l.ModuleID, &l.ModuleTitle); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// lectureFilterSQL is the shared WHERE part for the admin queries ($1 event, $2 module, $3 from, $4 to).
const lectureFilterSQL = `e.kind = 'lecture' AND e.deleted_at IS NULL
		   AND ($1::bigint IS NULL OR e.id = $1)
		   AND ($2::bigint IS NULL OR e.module_id = $2)
		   AND ($3::timestamptz IS NULL OR e.starts_at >= $3)
		   AND ($4::timestamptz IS NULL OR e.starts_at < $4)`

// ListLectureFeedback returns the feedback matching the filter, by lecture start, then student.
func (s *StoreImpl) ListLectureFeedback(ctx context.Context, f LectureFeedbackFilter) ([]LectureFeedbackRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT lf.id, e.id, e.title, e.starts_at, e.ends_at, e.module_id, m.title,
		       u.id, u.name, u.email::text, lf.overall, lf.clarity, lf.practice, lf.confidence, lf.pace,
		       lf.liked, lf.improve, lf.submitted_at
		  FROM lecture_feedback lf
		  JOIN events e ON e.id = lf.event_id
		  LEFT JOIN modules m ON m.id = e.module_id
		  JOIN users u ON u.id = lf.student_id
		 WHERE `+lectureFilterSQL+`
		 ORDER BY e.starts_at, e.id, u.name, u.id`,
		f.EventID, f.ModuleID, f.From, f.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LectureFeedbackRow{}
	for rows.Next() {
		var r LectureFeedbackRow
		if err := rows.Scan(&r.ID, &r.Lecture.ID, &r.Lecture.Title, &r.Lecture.StartsAt, &r.Lecture.EndsAt, &r.Lecture.ModuleID,
			&r.Lecture.ModuleTitle, &r.StudentID, &r.StudentName, &r.StudentEmail, &r.Overall, &r.Clarity, &r.Practice,
			&r.Confidence, &r.Pace, &r.Liked, &r.Improve, &r.SubmittedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListLecturesWithAudience returns every lecture matching the filter (also those without feedback) and how many
// students may give feedback on it today: active students without a staff role, enrolled in the lecture's week
// when it has one.
func (s *StoreImpl) ListLecturesWithAudience(ctx context.Context, f LectureFeedbackFilter) ([]LectureWithAudience, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id, e.title, e.starts_at, e.ends_at, e.module_id, m.title,
		       (SELECT COUNT(*) FROM users u
		         WHERE u.left_at IS NULL AND u.deleted_at IS NULL
		           AND (e.module_id IS NULL OR EXISTS (SELECT 1 FROM enrollments en WHERE en.module_id = e.module_id AND en.user_id = u.id))
		           AND EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = u.id AND r.role = 'student'
		                          AND (r.expires_at IS NULL OR r.expires_at > NOW()))
		           AND NOT EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = u.id AND r.role IN ('mentor', 'lecturer', 'admin')
		                          AND (r.expires_at IS NULL OR r.expires_at > NOW())))
		  FROM events e LEFT JOIN modules m ON m.id = e.module_id
		 WHERE `+lectureFilterSQL+`
		 ORDER BY e.starts_at, e.id`,
		f.EventID, f.ModuleID, f.From, f.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LectureWithAudience{}
	for rows.Next() {
		var l LectureWithAudience
		if err := rows.Scan(&l.Lecture.ID, &l.Lecture.Title, &l.Lecture.StartsAt, &l.Lecture.EndsAt, &l.Lecture.ModuleID,
			&l.Lecture.ModuleTitle, &l.Expected); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListWeekLecturers returns, per module, the lecturers assigned to it (enrolled with an active lecturer role).
func (s *StoreImpl) ListWeekLecturers(ctx context.Context, moduleIDs []int64) (map[int64][]LectureFeedbackPerson, error) {
	out := map[int64][]LectureFeedbackPerson{}
	if len(moduleIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT en.module_id, u.id, u.name, u.email::text
		  FROM enrollments en JOIN users u ON u.id = en.user_id AND u.deleted_at IS NULL
		 WHERE en.module_id = ANY($1)
		   AND EXISTS (SELECT 1 FROM user_roles r WHERE r.user_id = u.id AND r.role = 'lecturer'
		                  AND (r.expires_at IS NULL OR r.expires_at > NOW()))
		 ORDER BY en.module_id, u.name, u.id`, pq.Array(moduleIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var moduleID int64
		var p LectureFeedbackPerson
		if err := rows.Scan(&moduleID, &p.ID, &p.Name, &p.Email); err != nil {
			return nil, err
		}
		out[moduleID] = append(out[moduleID], p)
	}
	return out, rows.Err()
}
