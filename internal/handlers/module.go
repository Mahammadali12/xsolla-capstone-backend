package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// ModuleService is the interface that module handlers depend on.
type ModuleService interface {
	CreateModule(ctx context.Context, userID int64, title string, description string, opensAt, closesAt time.Time) (*store.Module, error)
	GetModuleByID(ctx context.Context, userID int64, roles []string, moduleID int64) (*store.Module, error)
	GetModulesForUser(ctx context.Context, userID int64, roles []string) ([]store.Module, error)
	PatchModule(ctx context.Context, userID int64, roles []string, moduleID int64, title *string, description string, opensAt, closesAt time.Time) (*store.Module, error)
	DeleteModule(ctx context.Context, userID int64, roles []string, moduleID int64) error
	CreateEnrollment(ctx context.Context, actorID int64, actorRoles []string, moduleID int64, userID int64) (*store.Enrollment, error)
	DeleteEnrollment(ctx context.Context, actorID int64, actorRoles []string, moduleID int64, userID int64) error
	GetEnrollmentsForModule(ctx context.Context, userID int64, roles []string, moduleID int64) ([]store.Enrollment, error)
	UpdateEnrollmentGrade(ctx context.Context, userID int64, roles []string, moduleID int64, studentID int64, grade string, feedback string) (*store.Enrollment, error)
}

type ModuleHandlers struct {
	svc ModuleService
}

// NewModuleHandlers creates module handlers backed by the given service.
func NewModuleHandlers(svc ModuleService) *ModuleHandlers {
	return &ModuleHandlers{svc: svc}
}

// @Summary     Create module
// @Description Create a new module (teacher or admin only). The creator is auto-enrolled as teacher.
// @Tags        modules
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       request body dto.CreateModuleRequest true "Module details"
// @Success     201 {object} dto.ModuleResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     500 {object} handlers.ErrorResponse
// @Router      /modules [post]
func (h *ModuleHandlers) CreateModule(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())

	if !middleware.HasAnyRole(r, "lecturer", "admin") {
		writeError(w, 403, "forbidden", "only lecturers and admins can create modules", nil)
		return
	}

	var req dto.CreateModuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, 400, "bad_request", "title is required", nil)
		return
	}
	if req.OpensAt.IsZero() {
		writeError(w, 400, "bad_request", "opens_at is required", nil)
		return
	}
	if req.ClosesAt.IsZero() {
		writeError(w, 400, "bad_request", "closes_at is required", nil)
		return
	}
	if !req.ClosesAt.After(req.OpensAt) {
		writeError(w, 400, "bad_request", "closes_at must be after opens_at", nil)
		return
	}

	m, err := h.svc.CreateModule(r.Context(), userID, req.Title, req.Description, req.OpensAt, req.ClosesAt)
	if err != nil {
		writeError(w, 500, "server_error", "could not create module", nil)
		return
	}

	writeJSON(w, 201, dto.ModuleResponse{
		ID:          m.ID,
		Title:       m.Title,
		Description: m.Description,
		CreatedBy:   m.CreatedBy,
		CreatedAt:   m.CreatedAt,
		OpensAt:     m.OpensAt,
		ClosesAt:    m.ClosesAt,
	})
}

// @Summary     List modules
// @Description Get modules the user is enrolled in (student), teaching (teacher), or all (admin)
// @Tags        modules
// @Produce     json
// @Security    BearerAuth
// @Success     200 {array} dto.ModuleResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     500 {object} handlers.ErrorResponse
// @Router      /modules [get]
func (h *ModuleHandlers) ListModules(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	modules, err := h.svc.GetModulesForUser(r.Context(), userID, roles)
	if err != nil {
		writeError(w, 500, "server_error", "could not list modules", nil)
		return
	}

	resp := make([]dto.ModuleResponse, len(modules))
	for i, m := range modules {
		resp[i] = dto.ModuleResponse{
			ID:          m.ID,
			Title:       m.Title,
			Description: m.Description,
			CreatedBy:   m.CreatedBy,
			CreatedAt:   m.CreatedAt,
			OpensAt:     m.OpensAt,
			ClosesAt:    m.ClosesAt,
		}
	}
	writeJSON(w, 200, resp)
}

// @Summary     Get module by ID
// @Description Get module details if the user is enrolled or is the teacher
// @Tags        modules
// @Produce     json
// @Security    BearerAuth
// @Param       id path int true "Module ID"
// @Success     200 {object} dto.ModuleResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /modules/{id} [get]
func (h *ModuleHandlers) GetModule(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	m, err := h.svc.GetModuleByID(r.Context(), userID, roles, moduleID)
	if err != nil {
		if errors.Is(err, service.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "not enrolled in this module", nil)
		} else if errors.Is(err, service.ErrModuleNotFound) {
			writeError(w, 404, "not_found", "module not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not get module", nil)
		}
		return
	}

	writeJSON(w, 200, dto.ModuleResponse{
		ID:          m.ID,
		Title:       m.Title,
		Description: m.Description,
		CreatedBy:   m.CreatedBy,
		CreatedAt:   m.CreatedAt,
		OpensAt:     m.OpensAt,
		ClosesAt:    m.ClosesAt,
	})
}

// @Summary     Update module
// @Description Update module title/description (lecturer or admin only)
// @Tags        modules
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id path int true "Module ID"
// @Param       request body dto.UpdateModuleRequest true "Module details"
// @Success     200 {object} dto.ModuleResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /modules/{id} [patch]
func (h *ModuleHandlers) PatchModule(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	var req dto.UpdateModuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	if req.Title != nil {
		trimmed := strings.TrimSpace(*req.Title)
		if trimmed == "" {
			writeError(w, 400, "bad_request", "title is required", nil)
			return
		}
		req.Title = &trimmed
	}

	m, err := h.svc.PatchModule(r.Context(), userID, roles, moduleID, req.Title, req.Description, req.OpensAt, req.ClosesAt)
	if err != nil {
		if errors.Is(err, service.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else if errors.Is(err, service.ErrModuleNotFound) {
			writeError(w, 404, "not_found", "module not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not update module", nil)
		}
		return
	}

	writeJSON(w, 200, dto.ModuleResponse{
		ID:          m.ID,
		Title:       m.Title,
		Description: m.Description,
		CreatedBy:   m.CreatedBy,
		CreatedAt:   m.CreatedAt,
		OpensAt:     m.OpensAt,
		ClosesAt:    m.ClosesAt,
	})
}

// @Summary     Delete module
// @Description Soft-delete a module (lecturer or admin only)
// @Tags        modules
// @Security    BearerAuth
// @Param       id path int true "Module ID"
// @Success     204 {string} string "No content"
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /modules/{id} [delete]
func (h *ModuleHandlers) DeleteModule(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	if err := h.svc.DeleteModule(r.Context(), userID, roles, moduleID); err != nil {
		if errors.Is(err, service.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else if errors.Is(err, service.ErrModuleNotFound) {
			writeError(w, 404, "not_found", "module not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not delete module", nil)
		}
		return
	}

	w.WriteHeader(204)
}

// @Summary     Enroll in module
// @Description Enroll a user in a module. Students can only enroll themselves, teachers/admins can enroll anyone
// @Tags        enrollments
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       moduleId path int true "Module ID"
// @Param       request body dto.EnrollRequest true "User ID to enroll"
// @Success     201 {object} dto.EnrollmentResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     409 {object} handlers.ErrorResponse
// @Router      /modules/{moduleId}/enrollments [post]
func (h *ModuleHandlers) CreateEnrollment(w http.ResponseWriter, r *http.Request) {
	actorID := middleware.CtxUserID(r.Context())
	actorRoles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	var req dto.EnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	userID := req.UserID

	e, err := h.svc.CreateEnrollment(r.Context(), actorID, actorRoles, moduleID, userID)
	if err != nil {
		if errors.Is(err, service.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else if errors.Is(err, service.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "students can only enroll themselves", nil)
		} else if errors.Is(err, service.ErrAlreadyEnrolled) {
			writeError(w, 409, "conflict", "already enrolled in this module", nil)
		} else if errors.Is(err, service.ErrModuleNotFound) {
			writeError(w, 404, "not_found", "module not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not enroll", nil)
		}
		return
	}

	writeJSON(w, 201, dto.EnrollmentResponse{
		ID:        e.ID,
		ModuleID:  e.ModuleID,
		UserID:    e.UserID,
		CreatedAt: e.CreatedAt,
	})
}

// @Summary     Unenroll from module
// @Description Remove a user from a module. Students can only unenroll themselves
// @Tags        enrollments
// @Security    BearerAuth
// @Param       moduleId path int true "Module ID"
// @Param       userId path int true "User ID"
// @Success     204 {string} string "No content"
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /modules/{moduleId}/enrollments/{userId} [delete]
func (h *ModuleHandlers) DeleteEnrollment(w http.ResponseWriter, r *http.Request) {
	actorID := middleware.CtxUserID(r.Context())
	actorRoles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid user ID", nil)
		return
	}

	if err := h.svc.DeleteEnrollment(r.Context(), actorID, actorRoles, moduleID, userID); err != nil {
		if errors.Is(err, service.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else if errors.Is(err, service.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "students can only unenroll themselves", nil)
		} else {
			writeError(w, 500, "server_error", "could not unenroll", nil)
		}
		return
	}

	w.WriteHeader(204)
}

// @Summary     List module enrollments
// @Description Get all enrollments for a module (members must be enrolled/teacher/admin)
// @Tags        enrollments
// @Produce     json
// @Security    BearerAuth
// @Param       moduleId path int true "Module ID"
// @Success     200 {array} dto.EnrollmentResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     500 {object} handlers.ErrorResponse
// @Router      /modules/{moduleId}/enrollments [get]
func (h *ModuleHandlers) ListEnrollments(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	enrollments, err := h.svc.GetEnrollmentsForModule(r.Context(), userID, roles, moduleID)
	if err != nil {
		if errors.Is(err, service.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "not enrolled in this module", nil)
		} else {
			writeError(w, 500, "server_error", "could not list enrollments", nil)
		}
		return
	}

	resp := make([]dto.EnrollmentResponse, len(enrollments))
	for i, e := range enrollments {
		resp[i] = dto.EnrollmentResponse{
			ID:        e.ID,
			ModuleID:  e.ModuleID,
			UserID:    e.UserID,
			Grade:     e.Grade,
			CreatedAt: e.CreatedAt,
			Feedback:  e.GradeFeedback,
			GradedAt:  e.GradedAt,
		}
	}
	writeJSON(w, 200, resp)
}

// @Summary     Grade enrollment
// @Description Set a student's module grade red/yellow/green with the mentor's module feedback (required, 1 to 1000
// @Description characters). A new grade replaces the old grade and feedback. Mentor of the student or admin only.
// @Tags        enrollments
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       moduleId path int true "Module ID"
// @Param       userId path int true "Student ID"
// @Param       request body dto.GradeEnrollmentRequest true "Grade"
// @Success     200 {object} dto.EnrollmentResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     422 {object} handlers.ErrorResponse
// @Router      /modules/{moduleId}/enrollments/{userId}/grade [patch]
func (h *ModuleHandlers) GradeEnrollment(w http.ResponseWriter, r *http.Request) {
	actorID := middleware.CtxUserID(r.Context())
	actorRoles := middleware.CtxRoles(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}
	studentID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid user ID", nil)
		return
	}

	var req dto.GradeEnrollmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	e, err := h.svc.UpdateEnrollmentGrade(r.Context(), actorID, actorRoles, moduleID, studentID, req.Grade, req.Feedback)
	if err != nil {
		if errors.Is(err, store.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "only mentors and admins can grade enrollments", nil)
		} else if errors.Is(err, service.ErrInvalidGrade) {
			writeError(w, 422, "validation_failed", err.Error(), nil)
		} else if errors.Is(err, service.ErrGradeFeedbackRequired) || errors.Is(err, service.ErrGradeFeedbackTooLong) {
			writeError(w, 422, "validation_failed", err.Error(), []ErrorDetail{{Field: "feedback", Message: err.Error()}})
		} else if errors.Is(err, store.ErrNotEnrolled) {
			writeError(w, 404, "not_found", "enrollment not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not grade enrollment", nil)
		}
		return
	}

	writeJSON(w, 200, dto.EnrollmentResponse{
		ID:        e.ID,
		ModuleID:  e.ModuleID,
		UserID:    e.UserID,
		Grade:     e.Grade,
		CreatedAt: e.CreatedAt,
		Feedback:  e.GradeFeedback,
		GradedAt:  e.GradedAt,
	})
}
