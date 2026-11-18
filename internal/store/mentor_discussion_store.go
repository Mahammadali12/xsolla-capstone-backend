package store

import (
	"context"
	"time"

	"github.com/lib/pq"
)

// Discussion is a talk a mentor set up with one student (a row per invited student, rows made together share BatchID).
// It is not a 1:1 slot, so it never moves a deadline.
type Discussion struct {
	ID          int64
	BatchID     int64
	MentorID    int64
	MentorName  string
	StudentID   int64
	StudentName string
	Title       string
	Note        string
	StartsAt    time.Time
	EndsAt      time.Time
	CreatedAt   time.Time
}

const discussionColumns = `d.id, d.batch_id, d.mentor_id, COALESCE(m.name, ''), d.student_id, COALESCE(u.name, ''),
	d.title, d.note, d.starts_at, d.ends_at, d.created_at`

// CreateMentorDiscussions saves one talk for every student in one transaction and returns the new rows.
func (s *StoreImpl) CreateMentorDiscussions(ctx context.Context, mentorID int64, studentIDs []int64, title, note string, startsAt, endsAt time.Time) ([]Discussion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var batch int64
	if err := tx.QueryRowContext(ctx, `SELECT nextval('mentor_discussion_batches')`).Scan(&batch); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx,
		`WITH ins AS (
		     INSERT INTO mentor_discussions (batch_id, mentor_id, student_id, title, note, starts_at, ends_at)
		     SELECT $1, $2, sid, $4, $5, $6, $7 FROM unnest($3::bigint[]) AS sid
		     RETURNING *
		 )
		 SELECT `+discussionColumns+`
		 FROM ins d
		 LEFT JOIN users m ON m.id = d.mentor_id
		 LEFT JOIN users u ON u.id = d.student_id
		 ORDER BY d.id`,
		batch, mentorID, pq.Array(studentIDs), title, note, startsAt, endsAt,
	)
	if err != nil {
		return nil, err
	}
	out, err := scanDiscussions(rows)
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// ListMentorDiscussions returns the talks the caller may see, earliest first: an admin all, a mentor their own,
// a student only the ones they are invited to.
func (s *StoreImpl) ListMentorDiscussions(ctx context.Context, userID int64, roles []string) ([]Discussion, error) {
	where, args := `d.student_id = $1`, []any{userID}
	for _, r := range roles {
		switch r {
		case "admin":
			where, args = `TRUE`, nil
		case "mentor":
			if where != `TRUE` {
				where = `d.mentor_id = $1`
			}
		}
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+discussionColumns+`
		 FROM mentor_discussions d
		 LEFT JOIN users m ON m.id = d.mentor_id
		 LEFT JOIN users u ON u.id = d.student_id
		 WHERE d.deleted_at IS NULL AND `+where+`
		 ORDER BY d.starts_at, d.id`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	return scanDiscussions(rows)
}

// DeleteMentorDiscussion removes a whole talk (every invited student) that the caller made, or any one for an admin.
// It reports whether anything was removed.
func (s *StoreImpl) DeleteMentorDiscussion(ctx context.Context, userID int64, isAdmin bool, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE mentor_discussions SET deleted_at = NOW()
		 WHERE deleted_at IS NULL
		   AND batch_id = (SELECT batch_id FROM mentor_discussions WHERE id = $1)
		   AND ($3 OR mentor_id = $2)`,
		id, userID, isAdmin,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func scanDiscussions(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}) ([]Discussion, error) {
	defer rows.Close()
	out := []Discussion{}
	for rows.Next() {
		var d Discussion
		if err := rows.Scan(&d.ID, &d.BatchID, &d.MentorID, &d.MentorName, &d.StudentID, &d.StudentName,
			&d.Title, &d.Note, &d.StartsAt, &d.EndsAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
