package store

import (
	"context"
	"encoding/json"
)

// ListTeamQuizAttempts returns the quiz attempts of the students in the mentor's team (every student's when
// everyone is true, for admins), oldest first. moduleID 0 means every module. Attempts of deleted quizzes are left out.
func (s *StoreImpl) ListTeamQuizAttempts(ctx context.Context, mentorID, moduleID int64, everyone bool) ([]QuizAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT qa.id, qa.quiz_id, qa.student_id, qa.answers, qa.score, qa.total, qa.submitted_at
		FROM quiz_attempts qa
		JOIN quizzes q ON q.id = qa.quiz_id AND q.deleted_at IS NULL
		WHERE ($2 = 0 OR q.module_id = $2)
		  AND ($3 OR qa.student_id IN (
		        SELECT tm.student_id FROM team_members tm JOIN teams t ON t.id = tm.team_id WHERE t.mentor_id = $1))
		ORDER BY qa.submitted_at, qa.id`,
		mentorID, moduleID, everyone,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []QuizAttempt{}
	for rows.Next() {
		var a QuizAttempt
		var rawAns []byte
		if err := rows.Scan(&a.ID, &a.QuizID, &a.StudentID, &rawAns, &a.Score, &a.Total, &a.SubmittedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawAns, &a.Answers); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
