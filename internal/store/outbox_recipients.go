package store

import (
	"context"

	"github.com/lib/pq"
)

// activeStudentRecipients returns every active student as a recipient snapshot.
func (q *Query) activeStudentRecipients(ctx context.Context) ([]recipientSnapshot, error) {
	return q.recipientsWithRole(ctx, "student", `
		SELECT u.id, u.name, u.email
		FROM users u
		WHERE u.deleted_at IS NULL AND u.left_at IS NULL
		  AND EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = u.id AND ur.role = 'student')
		ORDER BY u.id`)
}

// enrolledStudentRecipients returns the active students enrolled in a module.
func (q *Query) enrolledStudentRecipients(ctx context.Context, moduleID int64) ([]recipientSnapshot, error) {
	return q.recipientsWithRole(ctx, "student", `
		SELECT DISTINCT u.id, u.name, u.email
		FROM users u
		JOIN enrollments e ON e.user_id = u.id
		WHERE e.module_id = $1 AND u.deleted_at IS NULL AND u.left_at IS NULL
		  AND EXISTS (SELECT 1 FROM user_roles ur WHERE ur.user_id = u.id AND ur.role = 'student')
		ORDER BY u.id`, moduleID)
}

// moduleAttendeeRecipients returns the module creator and every enrolled user,
// each with one role so the notification policy can match them.
func (q *Query) moduleAttendeeRecipients(ctx context.Context, moduleID int64) ([]recipientSnapshot, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT u.id, u.name, u.email,
		       COALESCE(array_agg(ur.role) FILTER (WHERE ur.role IS NOT NULL), '{}') AS roles
		FROM users u
		LEFT JOIN user_roles ur ON ur.user_id = u.id
		WHERE u.deleted_at IS NULL AND u.left_at IS NULL
		  AND u.id IN (
		    SELECT created_by FROM modules WHERE id = $1 AND deleted_at IS NULL
		    UNION
		    SELECT user_id FROM enrollments WHERE module_id = $1
		  )
		GROUP BY u.id, u.name, u.email
		ORDER BY u.id`, moduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recipients := []recipientSnapshot{}
	for rows.Next() {
		var recipient recipientSnapshot
		var roles []string
		if err := rows.Scan(&recipient.ID, &recipient.Name, &recipient.Email, pq.Array(&roles)); err != nil {
			return nil, err
		}
		recipient.Role = primaryRole(roles)
		recipients = append(recipients, recipient)
	}
	return recipients, rows.Err()
}

// mentorTeamStudentRecipients returns the active students in a mentor's team.
func (q *Query) mentorTeamStudentRecipients(ctx context.Context, mentorID int64) ([]recipientSnapshot, error) {
	return q.recipientsWithRole(ctx, "student", `
		SELECT u.id, u.name, u.email
		FROM users u
		JOIN team_members tm ON tm.student_id = u.id
		JOIN teams t ON t.id = tm.team_id
		WHERE t.mentor_id = $1 AND u.deleted_at IS NULL AND u.left_at IS NULL
		ORDER BY u.id`, mentorID)
}

// studentMentorRecipients returns the mentor who leads the student's team, if any.
func (q *Query) studentMentorRecipients(ctx context.Context, studentID int64) ([]recipientSnapshot, error) {
	return q.recipientsWithRole(ctx, "mentor", `
		SELECT m.id, m.name, m.email
		FROM users m
		JOIN teams t ON t.mentor_id = m.id
		JOIN team_members tm ON tm.team_id = t.id
		WHERE tm.student_id = $1 AND m.deleted_at IS NULL AND m.left_at IS NULL
		LIMIT 1`, studentID)
}

// recipientsWithRole runs a three-column recipient query and labels every row
// with the same role.
func (q *Query) recipientsWithRole(ctx context.Context, role, query string, args ...any) ([]recipientSnapshot, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recipients := []recipientSnapshot{}
	for rows.Next() {
		recipient := recipientSnapshot{Role: role}
		if err := rows.Scan(&recipient.ID, &recipient.Name, &recipient.Email); err != nil {
			return nil, err
		}
		recipients = append(recipients, recipient)
	}
	return recipients, rows.Err()
}

// primaryRole picks a deterministic role for a user who may hold several.
func primaryRole(roles []string) string {
	for _, candidate := range []string{"student", "mentor", "lecturer", "admin"} {
		for _, role := range roles {
			if role == candidate {
				return candidate
			}
		}
	}
	return "student"
}
