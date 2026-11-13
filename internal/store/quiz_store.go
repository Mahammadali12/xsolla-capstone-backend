package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lib/pq"
)

var (
	ErrQuizNotFound = errors.New("quiz not found")
	ErrQuizNotReady = errors.New("quiz is not ready yet")
)

type QuizQuestionInput struct {
	Prompt       string
	Options      []string
	CorrectIndex int
}

type QuizQuestion struct {
	ID           int64
	QuizID       int64
	Position     int
	Prompt       string
	Options      []string
	CorrectIndex int
}

type QuizAttempt struct {
	ID          int64
	QuizID      int64
	StudentID   int64
	Answers     []int
	Score       int
	Total       int
	SubmittedAt time.Time
}

type Quiz struct {
	ID                  int64
	ModuleID            int64
	SourceContentItemID *int64
	Title               string
	Description         string
	DueAt               *time.Time
	Status              string
	ErrorMessage        string
	IsAIGenerated       bool
	CreatedBy           int64
	CreatedAt           time.Time
	DeletedAt           *time.Time
	Questions           []QuizQuestion
}

func (s *StoreImpl) CreateQuiz(ctx context.Context, moduleID int64, sourceContentItemID *int64, title, description string, dueAt *time.Time, status string, isAI bool, createdBy int64, questions []QuizQuestionInput) (*Quiz, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var q Quiz
	var srcID sql.NullInt64
	var due sql.NullTime
	err = tx.QueryRowContext(
		ctx,
		`INSERT INTO quizzes (module_id, source_content_item_id, title, description, due_at, status, is_ai_generated, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id, module_id, source_content_item_id, title, description, due_at, status, error_message, is_ai_generated, created_by, created_at`,
		moduleID, sourceContentItemID, title, description, dueAt, status, isAI, createdBy,
	).Scan(&q.ID, &q.ModuleID, &srcID, &q.Title, &q.Description, &due, &q.Status, &q.ErrorMessage, &q.IsAIGenerated, &q.CreatedBy, &q.CreatedAt)
	if err != nil {
		return nil, err
	}
	if srcID.Valid {
		v := srcID.Int64
		q.SourceContentItemID = &v
	}
	if due.Valid {
		t := due.Time
		q.DueAt = &t
	}

	inserted, err := insertQuizQuestionsTx(ctx, tx, q.ID, questions)
	if err != nil {
		return nil, err
	}
	q.Questions = inserted

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &q, nil
}

func (s *StoreImpl) GetQuizByID(ctx context.Context, id int64) (*Quiz, error) {
	var q Quiz
	var srcID sql.NullInt64
	var due sql.NullTime
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, module_id, source_content_item_id, title, description, due_at, status, error_message, is_ai_generated, created_by, created_at
		 FROM quizzes
		 WHERE id = $1 AND deleted_at IS NULL`,
		id,
	).Scan(&q.ID, &q.ModuleID, &srcID, &q.Title, &q.Description, &due, &q.Status, &q.ErrorMessage, &q.IsAIGenerated, &q.CreatedBy, &q.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrQuizNotFound
	}
	if err != nil {
		return nil, err
	}
	if srcID.Valid {
		v := srcID.Int64
		q.SourceContentItemID = &v
	}
	if due.Valid {
		t := due.Time
		q.DueAt = &t
	}

	qMap, err := s.loadQuestionsByQuizIDs(ctx, []int64{q.ID})
	if err != nil {
		return nil, err
	}
	q.Questions = qMap[q.ID]
	if q.Questions == nil {
		q.Questions = []QuizQuestion{}
	}
	return &q, nil
}

func (s *StoreImpl) ListQuizzesForModule(ctx context.Context, moduleID int64, readyOnly bool) ([]Quiz, error) {
	query := `SELECT id, module_id, source_content_item_id, title, description, due_at, status, error_message, is_ai_generated, created_by, created_at
		FROM quizzes
		WHERE module_id = $1 AND deleted_at IS NULL`
	if readyOnly {
		query += ` AND status = 'ready'`
	}
	query += ` ORDER BY created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, query, moduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var quizzes []Quiz
	var ids []int64
	for rows.Next() {
		var q Quiz
		var srcID sql.NullInt64
		var due sql.NullTime
		if err := rows.Scan(&q.ID, &q.ModuleID, &srcID, &q.Title, &q.Description, &due, &q.Status, &q.ErrorMessage, &q.IsAIGenerated, &q.CreatedBy, &q.CreatedAt); err != nil {
			return nil, err
		}
		if srcID.Valid {
			v := srcID.Int64
			q.SourceContentItemID = &v
		}
		if due.Valid {
			t := due.Time
			q.DueAt = &t
		}
		q.Questions = []QuizQuestion{}
		quizzes = append(quizzes, q)
		ids = append(ids, q.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(quizzes) == 0 {
		return []Quiz{}, nil
	}

	qMap, err := s.loadQuestionsByQuizIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range quizzes {
		if qs := qMap[quizzes[i].ID]; qs != nil {
			quizzes[i].Questions = qs
		}
	}
	return quizzes, nil
}

func (s *StoreImpl) UpdateQuiz(ctx context.Context, id int64, title *string, description *string, dueAt *time.Time, clearDueAt bool, questions []QuizQuestionInput, replaceQuestions bool) (*Quiz, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var curTitle, curDesc string
	var curDue sql.NullTime
	err = tx.QueryRowContext(
		ctx,
		`SELECT title, description, due_at FROM quizzes WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		id,
	).Scan(&curTitle, &curDesc, &curDue)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrQuizNotFound
	}
	if err != nil {
		return nil, err
	}

	newTitle := curTitle
	if title != nil {
		newTitle = *title
	}
	newDesc := curDesc
	if description != nil {
		newDesc = *description
	}
	var newDue *time.Time
	if !clearDueAt {
		if dueAt != nil {
			newDue = dueAt
		} else if curDue.Valid {
			t := curDue.Time
			newDue = &t
		}
	}

	var q Quiz
	var srcID sql.NullInt64
	var due sql.NullTime
	err = tx.QueryRowContext(
		ctx,
		`UPDATE quizzes
		 SET title = $2, description = $3, due_at = $4
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING id, module_id, source_content_item_id, title, description, due_at, status, error_message, is_ai_generated, created_by, created_at`,
		id, newTitle, newDesc, newDue,
	).Scan(&q.ID, &q.ModuleID, &srcID, &q.Title, &q.Description, &due, &q.Status, &q.ErrorMessage, &q.IsAIGenerated, &q.CreatedBy, &q.CreatedAt)
	if err != nil {
		return nil, err
	}
	if srcID.Valid {
		v := srcID.Int64
		q.SourceContentItemID = &v
	}
	if due.Valid {
		t := due.Time
		q.DueAt = &t
	}

	if replaceQuestions {
		if _, err := tx.ExecContext(ctx, `DELETE FROM quiz_questions WHERE quiz_id = $1`, id); err != nil {
			return nil, err
		}
		inserted, err := insertQuizQuestionsTx(ctx, tx, id, questions)
		if err != nil {
			return nil, err
		}
		q.Questions = inserted
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if !replaceQuestions {
		qMap, err := s.loadQuestionsByQuizIDs(ctx, []int64{q.ID})
		if err != nil {
			return nil, err
		}
		q.Questions = qMap[q.ID]
		if q.Questions == nil {
			q.Questions = []QuizQuestion{}
		}
	}
	return &q, nil
}

func (s *StoreImpl) CompleteAIQuiz(ctx context.Context, quizID int64, title, description string, questions []QuizQuestionInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var curTitle, curDesc string
	err = tx.QueryRowContext(
		ctx,
		`SELECT title, description FROM quizzes WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		quizID,
	).Scan(&curTitle, &curDesc)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrQuizNotFound
	}
	if err != nil {
		return err
	}

	finalTitle := curTitle
	if title != "" && (curTitle == "" || curTitle == "Generating AI quiz...") {
		finalTitle = title
	}
	finalDesc := curDesc
	if description != "" {
		finalDesc = description
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM quiz_questions WHERE quiz_id = $1`, quizID); err != nil {
		return err
	}
	if _, err := insertQuizQuestionsTx(ctx, tx, quizID, questions); err != nil {
		return err
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE quizzes SET title = $2, description = $3, status = 'ready', error_message = '' WHERE id = $1`,
		quizID, finalTitle, finalDesc,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *StoreImpl) FailAIQuiz(ctx context.Context, quizID int64, errMsg string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE quizzes SET status = 'failed', error_message = $2 WHERE id = $1 AND deleted_at IS NULL`,
		quizID, errMsg,
	)
	return err
}

func (s *StoreImpl) SoftDeleteQuiz(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE quizzes SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrQuizNotFound
	}
	return nil
}

func (s *StoreImpl) UpsertQuizAttempt(ctx context.Context, quizID, studentID int64, answers []int, score, total int) (*QuizAttempt, error) {
	ansJSON, err := json.Marshal(answers)
	if err != nil {
		return nil, err
	}

	var a QuizAttempt
	var rawAns []byte
	err = s.db.QueryRowContext(
		ctx,
		`INSERT INTO quiz_attempts (quiz_id, student_id, answers, score, total, submitted_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (quiz_id, student_id)
		 DO UPDATE SET answers = EXCLUDED.answers, score = EXCLUDED.score, total = EXCLUDED.total, submitted_at = NOW()
		 RETURNING id, quiz_id, student_id, answers, score, total, submitted_at`,
		quizID, studentID, ansJSON, score, total,
	).Scan(&a.ID, &a.QuizID, &a.StudentID, &rawAns, &a.Score, &a.Total, &a.SubmittedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rawAns, &a.Answers); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *StoreImpl) GetQuizAttempt(ctx context.Context, quizID, studentID int64) (*QuizAttempt, error) {
	var a QuizAttempt
	var rawAns []byte
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, quiz_id, student_id, answers, score, total, submitted_at
		 FROM quiz_attempts
		 WHERE quiz_id = $1 AND student_id = $2`,
		quizID, studentID,
	).Scan(&a.ID, &a.QuizID, &a.StudentID, &rawAns, &a.Score, &a.Total, &a.SubmittedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rawAns, &a.Answers); err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *StoreImpl) ListQuizAttemptsForStudent(ctx context.Context, studentID int64, quizIDs []int64) (map[int64]QuizAttempt, error) {
	out := make(map[int64]QuizAttempt)
	if len(quizIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, quiz_id, student_id, answers, score, total, submitted_at
		 FROM quiz_attempts
		 WHERE student_id = $1 AND quiz_id = ANY($2)`,
		studentID, pq.Array(quizIDs),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var a QuizAttempt
		var rawAns []byte
		if err := rows.Scan(&a.ID, &a.QuizID, &a.StudentID, &rawAns, &a.Score, &a.Total, &a.SubmittedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawAns, &a.Answers); err != nil {
			return nil, err
		}
		out[a.QuizID] = a
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func insertQuizQuestionsTx(ctx context.Context, tx *sql.Tx, quizID int64, questions []QuizQuestionInput) ([]QuizQuestion, error) {
	out := make([]QuizQuestion, 0, len(questions))
	for i, q := range questions {
		optsJSON, err := json.Marshal(q.Options)
		if err != nil {
			return nil, err
		}
		var inserted QuizQuestion
		var rawOpts []byte
		err = tx.QueryRowContext(
			ctx,
			`INSERT INTO quiz_questions (quiz_id, position, prompt, options, correct_index)
			 VALUES ($1, $2, $3, $4, $5)
			 RETURNING id, quiz_id, position, prompt, options, correct_index`,
			quizID, i, q.Prompt, optsJSON, q.CorrectIndex,
		).Scan(&inserted.ID, &inserted.QuizID, &inserted.Position, &inserted.Prompt, &rawOpts, &inserted.CorrectIndex)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawOpts, &inserted.Options); err != nil {
			return nil, err
		}
		out = append(out, inserted)
	}
	return out, nil
}

func (s *StoreImpl) loadQuestionsByQuizIDs(ctx context.Context, quizIDs []int64) (map[int64][]QuizQuestion, error) {
	out := make(map[int64][]QuizQuestion)
	if len(quizIDs) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, quiz_id, position, prompt, options, correct_index
		 FROM quiz_questions
		 WHERE quiz_id = ANY($1)
		 ORDER BY quiz_id ASC, position ASC, id ASC`,
		pq.Array(quizIDs),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var qq QuizQuestion
		var rawOpts []byte
		if err := rows.Scan(&qq.ID, &qq.QuizID, &qq.Position, &qq.Prompt, &rawOpts, &qq.CorrectIndex); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rawOpts, &qq.Options); err != nil {
			return nil, err
		}
		out[qq.QuizID] = append(out[qq.QuizID], qq)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
