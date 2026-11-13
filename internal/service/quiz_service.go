package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"xsolla-capstone-api/internal/auth"
	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/store"
)

var (
	ErrInvalidQuizInput = errors.New("invalid quiz input")
	ErrQuizForbidden    = errors.New("only lecturers can create or manage quizzes")
	ErrQuizStudentOnly  = errors.New("only students can submit quiz attempts")
	ErrQuizClosed       = errors.New("quiz is closed for submissions")
)

type QuizService struct {
	store   store.Store
	fileSvc *FileService
	ai      QuizGenerator
}

func NewQuizService(s store.Store, fileSvc *FileService, ai QuizGenerator) *QuizService {
	return &QuizService{
		store:   s,
		fileSvc: fileSvc,
		ai:      ai,
	}
}

func (s *QuizService) CreateQuiz(ctx context.Context, userID int64, roles []string, moduleID int64, req dto.CreateQuizRequest) (*dto.QuizResponse, error) {
	if !auth.HasRole(roles, "lecturer") {
		return nil, ErrQuizForbidden
	}
	teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !teacher {
		return nil, store.ErrNotModuleTeacher
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidQuizInput)
	}
	qInputs, err := validateQuestionRequests(req.Questions)
	if err != nil {
		return nil, err
	}

	q, err := s.store.CreateQuiz(ctx, moduleID, nil, title, strings.TrimSpace(req.Description), req.DueAt, "ready", false, userID, qInputs)
	if err != nil {
		return nil, err
	}
	return mapQuizResponse(*q, nil, true), nil
}

func (s *QuizService) StartAIQuizGeneration(ctx context.Context, userID int64, roles []string, moduleID int64, req dto.GenerateAIQuizRequest) (*dto.QuizResponse, error) {
	if !auth.HasRole(roles, "lecturer") {
		return nil, ErrQuizForbidden
	}
	teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
	if err != nil {
		return nil, err
	}
	if !teacher {
		return nil, store.ErrNotModuleTeacher
	}
	if s.ai == nil {
		return nil, errors.New("AI quiz generator is not configured")
	}
	if req.ContentItemID <= 0 {
		return nil, fmt.Errorf("%w: content_item_id is required", ErrInvalidQuizInput)
	}

	ci, err := s.store.GetContentItemByID(ctx, req.ContentItemID)
	if err != nil {
		return nil, err
	}
	if ci.ModuleID != moduleID {
		return nil, fmt.Errorf("%w: content item does not belong to this module", ErrInvalidQuizInput)
	}
	if ci.Type != "file" && ci.Type != "link" {
		return nil, fmt.Errorf("%w: quiz can only be generated from lesson materials", ErrInvalidQuizInput)
	}

	src := QuizSource{
		Title:       ci.Title,
		Description: ci.Description,
	}
	if ci.Url != nil {
		src.URL = *ci.Url
	}

	var storageKey string
	if ci.FileID != nil {
		f, err := s.store.GetFileByID(ctx, *ci.FileID)
		if err != nil {
			return nil, err
		}
		src.FileName = f.OriginalName
		src.MimeType = f.MimeType
		storageKey = f.StorageKey
	}

	customTitle := strings.TrimSpace(req.Title)
	initialTitle := customTitle
	if initialTitle == "" {
		initialTitle = fmt.Sprintf("AI Quiz: %s", ci.Title)
	}

	quiz, err := s.store.CreateQuiz(
		ctx,
		moduleID,
		&ci.ID,
		initialTitle,
		"Generating 10 multiple-choice questions from lesson material...",
		req.DueAt,
		"generating",
		true,
		userID,
		nil,
	)
	if err != nil {
		return nil, err
	}

	go s.runAIQuizBackground(quiz.ID, src, storageKey, customTitle)
	return mapQuizResponse(*quiz, nil, true), nil
}

func (s *QuizService) runAIQuizBackground(quizID int64, src QuizSource, storageKey string, customTitle string) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if storageKey != "" {
		if s.fileSvc == nil {
			_ = s.store.FailAIQuiz(bgCtx, quizID, "file storage is unavailable")
			return
		}
		rc, err := s.fileSvc.DownloadFile(bgCtx, storageKey)
		if err != nil {
			log.Printf("ai quiz %d: download error: %v", quizID, err)
			_ = s.store.FailAIQuiz(bgCtx, quizID, "could not download uploaded lesson material")
			return
		}
		data, readErr := io.ReadAll(io.LimitReader(rc, 20*1024*1024))
		_ = rc.Close()
		if readErr != nil {
			log.Printf("ai quiz %d: read file error: %v", quizID, readErr)
			_ = s.store.FailAIQuiz(bgCtx, quizID, "could not read uploaded lesson material")
			return
		}
		src.FileBytes = data
	}

	gen, err := s.ai.GenerateQuiz(bgCtx, src)
	if err != nil {
		log.Printf("ai quiz %d: generation error: %v", quizID, err)
		_ = s.store.FailAIQuiz(bgCtx, quizID, err.Error())
		return
	}

	qInputs := make([]store.QuizQuestionInput, 0, len(gen.Questions))
	for _, gq := range gen.Questions {
		qInputs = append(qInputs, store.QuizQuestionInput{
			Prompt:       gq.Prompt,
			Options:      gq.Options,
			CorrectIndex: gq.Correct,
		})
	}

	finalTitle := customTitle
	if finalTitle == "" {
		if gen.Title != "" {
			finalTitle = gen.Title
		} else {
			finalTitle = fmt.Sprintf("AI Quiz: %s", src.Title)
		}
	}
	finalDesc := gen.Description
	if finalDesc == "" {
		finalDesc = fmt.Sprintf("10-question multiple-choice quiz generated from %s.", src.Title)
	}

	if err := s.store.CompleteAIQuiz(bgCtx, quizID, finalTitle, finalDesc, qInputs); err != nil {
		log.Printf("ai quiz %d: save error: %v", quizID, err)
		_ = s.store.FailAIQuiz(bgCtx, quizID, "failed to save generated quiz")
	}
}

func (s *QuizService) ListQuizzesForModule(ctx context.Context, userID int64, roles []string, moduleID int64) ([]dto.QuizResponse, error) {
	canSeeAllStatuses, isStaff, err := s.authorizeModuleRead(ctx, userID, roles, moduleID)
	if err != nil {
		return nil, err
	}

	readyOnly := !canSeeAllStatuses
	quizzes, err := s.store.ListQuizzesForModule(ctx, moduleID, readyOnly)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(quizzes))
	for _, q := range quizzes {
		ids = append(ids, q.ID)
	}
	attempts, err := s.store.ListQuizAttemptsForStudent(ctx, userID, ids)
	if err != nil {
		return nil, err
	}

	out := make([]dto.QuizResponse, 0, len(quizzes))
	for _, q := range quizzes {
		var attemptPtr *store.QuizAttempt
		if a, ok := attempts[q.ID]; ok {
			copyA := a
			attemptPtr = &copyA
		}
		includeCorrect := isStaff || attemptPtr != nil
		out = append(out, *mapQuizResponse(q, attemptPtr, includeCorrect))
	}
	return out, nil
}

func (s *QuizService) GetQuiz(ctx context.Context, userID int64, roles []string, quizID int64) (*dto.QuizResponse, error) {
	q, err := s.store.GetQuizByID(ctx, quizID)
	if err != nil {
		return nil, err
	}
	canSeeAllStatuses, isStaff, err := s.authorizeModuleRead(ctx, userID, roles, q.ModuleID)
	if err != nil {
		return nil, err
	}
	if !canSeeAllStatuses && q.Status != "ready" {
		return nil, store.ErrQuizNotFound
	}

	attempt, err := s.store.GetQuizAttempt(ctx, quizID, userID)
	if err != nil {
		return nil, err
	}
	includeCorrect := isStaff || attempt != nil
	return mapQuizResponse(*q, attempt, includeCorrect), nil
}

func (s *QuizService) UpdateQuiz(ctx context.Context, userID int64, roles []string, quizID int64, req dto.UpdateQuizRequest) (*dto.QuizResponse, error) {
	if !auth.HasRole(roles, "lecturer") {
		return nil, ErrQuizForbidden
	}
	existing, err := s.store.GetQuizByID(ctx, quizID)
	if err != nil {
		return nil, err
	}
	teacher, err := s.store.IsUserTeacherOfModule(ctx, existing.ModuleID, userID)
	if err != nil {
		return nil, err
	}
	if !teacher {
		return nil, store.ErrNotModuleTeacher
	}

	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		return nil, fmt.Errorf("%w: title cannot be empty", ErrInvalidQuizInput)
	}
	var qInputs []store.QuizQuestionInput
	replaceQuestions := req.ReplaceQuestions || len(req.Questions) > 0
	if replaceQuestions {
		qInputs, err = validateQuestionRequests(req.Questions)
		if err != nil {
			return nil, err
		}
	}

	var trimmedTitle *string
	if req.Title != nil {
		v := strings.TrimSpace(*req.Title)
		trimmedTitle = &v
	}
	var trimmedDesc *string
	if req.Description != nil {
		v := strings.TrimSpace(*req.Description)
		trimmedDesc = &v
	}

	updated, err := s.store.UpdateQuiz(ctx, quizID, trimmedTitle, trimmedDesc, req.DueAt, req.ClearDueAt, qInputs, replaceQuestions)
	if err != nil {
		return nil, err
	}
	return mapQuizResponse(*updated, nil, true), nil
}

func (s *QuizService) DeleteQuiz(ctx context.Context, userID int64, roles []string, quizID int64) error {
	if !auth.HasRole(roles, "lecturer") {
		return ErrQuizForbidden
	}
	existing, err := s.store.GetQuizByID(ctx, quizID)
	if err != nil {
		return err
	}
	teacher, err := s.store.IsUserTeacherOfModule(ctx, existing.ModuleID, userID)
	if err != nil {
		return err
	}
	if !teacher {
		return store.ErrNotModuleTeacher
	}
	return s.store.SoftDeleteQuiz(ctx, quizID)
}

func (s *QuizService) SubmitAttempt(ctx context.Context, userID int64, roles []string, quizID int64, req dto.SubmitQuizAttemptRequest) (*dto.QuizResponse, error) {
	if !auth.HasRole(roles, "student") {
		return nil, ErrQuizStudentOnly
	}
	q, err := s.store.GetQuizByID(ctx, quizID)
	if err != nil {
		return nil, err
	}
	enrolled, err := s.store.IsUserEnrolled(ctx, q.ModuleID, userID)
	if err != nil {
		return nil, err
	}
	if !enrolled {
		return nil, store.ErrNotEnrolled
	}
	if q.Status != "ready" || len(q.Questions) == 0 {
		return nil, store.ErrQuizNotReady
	}

	m, err := s.store.GetModuleByID(ctx, q.ModuleID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if !m.OpensAt.IsZero() && now.Before(m.OpensAt) {
		return nil, fmt.Errorf("%w: module is not open yet", ErrQuizClosed)
	}
	if !m.ClosesAt.IsZero() && now.After(m.ClosesAt) {
		return nil, fmt.Errorf("%w: module is closed", ErrQuizClosed)
	}
	if q.DueAt != nil && now.After(*q.DueAt) {
		return nil, fmt.Errorf("%w: quiz deadline has passed", ErrQuizClosed)
	}

	existing, err := s.store.GetQuizAttempt(ctx, quizID, userID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, store.ErrQuizAttemptExists
	}

	if len(req.Answers) != len(q.Questions) {
		return nil, fmt.Errorf("%w: must answer all %d questions", ErrInvalidQuizInput, len(q.Questions))
	}

	score := 0
	for i, qq := range q.Questions {
		ans := req.Answers[i]
		if ans < 0 || ans >= len(qq.Options) {
			return nil, fmt.Errorf("%w: invalid option selected for question %d", ErrInvalidQuizInput, i+1)
		}
		if ans == qq.CorrectIndex {
			score++
		}
	}

	attempt, err := s.store.UpsertQuizAttempt(ctx, quizID, userID, req.Answers, score, len(q.Questions))
	if err != nil {
		return nil, err
	}
	return mapQuizResponse(*q, attempt, true), nil
}

func (s *QuizService) authorizeModuleRead(ctx context.Context, userID int64, roles []string, moduleID int64) (bool, bool, error) {
	if auth.HasRole(roles, "admin") {
		return true, true, nil
	}
	if auth.HasRole(roles, "lecturer") {
		teacher, err := s.store.IsUserTeacherOfModule(ctx, moduleID, userID)
		if err != nil {
			return false, false, err
		}
		return teacher, true, nil
	}
	if auth.HasRole(roles, "mentor") {
		return false, true, nil
	}
	enrolled, err := s.store.IsUserEnrolled(ctx, moduleID, userID)
	if err != nil {
		return false, false, err
	}
	if !enrolled {
		return false, false, store.ErrNotEnrolled
	}
	return false, false, nil
}

func validateQuestionRequests(reqs []dto.CreateQuizQuestionRequest) ([]store.QuizQuestionInput, error) {
	if len(reqs) == 0 {
		return nil, fmt.Errorf("%w: at least one question is required", ErrInvalidQuizInput)
	}
	out := make([]store.QuizQuestionInput, 0, len(reqs))
	for i, q := range reqs {
		prompt := strings.TrimSpace(q.Prompt)
		if prompt == "" {
			return nil, fmt.Errorf("%w: question %d prompt cannot be empty", ErrInvalidQuizInput, i+1)
		}
		opts := make([]string, 0, len(q.Options))
		for _, opt := range q.Options {
			trimmed := strings.TrimSpace(opt)
			if trimmed == "" {
				return nil, fmt.Errorf("%w: question %d contains an empty option", ErrInvalidQuizInput, i+1)
			}
			opts = append(opts, trimmed)
		}
		if len(opts) < 2 || len(opts) > 6 {
			return nil, fmt.Errorf("%w: question %d must have between 2 and 6 options", ErrInvalidQuizInput, i+1)
		}
		if q.Correct < 0 || q.Correct >= len(opts) {
			return nil, fmt.Errorf("%w: question %d correct option index is out of range", ErrInvalidQuizInput, i+1)
		}
		out = append(out, store.QuizQuestionInput{
			Prompt:       prompt,
			Options:      opts,
			CorrectIndex: q.Correct,
		})
	}
	return out, nil
}

func mapQuizResponse(q store.Quiz, attempt *store.QuizAttempt, includeCorrect bool) *dto.QuizResponse {
	questions := make([]dto.QuizQuestionResponse, 0, len(q.Questions))
	for _, qq := range q.Questions {
		qr := dto.QuizQuestionResponse{
			ID:       qq.ID,
			Position: qq.Position,
			Prompt:   qq.Prompt,
			Options:  qq.Options,
		}
		if includeCorrect {
			c := qq.CorrectIndex
			qr.Correct = &c
		}
		questions = append(questions, qr)
	}

	var attemptResp *dto.QuizAttemptResponse
	if attempt != nil {
		attemptResp = &dto.QuizAttemptResponse{
			ID:          attempt.ID,
			QuizID:      attempt.QuizID,
			StudentID:   attempt.StudentID,
			Answers:     attempt.Answers,
			Score:       attempt.Score,
			Total:       attempt.Total,
			SubmittedAt: attempt.SubmittedAt,
		}
	}

	return &dto.QuizResponse{
		ID:                  q.ID,
		ModuleID:            q.ModuleID,
		SourceContentItemID: q.SourceContentItemID,
		Title:               q.Title,
		Description:         q.Description,
		DueAt:               q.DueAt,
		Status:              q.Status,
		ErrorMessage:        q.ErrorMessage,
		IsAIGenerated:       q.IsAIGenerated,
		CreatedBy:           q.CreatedBy,
		CreatedAt:           q.CreatedAt,
		Questions:           questions,
		Attempt:             attemptResp,
	}
}
