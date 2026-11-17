package store

import (
	"context"
	"time"
)

// ListBookedSlotStarts returns when the student's booked 1:1s start, for slots starting in [from, to), earliest first.
// Task deadlines follow the day of the student's 1:1.
func (s *StoreImpl) ListBookedSlotStarts(ctx context.Context, studentID int64, from, to time.Time) ([]time.Time, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT starts_at FROM mentor_slots
		 WHERE booked_by = $1 AND deleted_at IS NULL AND starts_at >= $2 AND starts_at < $3
		 ORDER BY starts_at`,
		studentID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var starts []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		starts = append(starts, t)
	}
	return starts, rows.Err()
}
