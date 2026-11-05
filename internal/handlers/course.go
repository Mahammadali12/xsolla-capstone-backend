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

// @Summary     Create course
// @Description Create a new course (teacher or admin only). The creator is auto-enrolled as teacher.
// @Tags        courses
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       request body dto.CreateCourseRequest true "Course details"
// @Success     201 {object} dto.CourseResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     500 {object} handlers.ErrorResponse
// @Router      /courses [post]
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

// @Summary     List courses
// @Description Get courses the user is enrolled in (student), teaching (teacher), or all (admin)
// @Tags        courses
// @Produce     json
// @Security    BearerAuth
// @Success     200 {array} dto.CourseResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     500 {object} handlers.ErrorResponse
// @Router      /courses [get]
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

// @Summary     Get course by ID
// @Description Get course details if the user is enrolled or is the teacher
// @Tags        courses
// @Produce     json
// @Security    BearerAuth
// @Param       id path int true "Course ID"
// @Success     200 {object} dto.CourseResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /courses/{id} [get]
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

// @Summary     Update course
// @Description Update course title/description (teacher or admin only)
// @Tags        courses
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id path int true "Course ID"
// @Param       request body dto.UpdateCourseRequest true "Course details"
// @Success     200 {object} dto.CourseResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /courses/{id} [patch]
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

// @Summary     Delete course
// @Description Soft-delete a course (teacher or admin only)
// @Tags        courses
// @Security    BearerAuth
// @Param       id path int true "Course ID"
// @Success     204 {string} string "No content"
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /courses/{id} [delete]
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

// @Summary     Enroll in course
// @Description Enroll a user in a course. Students can only enroll themselves, teachers/admins can enroll anyone
// @Tags        enrollments
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       courseId path int true "Course ID"
// @Param       request body dto.EnrollRequest true "User ID to enroll"
// @Success     201 {object} dto.EnrollmentResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     409 {object} handlers.ErrorResponse
// @Router      /courses/{courseId}/enrollments [post]
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

// @Summary     Unenroll from course
// @Description Remove a user from a course. Students can only unenroll themselves
// @Tags        enrollments
// @Security    BearerAuth
// @Param       courseId path int true "Course ID"
// @Param       userId path int true "User ID"
// @Success     204 {string} string "No content"
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /courses/{courseId}/enrollments/{userId} [delete]
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

// @Summary     List course enrollments
// @Description Get all enrollments for a course (members must be enrolled/teacher/admin)
// @Tags        enrollments
// @Produce     json
// @Security    BearerAuth
// @Param       courseId path int true "Course ID"
// @Success     200 {array} dto.EnrollmentResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     500 {object} handlers.ErrorResponse
// @Router      /courses/{courseId}/enrollments [get]
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
