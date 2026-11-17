package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

type QuizService interface {
	CreateQuiz(ctx context.Context, userID int64, roles []string, moduleID int64, req dto.CreateQuizRequest) (*dto.QuizResponse, error)
	StartAIQuizGeneration(ctx context.Context, userID int64, roles []string, moduleID int64, req dto.GenerateAIQuizRequest) (*dto.QuizResponse, error)
	ListQuizzesForModule(ctx context.Context, userID int64, roles []string, moduleID int64) ([]dto.QuizResponse, error)
	GetQuiz(ctx context.Context, userID int64, roles []string, quizID int64) (*dto.QuizResponse, error)
	UpdateQuiz(ctx context.Context, userID int64, roles []string, quizID int64, req dto.UpdateQuizRequest) (*dto.QuizResponse, error)
	DeleteQuiz(ctx context.Context, userID int64, roles []string, quizID int64) error
	SubmitAttempt(ctx context.Context, userID int64, roles []string, quizID int64, req dto.SubmitQuizAttemptRequest) (*dto.QuizResponse, error)
	TeamQuizAttempts(ctx context.Context, userID int64, roles []string, moduleID int64) ([]dto.QuizAttemptResponse, error)
}

type QuizHandlers struct {
	svc QuizService
}

func NewQuizHandlers(svc QuizService) *QuizHandlers {
	return &QuizHandlers{svc: svc}
}

func (h *QuizHandlers) ListQuizzes(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	quizzes, err := h.svc.ListQuizzesForModule(r.Context(), userID, roles, moduleID)
	if err != nil {
		handleQuizError(w, err)
		return
	}
	writeJSON(w, 200, quizzes)
}

func (h *QuizHandlers) CreateQuiz(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	var req dto.CreateQuizRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	quiz, err := h.svc.CreateQuiz(r.Context(), userID, roles, moduleID, req)
	if err != nil {
		handleQuizError(w, err)
		return
	}
	writeJSON(w, 201, quiz)
}

func (h *QuizHandlers) GenerateAIQuiz(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	var req dto.GenerateAIQuizRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	quiz, err := h.svc.StartAIQuizGeneration(r.Context(), userID, roles, moduleID, req)
	if err != nil {
		handleQuizError(w, err)
		return
	}
	writeJSON(w, 202, quiz)
}

func (h *QuizHandlers) GetQuiz(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	quizID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid quiz ID", nil)
		return
	}

	quiz, err := h.svc.GetQuiz(r.Context(), userID, roles, quizID)
	if err != nil {
		handleQuizError(w, err)
		return
	}
	writeJSON(w, 200, quiz)
}

func (h *QuizHandlers) UpdateQuiz(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	quizID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid quiz ID", nil)
		return
	}

	var req dto.UpdateQuizRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	quiz, err := h.svc.UpdateQuiz(r.Context(), userID, roles, quizID, req)
	if err != nil {
		handleQuizError(w, err)
		return
	}
	writeJSON(w, 200, quiz)
}

func (h *QuizHandlers) DeleteQuiz(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	quizID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid quiz ID", nil)
		return
	}

	if err := h.svc.DeleteQuiz(r.Context(), userID, roles, quizID); err != nil {
		handleQuizError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *QuizHandlers) SubmitAttempt(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	quizID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid quiz ID", nil)
		return
	}

	var req dto.SubmitQuizAttemptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	quiz, err := h.svc.SubmitAttempt(r.Context(), userID, roles, quizID, req)
	if err != nil {
		handleQuizError(w, err)
		return
	}
	writeJSON(w, 200, quiz)
}

// @Summary     My team's quiz results
// @Description Every quiz attempt of the students in the caller's team (mentor), or of every student (admin), oldest
// @Description first. A student who has not taken a quiz has no row. Anyone else gets 403.
// @Tags        quizzes
// @Produce     json
// @Security    BearerAuth
// @Param       module_id query int false "only this module's quizzes"
// @Success     200 {array} dto.QuizAttemptResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /teams/me/quiz-attempts [get]
func (h *QuizHandlers) TeamQuizAttempts(w http.ResponseWriter, r *http.Request) {
	var moduleID int64
	if raw := r.URL.Query().Get("module_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			writeError(w, 400, "bad_request", "module_id must be a module ID", nil)
			return
		}
		moduleID = id
	}
	attempts, err := h.svc.TeamQuizAttempts(r.Context(), middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context()), moduleID)
	if err != nil {
		handleQuizError(w, err)
		return
	}
	writeJSON(w, 200, attempts)
}

func handleQuizError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrQuizNotFound),
		errors.Is(err, store.ErrModuleNotFound),
		errors.Is(err, store.ErrContentItemNotFound),
		errors.Is(err, store.ErrFileNotFound):
		writeError(w, 404, "not_found", err.Error(), nil)
	case errors.Is(err, service.ErrQuizForbidden),
		errors.Is(err, service.ErrQuizStudentOnly),
		errors.Is(err, store.ErrNotModuleTeacher),
		errors.Is(err, store.ErrNotEnrolled):
		writeError(w, 403, "forbidden", err.Error(), nil)
	case errors.Is(err, store.ErrQuizAttemptExists):
		writeError(w, 409, "conflict", err.Error(), nil)
	case errors.Is(err, service.ErrInvalidQuizInput),
		errors.Is(err, service.ErrQuizClosed),
		errors.Is(err, store.ErrQuizNotReady):
		writeError(w, 400, "bad_request", err.Error(), nil)
	default:
		writeError(w, 500, "server_error", "quiz request failed", nil)
	}
}
