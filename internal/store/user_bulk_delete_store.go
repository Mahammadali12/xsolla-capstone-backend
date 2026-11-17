package store

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
)

// handOverUsersData clears or hands over every row that points at the users in ids without ON DELETE, so they can be
// deleted. Course content (modules, files, quizzes) goes to transferTo; the users' own 1:1 slots are removed, and
// slots they booked are freed. Everything else pointing at users cascades or is set to NULL by the database.
func handOverUsersData(ctx context.Context, tx *sql.Tx, ids []int64, transferTo int64) error {
	steps := []struct {
		query string
		args  []any
	}{
		{`UPDATE modules SET created_by = $2 WHERE created_by = ANY($1)`, []any{pq.Array(ids), transferTo}},
		{`UPDATE files SET uploaded_by = $2 WHERE uploaded_by = ANY($1)`, []any{pq.Array(ids), transferTo}},
		{`UPDATE quizzes SET created_by = $2 WHERE created_by = ANY($1)`, []any{pq.Array(ids), transferTo}},
		{`UPDATE content_items SET created_by = NULL WHERE created_by = ANY($1)`, []any{pq.Array(ids)}},
		{`UPDATE submissions SET graded_by = NULL WHERE graded_by = ANY($1)`, []any{pq.Array(ids)}},
		{`DELETE FROM mentor_slots WHERE mentor_id = ANY($1)`, []any{pq.Array(ids)}},
		{`UPDATE mentor_slots SET booked_by = NULL, meeting_url = NULL, google_event_id = NULL WHERE booked_by = ANY($1)`, []any{pq.Array(ids)}},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}

// DeleteNonAdminUsers permanently deletes, in one transaction, every user who does not hold the admin role, except
// keep. With roles set, only users holding at least one of those roles are deleted. Course content is handed to
// transferTo (see handOverUsersData). It returns how many users were deleted and how many held each role (a user
// with two roles counts under both; a user with no role counts under "none").
func (s *StoreImpl) DeleteNonAdminUsers(ctx context.Context, roles []string, keep, transferTo int64) (int, map[string]int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx,
		`SELECT u.id, ARRAY(SELECT ur.role FROM user_roles ur WHERE ur.user_id = u.id ORDER BY ur.role)
		 FROM users u
		 WHERE u.id <> $1
		   AND NOT EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = u.id AND ur.role = 'admin')
		   AND (coalesce(cardinality($2::text[]), 0) = 0
		        OR EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = u.id AND ur.role = ANY($2::text[])))
		 FOR UPDATE OF u`,
		keep, pq.Array(roles),
	)
	if err != nil {
		return 0, nil, err
	}
	var ids []int64
	byRole := map[string]int{}
	for rows.Next() {
		var id int64
		var held []string
		if err := rows.Scan(&id, pq.Array(&held)); err != nil {
			rows.Close()
			return 0, nil, err
		}
		ids = append(ids, id)
		if len(held) == 0 {
			byRole["none"]++
		}
		for _, r := range held {
			byRole[r]++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	if len(ids) == 0 {
		return 0, byRole, nil
	}

	if err := handOverUsersData(ctx, tx, ids, transferTo); err != nil {
		return 0, nil, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return 0, nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil, err
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, err
	}
	return int(n), byRole, nil
}
