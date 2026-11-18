package handlers

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// Combined reads: one request for many weeks (or tasks) instead of one request each. Each answer is an object keyed
// by the week's (or task's) id, holding exactly what the matching per-week route would answer for it. The rules of
// those routes apply unchanged: a week the caller may not read (or that does not exist) is simply left out.

// maxBatchIDs caps how many weeks or tasks one combined request may ask for.
const maxBatchIDs = 200

// BatchHandlers serves the combined reads.
type BatchHandlers struct {
	content     ContentService
	quizzes     QuizService
	modules     ModuleService
	submissions SubmissionService
}

// NewBatchHandlers creates the combined-read handlers on top of the per-week services.
func NewBatchHandlers(content ContentService, quizzes QuizService, modules ModuleService, submissions SubmissionService) *BatchHandlers {
	return &BatchHandlers{content: content, quizzes: quizzes, modules: modules, submissions: submissions}
}

// batchIDs reads ?name=1,2,3: positive ids, each once, at most maxBatchIDs. It answers 400 itself when they are wrong.
func batchIDs(w http.ResponseWriter, r *http.Request, name string) ([]int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "bad_request", name+" is required, e.g. ?"+name+"=1,2,3", nil)
		return nil, false
	}
	seen := map[int64]bool{}
	var ids []int64
	for _, part := range strings.Split(raw, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id < 1 {
			writeError(w, http.StatusBadRequest, "bad_request", name+" must be a comma-separated list of ids", nil)
			return nil, false
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) > maxBatchIDs {
		writeError(w, http.StatusBadRequest, "bad_request", name+" may hold at most 200 ids", nil)
		return nil, false
	}
	return ids, true
}

// leftOut reports the errors that make a per-week route answer 403 or 404: that week is left out of a combined read.
func leftOut(err error) bool {
	return errors.Is(err, store.ErrNotEnrolled) || errors.Is(err, store.ErrNotModuleTeacher) ||
		errors.Is(err, store.ErrModuleNotFound) || errors.Is(err, service.ErrQuizForbidden) ||
		errors.Is(err, service.ErrQuizStudentOnly) || errors.Is(err, store.ErrContentItemNotFound) ||
		errors.Is(err, store.ErrSubmissionNotFound)
}

// @Summary     Content items of many weeks
// @Description What GET /modules/{moduleId}/content-items answers, for every week in module_ids at once, keyed by week
// @Description id. A week the caller may not read is left out.
// @Tags        content-items
// @Produce     json
// @Security    BearerAuth
// @Param       module_ids query string true "Week ids, comma-separated (at most 200)"
// @Success     200 {object} map[string][]dto.ContentTreeNode
// @Failure     400 {object} handlers.ErrorResponse
// @Router      /content-items [get]
func (h *BatchHandlers) ContentItems(w http.ResponseWriter, r *http.Request) {
	ids, ok := batchIDs(w, r, "module_ids")
	if !ok {
		return
	}
	userID, roles := middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context())
	out := map[string][]dto.ContentTreeNode{}
	for _, id := range ids {
		items, err := h.content.GetContentItemsForModule(r.Context(), userID, roles, id)
		if err != nil {
			if leftOut(err) {
				continue
			}
			log.Printf("batch: content items of week %d: %v", id, err)
			writeError(w, http.StatusInternalServerError, "server_error", "could not list content items", nil)
			return
		}
		out[strconv.FormatInt(id, 10)] = buildContentTree(items)
	}
	writeJSON(w, http.StatusOK, out)
}

// @Summary     Quizzes of many weeks
// @Description What GET /modules/{moduleId}/quizzes answers, for every week in module_ids at once, keyed by week id.
// @Description A week the caller may not read the quizzes of is left out.
// @Tags        quizzes
// @Produce     json
// @Security    BearerAuth
// @Param       module_ids query string true "Week ids, comma-separated (at most 200)"
// @Success     200 {object} map[string][]dto.QuizResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Router      /quizzes [get]
func (h *BatchHandlers) Quizzes(w http.ResponseWriter, r *http.Request) {
	ids, ok := batchIDs(w, r, "module_ids")
	if !ok {
		return
	}
	userID, roles := middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context())
	out := map[string][]dto.QuizResponse{}
	for _, id := range ids {
		quizzes, err := h.quizzes.ListQuizzesForModule(r.Context(), userID, roles, id)
		if err != nil {
			if leftOut(err) {
				continue
			}
			log.Printf("batch: quizzes of week %d: %v", id, err)
			writeError(w, http.StatusInternalServerError, "server_error", "could not list quizzes", nil)
			return
		}
		if quizzes == nil {
			quizzes = []dto.QuizResponse{}
		}
		out[strconv.FormatInt(id, 10)] = quizzes
	}
	writeJSON(w, http.StatusOK, out)
}

// @Summary     Enrollments of many weeks
// @Description What GET /modules/{moduleId}/enrollments answers, for every week in module_ids at once, keyed by week
// @Description id (a student gets only their own row). A week the caller may not read is left out.
// @Tags        enrollments
// @Produce     json
// @Security    BearerAuth
// @Param       module_ids query string true "Week ids, comma-separated (at most 200)"
// @Success     200 {object} map[string][]dto.EnrollmentResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Router      /enrollments [get]
func (h *BatchHandlers) Enrollments(w http.ResponseWriter, r *http.Request) {
	ids, ok := batchIDs(w, r, "module_ids")
	if !ok {
		return
	}
	userID, roles := middleware.CtxUserID(r.Context()), middleware.CtxRoles(r.Context())
	out := map[string][]dto.EnrollmentResponse{}
	for _, id := range ids {
		enrollments, err := h.modules.GetEnrollmentsForModule(r.Context(), userID, roles, id)
		if err != nil {
			if leftOut(err) {
				continue
			}
			log.Printf("batch: enrollments of week %d: %v", id, err)
			writeError(w, http.StatusInternalServerError, "server_error", "could not list enrollments", nil)
			return
		}
		out[strconv.FormatInt(id, 10)] = enrollmentResponses(enrollments)
	}
	writeJSON(w, http.StatusOK, out)
}

// @Summary     My hand-ins for many tasks
// @Description What GET /modules/{moduleId}/content-items/{assignmentId}/submissions/me answers (the newest hand-in
// @Description with its history), for every task in assignment_ids at once, keyed by task id. A task without a
// @Description hand-in is left out.
// @Tags        submissions
// @Produce     json
// @Security    BearerAuth
// @Param       assignment_ids query string true "Task ids, comma-separated (at most 200)"
// @Success     200 {object} map[string]dto.SubmissionResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Router      /submissions/me [get]
func (h *BatchHandlers) MySubmissions(w http.ResponseWriter, r *http.Request) {
	ids, ok := batchIDs(w, r, "assignment_ids")
	if !ok {
		return
	}
	userID := middleware.CtxUserID(r.Context())
	out := map[string]dto.SubmissionResponse{}
	for _, id := range ids {
		s, err := h.submissions.GetSubmissionByStudentAndAssignment(r.Context(), userID, id)
		if err != nil {
			if leftOut(err) {
				continue
			}
			log.Printf("batch: my hand-in for task %d: %v", id, err)
			writeError(w, http.StatusInternalServerError, "server_error", "could not get submissions", nil)
			return
		}
		resp := submissionVersion(*s)
		for _, old := range s.History {
			resp.History = append(resp.History, submissionVersion(old))
		}
		out[strconv.FormatInt(id, 10)] = resp
	}
	writeJSON(w, http.StatusOK, out)
}
