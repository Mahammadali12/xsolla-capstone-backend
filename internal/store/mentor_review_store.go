package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

var (
	// ErrMentorReviewExists is returned when the student already reviewed this module.
	ErrMentorReviewExists = errors.New("mentor review already sent for this module")
	// ErrMentorReviewNoTeam is returned when the student is not in a team.
	ErrMentorReviewNoTeam = errors.New("student is not in a team")
	// ErrMentorReviewNoMentor is returned when the student's team has no mentor.
	ErrMentorReviewNoMentor = errors.New("student's team has no mentor")
)

// MentorReviewRatings are the four statements a student rates 1-5 when there was a session.
type MentorReviewRatings struct {
	Clarity    int
	Engagement int
	Comfort    int
	Time       int
}

// MentorReviewInput is a validated review ready to be stored. MentorID and TeamID are not part of it:
// they are read from the student's team at the moment of the insert.
type MentorReviewInput struct {
	ModuleID        int64
	StudentID       int64
	HadSession      bool
	NoSessionReason *string
	Topics          []string
	OtherTopic      *string
	Ratings         *MentorReviewRatings
	Overall         int
	Comment         *string
}

// MentorReview is a stored weekly mentor review.
type MentorReview struct {
	ID          int64
	ModuleID    int64
	StudentID   int64
	MentorID    int64
	TeamID      *int64
	SubmittedAt time.Time
}

// ReviewedModule is a module the student has reviewed, and when.
type ReviewedModule struct {
	ModuleID    int64
	SubmittedAt time.Time
}

// HasMentorReview reports whether the student already reviewed the module.
func (s *StoreImpl) HasMentorReview(ctx context.Context, moduleID, studentID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM mentor_reviews WHERE module_id = $1 AND student_id = $2)`,
		moduleID, studentID,
	).Scan(&exists)
	return exists, err
}

// CreateMentorReview stores the review with the mentor and team the student has right now.
// The team lookup and the insert are one statement, so a team change in between cannot mix them up.
// Returns ErrMentorReviewExists, ErrMentorReviewNoTeam or ErrMentorReviewNoMentor when nothing was stored.
func (s *StoreImpl) CreateMentorReview(ctx context.Context, in MentorReviewInput) (*MentorReview, error) {
	var clarity, engagement, comfort, timeRating sql.NullInt16
	if in.Ratings != nil {
		clarity = sql.NullInt16{Int16: int16(in.Ratings.Clarity), Valid: true}
		engagement = sql.NullInt16{Int16: int16(in.Ratings.Engagement), Valid: true}
		comfort = sql.NullInt16{Int16: int16(in.Ratings.Comfort), Valid: true}
		timeRating = sql.NullInt16{Int16: int16(in.Ratings.Time), Valid: true}
	}
	topics := in.Topics
	if topics == nil {
		topics = []string{}
	}

	var r MentorReview
	var teamID sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO mentor_reviews (
			module_id, student_id, mentor_id, team_id, had_session, no_session_reason, topics, other_topic,
			rating_clarity, rating_engagement, rating_comfort, rating_time, overall, comment)
		SELECT $1::bigint, $2::bigint, t.mentor_id, t.id, $3::boolean, $4::text, $5::text[], $6::text,
		       $7::smallint, $8::smallint, $9::smallint, $10::smallint, $11::smallint, $12::text
		  FROM team_members tm
		  JOIN teams t ON t.id = tm.team_id
		 WHERE tm.student_id = $2 AND t.mentor_id IS NOT NULL
		ON CONFLICT ON CONSTRAINT mentor_reviews_one_per_week DO NOTHING
		RETURNING id, module_id, student_id, mentor_id, team_id, submitted_at`,
		in.ModuleID, in.StudentID, in.HadSession, in.NoSessionReason, pq.Array(topics), in.OtherTopic,
		clarity, engagement, comfort, timeRating, in.Overall, in.Comment,
	).Scan(&r.ID, &r.ModuleID, &r.StudentID, &r.MentorID, &teamID, &r.SubmittedAt)
	if err == nil {
		if teamID.Valid {
			r.TeamID = &teamID.Int64
		}
		return &r, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// Nothing inserted: find out why.
	return nil, s.whyNoMentorReview(ctx, in.ModuleID, in.StudentID)
}

func (s *StoreImpl) whyNoMentorReview(ctx context.Context, moduleID, studentID int64) error {
	exists, err := s.HasMentorReview(ctx, moduleID, studentID)
	if err != nil {
		return err
	}
	if exists {
		return ErrMentorReviewExists
	}
	var mentorID sql.NullInt64
	err = s.db.QueryRowContext(ctx,
		`SELECT t.mentor_id FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE tm.student_id = $1`,
		studentID,
	).Scan(&mentorID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMentorReviewNoTeam
	}
	if err != nil {
		return err
	}
	if !mentorID.Valid {
		return ErrMentorReviewNoMentor
	}
	// The team got a mentor between the insert and this check; the caller may simply try again.
	return errors.New("mentor review not stored: the student's team changed, try again")
}

// ListReviewedModules returns the modules the student has reviewed, oldest review first.
func (s *StoreImpl) ListReviewedModules(ctx context.Context, studentID int64) ([]ReviewedModule, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT module_id, submitted_at FROM mentor_reviews WHERE student_id = $1 ORDER BY submitted_at, module_id`,
		studentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reviewed := []ReviewedModule{}
	for rows.Next() {
		var m ReviewedModule
		if err := rows.Scan(&m.ModuleID, &m.SubmittedAt); err != nil {
			return nil, err
		}
		reviewed = append(reviewed, m)
	}
	return reviewed, rows.Err()
}
