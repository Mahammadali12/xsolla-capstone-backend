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

type CourseHandlers struct {
	svc *service.CourseService
}

// NewCourseHandlers creates course handlers backed by the given service.
func NewCourseHandlers(svc *service.CourseService) *CourseHandlers {
	return &CourseHandlers{svc: svc}
}

// POST /courses
func (h *CourseHandlers) CreateCourse(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	if role != "teacher" && role != "admin" {
		writeError(w, 403, "forbidden", "only teachers and admins can create courses", nil)
		return
	}

	var req dto.CreateCourseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	if req.Title == "" {
		writeError(w, 400, "bad_request", "title is required", nil)
		return
	}

	id, title, description, err := h.svc.CreateCourse(r.Context(), userID, req.Title, req.Description)
	if err != nil {
		writeError(w, 500, "server_error", "could not create course", nil)
		return
	}

	writeJSON(w, 201, dto.CourseResponse{
		ID:          id,
		Title:       title,
		Description: description,
		CreatedBy:   userID,
	})
}

// GET /courses
func (h *CourseHandlers) ListCourses(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	courses, err := h.svc.GetCoursesForUser(r.Context(), userID, role)
	if err != nil {
		writeError(w, 500, "server_error", "could not list courses", nil)
		return
	}

	resp := make([]dto.CourseResponse, len(courses))
	for i, c := range courses {
		resp[i] = dto.CourseResponse{
			ID:          c.ID,
			Title:       c.Title,
			Description: c.Description,
			CreatedBy:   c.CreatedBy,
			CreatedAt:   c.CreatedAt,
		}
	}
	writeJSON(w, 200, resp)
}

// GET /courses/:id
func (h *CourseHandlers) GetCourse(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	courseID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid course ID", nil)
		return
	}

	c, err := h.svc.GetCourseByID(r.Context(), userID, role, courseID)
	if err != nil {
		if errors.Is(err, service.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "not enrolled in this course", nil)
		} else if errors.Is(err, service.ErrCourseNotFound) {
			writeError(w, 404, "not_found", "course not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not get course", nil)
		}
		return
	}

	writeJSON(w, 200, dto.CourseResponse{
		ID:          c.ID,
		Title:       c.Title,
		Description: c.Description,
		CreatedBy:   c.CreatedBy,
		CreatedAt:   c.CreatedAt,
	})
}

// PATCH /courses/:id
func (h *CourseHandlers) PatchCourse(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	courseID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid course ID", nil)
		return
	}

	var req dto.UpdateCourseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	c, err := h.svc.PatchCourse(r.Context(), userID, role, courseID, req.Title, req.Description)
	if err != nil {
		if errors.Is(err, service.ErrNotCourseTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this course", nil)
		} else if errors.Is(err, service.ErrCourseNotFound) {
			writeError(w, 404, "not_found", "course not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not update course", nil)
		}
		return
	}

	writeJSON(w, 200, dto.CourseResponse{
		ID:          c.ID,
		Title:       c.Title,
		Description: c.Description,
		CreatedBy:   c.CreatedBy,
		CreatedAt:   c.CreatedAt,
	})
}

// DELETE /courses/:id
func (h *CourseHandlers) DeleteCourse(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	courseID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid course ID", nil)
		return
	}

	if err := h.svc.DeleteCourse(r.Context(), userID, role, courseID); err != nil {
		if errors.Is(err, service.ErrNotCourseTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this course", nil)
		} else if errors.Is(err, service.ErrCourseNotFound) {
			writeError(w, 404, "not_found", "course not found", nil)
		} else {
			writeError(w, 500, "server_error", "could not delete course", nil)
		}
		return
	}

	w.WriteHeader(204)
}

// POST /courses/:courseId/enrollments
func (h *CourseHandlers) CreateEnrollment(w http.ResponseWriter, r *http.Request) {
	actorID := middleware.CtxUserID(r.Context())
	actorRole := middleware.CtxRole(r.Context())

	courseID, err := strconv.ParseInt(r.PathValue("courseId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid course ID", nil)
		return
	}

	var req dto.EnrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}
	userID := req.UserID

	e, err := h.svc.CreateEnrollment(r.Context(), actorID, actorRole, courseID, userID)
	if err != nil {
		if errors.Is(err, service.ErrNotCourseTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this course", nil)
		} else if errors.Is(err, service.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "students can only enroll themselves", nil)
		} else if errors.Is(err, service.ErrAlreadyEnrolled) {
			writeError(w, 409, "conflict", "already enrolled in this course", nil)
		} else {
			writeError(w, 500, "server_error", "could not enroll", nil)
		}
		return
	}

	writeJSON(w, 201, dto.EnrollmentResponse{
		ID:        e.ID,
		CourseID:  e.CourseID,
		UserID:    e.UserID,
		CreatedAt: e.CreatedAt,
	})
}

// DELETE /courses/:courseId/enrollments/:userId
func (h *CourseHandlers) DeleteEnrollment(w http.ResponseWriter, r *http.Request) {
	actorID := middleware.CtxUserID(r.Context())
	actorRole := middleware.CtxRole(r.Context())

	courseID, err := strconv.ParseInt(r.PathValue("courseId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid course ID", nil)
		return
	}
	userID, err := strconv.ParseInt(r.PathValue("userId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid user ID", nil)
		return
	}

	if err := h.svc.DeleteEnrollment(r.Context(), actorID, actorRole, courseID, userID); err != nil {
		if errors.Is(err, service.ErrNotCourseTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this course", nil)
		} else if errors.Is(err, service.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "students can only unenroll themselves", nil)
		} else {
			writeError(w, 500, "server_error", "could not unenroll", nil)
		}
		return
	}

	w.WriteHeader(204)
}

// GET /courses/:courseId/enrollments
func (h *CourseHandlers) ListEnrollments(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	role := middleware.CtxRole(r.Context())

	courseID, err := strconv.ParseInt(r.PathValue("courseId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid course ID", nil)
		return
	}

	enrollments, err := h.svc.GetEnrollmentsForCourse(r.Context(), userID, role, courseID)
	if err != nil {
		if errors.Is(err, service.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "not enrolled in this course", nil)
		} else {
			writeError(w, 500, "server_error", "could not list enrollments", nil)
		}
		return
	}

	resp := make([]dto.EnrollmentResponse, len(enrollments))
	for i, e := range enrollments {
		resp[i] = dto.EnrollmentResponse{
			ID:        e.ID,
			CourseID:  e.CourseID,
			UserID:    e.UserID,
			CreatedAt: e.CreatedAt,
		}
	}
	writeJSON(w, 200, resp)
}
