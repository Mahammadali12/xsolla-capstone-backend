package store

import (
	"context"
	"time"
)

// BookedSlot is one of a student's booked 1:1s.
type BookedSlot struct {
	StartsAt time.Time
	EndsAt   time.Time
}

// ListBookedSlots returns the student's booked 1:1s that start in [from, to), earliest first. Deadlines follow the day
// of the student's 1:1, and a hand-in made until the 1:1 has ended can still count as late.
func (s *StoreImpl) ListBookedSlots(ctx context.Context, studentID int64, from, to time.Time) ([]BookedSlot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT starts_at, ends_at FROM mentor_slots
		 WHERE booked_by = $1 AND deleted_at IS NULL AND starts_at >= $2 AND starts_at < $3
		 ORDER BY starts_at`,
		studentID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slots []BookedSlot
	for rows.Next() {
		var b BookedSlot
		if err := rows.Scan(&b.StartsAt, &b.EndsAt); err != nil {
			return nil, err
		}
		slots = append(slots, b)
	}
	return slots, rows.Err()
}

// MarkEnrollmentRed turns a student's grade for a week red because they handed work in late. It is the system's grade:
// graded_by is cleared, and the mentor's feedback text is kept. A mentor or admin can change it afterwards (an exception).
func (s *StoreImpl) MarkEnrollmentRed(ctx context.Context, moduleID, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE enrollments SET grade = 'red', graded_at = NOW(), graded_by = NULL
		 WHERE module_id = $1 AND user_id = $2 AND (grade IS DISTINCT FROM 'red')`,
		moduleID, userID,
	)
	return err
}
