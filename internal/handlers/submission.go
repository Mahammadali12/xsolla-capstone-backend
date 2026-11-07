package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/store"
)

// SubmissionService is the interface that submission handlers depend on.
type SubmissionService interface {
	CreateSubmission(ctx context.Context, studentID int64, courseID int64, contentItemID int64, textBody string, fileIDs []int64) (*store.Submission, error)
	GetSubmissionByID(ctx context.Context, userID int64, roles []string, submissionID int64) (*store.Submission, error)
	GetSubmissionByStudentAndAssignment(ctx context.Context, studentID int64, contentItemID int64) (*store.Submission, error)
	GetSubmissionsForAssignment(ctx context.Context, userID int64, roles []string, contentItemID int64) ([]store.Submission, error)
	UpdateSubmissionGrade(ctx context.Context, userID int64, roles []string, submissionID int64, grade *float64, feedback string) (*store.Submission, error)
	DeleteSubmission(ctx context.Context, userID int64, roles []string, submissionID int64) error
}

type SubmissionHandlers struct {
	svc SubmissionService
}

// NewSubmissionHandlers creates submission handlers backed by the given service.
func NewSubmissionHandlers(svc SubmissionService) *SubmissionHandlers {
	return &SubmissionHandlers{svc: svc}
}

// POST /modules/:moduleId/content-items/:assignmentId/submissions
// For students to create a submission into assignment
func (h *SubmissionHandlers) CreateSubmission(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())

	courseID, err := strconv.ParseInt(r.PathValue("moduleId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid module ID", nil)
		return
	}
	contentItemID, err := strconv.ParseInt(r.PathValue("assignmentId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid assignment ID", nil)
		return
	}

	var req dto.CreateSubmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	ci, err := h.svc.CreateSubmission(r.Context(), userID, courseID, contentItemID, req.TextBody, req.FileIDs)
	if err != nil {
		log.Printf("submission handler: CreateSubmission error: %v", err)
		switch {
		case errors.Is(err, store.ErrNotEnrolled):
			writeError(w, 403, "forbidden", "not enrolled in this module", nil)
		case errors.Is(err, store.ErrContentItemNotFound):
			writeError(w, 404, "not_found", "assignment not found", nil)
		case errors.Is(err, errors.New("file does not belong to student")):
			writeError(w, 400, "bad_request", err.Error(), nil)
		default:
			writeError(w, 500, "server_error", "could not create submission", nil)
		}
		return
	}

	writeJSON(w, 201, dto.SubmissionResponse{
		ID:           ci.ID,
		ContentItemID: ci.ContentItemID,
		StudentID:    ci.StudentID,
		TextBody:     ci.TextBody,
		SubmittedAt:  ci.SubmittedAt,
		Grade:        ci.Grade,
		Feedback:     feedbackString(ci.Feedback),
		GradedBy:     ci.GradedBy,
		GradedAt:     ci.GradedAt,
		CreatedAt:    ci.CreatedAt,
		Files:        dto.FileList(ci.Files),
	})
}

// GET /modules/:moduleId/content-items/:assignmentId/submissions
func (h *SubmissionHandlers) ListSubmissions(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	contentItemID, err := strconv.ParseInt(r.PathValue("assignmentId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid assignment ID", nil)
		return
	}

	submissions, err := h.svc.GetSubmissionsForAssignment(r.Context(), userID, roles, contentItemID)
	if err != nil {
		log.Printf("submission handler: ListSubmissions error: %v", err)
		if errors.Is(err, store.ErrContentItemNotFound) {
			writeError(w, 404, "not_found", "assignment not found", nil)
		} else if errors.Is(err, store.ErrNotModuleTeacher) {
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		} else if errors.Is(err, store.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "not enrolled in this module", nil)
		} else {
			writeError(w, 500, "server_error", "could not list submissions", nil)
		}
		return
	}

	resp := make([]dto.SubmissionResponse, len(submissions))
	for i, s := range submissions {
		resp[i] = dto.SubmissionResponse{
			ID:           s.ID,
			ContentItemID: s.ContentItemID,
			StudentID:    s.StudentID,
			TextBody:     s.TextBody,
			SubmittedAt:  s.SubmittedAt,
			Grade:        s.Grade,
			Feedback:     feedbackString(s.Feedback),
			GradedBy:     s.GradedBy,
			GradedAt:     s.GradedAt,
			CreatedAt:    s.CreatedAt,
			Files:        dto.FileList(s.Files),
		}
	}
	writeJSON(w, 200, resp)
}

// GET /modules/:moduleId/content-items/:assignmentId/submissions/me
func (h *SubmissionHandlers) GetMySubmission(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())

	contentItemID, err := strconv.ParseInt(r.PathValue("assignmentId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid assignment ID", nil)
		return
	}

	s, err := h.svc.GetSubmissionByStudentAndAssignment(r.Context(), userID, contentItemID)
	if err != nil {
		if errors.Is(err, store.ErrSubmissionNotFound) {
			writeError(w, 404, "not_found", "no submission found", nil)
		} else {
			log.Printf("submission handler: GetMySubmission error: %v", err)
			writeError(w, 500, "server_error", "could not get submission", nil)
		}
		return
	}

	writeJSON(w, 200, dto.SubmissionResponse{
		ID:           s.ID,
		ContentItemID: s.ContentItemID,
		StudentID:    s.StudentID,
		TextBody:     s.TextBody,
		SubmittedAt:  s.SubmittedAt,
		Grade:        s.Grade,
		Feedback:     feedbackString(s.Feedback),
		GradedBy:     s.GradedBy,
		GradedAt:     s.GradedAt,
		CreatedAt:    s.CreatedAt,
		Files:        dto.FileList(s.Files),
	})
}

// POST /submissions/:id/grade
func (h *SubmissionHandlers) GradeSubmission(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	submissionID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid submission ID", nil)
		return
	}

	var req dto.GradeSubmissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	grade := req.Grade
	s, err := h.svc.UpdateSubmissionGrade(r.Context(), userID, roles, submissionID, &grade, req.Feedback)
	if err != nil {
		log.Printf("submission handler: GradeSubmission error: %v", err)
		switch {
		case errors.Is(err, store.ErrNotModuleTeacher):
			writeError(w, 403, "forbidden", "not the teacher of this module", nil)
		case errors.Is(err, store.ErrSubmissionNotFound):
			writeError(w, 404, "not_found", "submission not found", nil)
		default:
			writeError(w, 500, "server_error", "could not grade submission", nil)
		}
		return
	}

	writeJSON(w, 200, dto.SubmissionResponse{
		ID:           s.ID,
		ContentItemID: s.ContentItemID,
		StudentID:    s.StudentID,
		TextBody:     s.TextBody,
		SubmittedAt:  s.SubmittedAt,
		Grade:        s.Grade,
		Feedback:     feedbackString(s.Feedback),
		GradedBy:     s.GradedBy,
		GradedAt:     s.GradedAt,
		CreatedAt:    s.CreatedAt,
		Files:        dto.FileList(s.Files),
	})
}

func feedbackString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// GET /submissions/:id/files — list files attached to a submission
func (h *SubmissionHandlers) GetSubmissionFiles(w http.ResponseWriter, r *http.Request) {
	userID := middleware.CtxUserID(r.Context())
	roles := middleware.CtxRoles(r.Context())

	submissionID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid submission ID", nil)
		return
	}

	sub, err := h.svc.GetSubmissionByID(r.Context(), userID, roles, submissionID)
	if err != nil {
		if errors.Is(err, store.ErrSubmissionNotFound) {
			writeError(w, 404, "not_found", "submission not found", nil)
		} else if errors.Is(err, store.ErrNotModuleTeacher) || errors.Is(err, store.ErrNotEnrolled) {
			writeError(w, 403, "forbidden", "cannot access this submission", nil)
		} else {
			log.Printf("submission handler: GetSubmissionFiles error: %v", err)
			writeError(w, 500, "server_error", "could not get submission files", nil)
		}
		return
	}

	writeJSON(w, 200, dto.FileList(sub.Files))
}
