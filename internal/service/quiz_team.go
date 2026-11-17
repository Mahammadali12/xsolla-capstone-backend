package service

import (
	"context"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/dto"
)

// TeamQuizAttempts returns the quiz attempts of the caller's team: a mentor gets their own team's students (none
// without a team), an admin every student's. moduleID 0 means every module. Anyone else: ErrQuizForbidden.
func (s *QuizService) TeamQuizAttempts(ctx context.Context, userID int64, roles []string, moduleID int64) ([]dto.QuizAttemptResponse, error) {
	admin := auth.HasRole(roles, "admin")
	if !admin && !auth.HasRole(roles, "mentor") {
		return nil, ErrQuizForbidden
	}
	attempts, err := s.store.ListTeamQuizAttempts(ctx, userID, moduleID, admin)
	if err != nil {
		return nil, err
	}
	out := make([]dto.QuizAttemptResponse, len(attempts))
	for i, a := range attempts {
		out[i] = dto.QuizAttemptResponse{ID: a.ID, QuizID: a.QuizID, StudentID: a.StudentID, Answers: a.Answers,
			Score: a.Score, Total: a.Total, SubmittedAt: a.SubmittedAt}
	}
	return out, nil
}
