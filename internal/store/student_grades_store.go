package store

import (
	"context"

	"github.com/lib/pq"
)

// ListStudentWeekGrades returns the week grades (red, yellow, green) the given students have, by student. Weeks of
// deleted modules and ungraded weeks are left out.
func (s *StoreImpl) ListStudentWeekGrades(ctx context.Context, studentIDs []int64) (map[int64][]string, error) {
	out := map[int64][]string{}
	if len(studentIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.user_id, e.grade
		  FROM enrollments e
		  JOIN modules m ON m.id = e.module_id AND m.deleted_at IS NULL
		 WHERE e.user_id = ANY($1) AND e.grade IS NOT NULL`,
		pq.Array(studentIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var grade string
		if err := rows.Scan(&id, &grade); err != nil {
			return nil, err
		}
		out[id] = append(out[id], grade)
	}
	return out, rows.Err()
}
