package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
)

type ModuleHandlers struct {
	svc *service.ModuleService
}

// NewModuleHandlers creates module handlers backed by the given service.
func NewModuleHandlers(svc *service.ModuleService) *ModuleHandlers {
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
	role := middleware.CtxRole(r.Context())

	if role != "teacher" && role != "admin" {
		writeError(w, 403, "forbidden", "only teachers and admins can create modules", nil)
		return
	}

	var req dto.CreateModuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	if req.Title == "" {
		writeError(w, 400, "bad_request", "title is required", nil)
		return
	}

	id, title, description, err := h.svc.CreateModule(r.Context(), userID, req.Title, req.Description)
	if err != nil {
		writeError(w, 500, "server_error", "could not create module", nil)
		return
	}

	writeJSON(w, 201, dto.ModuleResponse{
		ID:          id,
		Title:       title,
		Description: description,
		CreatedBy:   userID,
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
	role := middleware.CtxRole(r.Context())

	modules, err := h.svc.GetModulesForUser(r.Context(), userID, role)
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
	role := middleware.CtxRole(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	m, err := h.svc.GetModuleByID(r.Context(), userID, role, moduleID)
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
	})
}

// @Summary     Update module
// @Description Update module title/description (teacher or admin only)
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
	role := middleware.CtxRole(r.Context())

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

	m, err := h.svc.PatchModule(r.Context(), userID, role, moduleID, req.Title, req.Description)
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
	})
}

// @Summary     Delete module
// @Description Soft-delete a module (teacher or admin only)
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
	role := middleware.CtxRole(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	if err := h.svc.DeleteModule(r.Context(), userID, role, moduleID); err != nil {
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
	actorRole := middleware.CtxRole(r.Context())

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

	e, err := h.svc.CreateEnrollment(r.Context(), actorID, actorRole, moduleID, userID)
	if err != nil {
		if errors.Is(err, service.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else if errors.Is(err, service.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "students can only enroll themselves", nil)
		} else if errors.Is(err, service.ErrAlreadyEnrolled) {
			writeError(w, 409, "conflict", "already enrolled in this module", nil)
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
	actorRole := middleware.CtxRole(r.Context())

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

	if err := h.svc.DeleteEnrollment(r.Context(), actorID, actorRole, moduleID, userID); err != nil {
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
	role := middleware.CtxRole(r.Context())

	moduleID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}

	enrollments, err := h.svc.GetEnrollmentsForModule(r.Context(), userID, role, moduleID)
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
			CreatedAt: e.CreatedAt,
		}
	}
	writeJSON(w, 200, resp)
}
