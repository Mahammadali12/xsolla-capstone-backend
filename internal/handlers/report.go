package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"xsolla-capstone-api/internal/dto"
	"xsolla-capstone-api/internal/middleware"
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// maxReportBody is far above the largest valid report (a 200-character subject and a 5000-character message).
const maxReportBody = 64 << 10

// ReportService is what the report routes need.
type ReportService interface {
	Send(ctx context.Context, reporterID int64, subject, message string) (*store.Report, error)
	List(ctx context.Context, f store.ReportFilter) ([]store.Report, error)
	Get(ctx context.Context, id int64) (*store.Report, error)
	Counts(ctx context.Context) (open, resolved int, err error)
	Update(ctx context.Context, adminID, id int64, status, adminNote *string) (*store.Report, error)
	Delete(ctx context.Context, id int64) error
}

// ReportHandlers serves the reports: anyone signed in sends one, only admins read and handle them.
type ReportHandlers struct {
	svc ReportService
}

// NewReportHandlers creates the report handlers.
func NewReportHandlers(svc ReportService) *ReportHandlers {
	return &ReportHandlers{svc: svc}
}

func reportPerson(u *store.UserBrief, roles []string) *dto.ReportPerson {
	if u == nil {
		return nil
	}
	return &dto.ReportPerson{ID: u.ID, Name: u.Name, Email: u.Email, Roles: roles}
}

func toReportResponse(r *store.Report) dto.ReportResponse {
	return dto.ReportResponse{
		ID:         r.ID,
		Subject:    r.Subject,
		Message:    r.Message,
		Status:     r.Status,
		Reporter:   reportPerson(r.Reporter, r.Roles),
		AdminNote:  r.AdminNote,
		ResolvedBy: reportPerson(r.ResolvedBy, nil),
		ResolvedAt: r.ResolvedAt,
		CreatedAt:  r.CreatedAt,
	}
}

// decodeReportBody reads one JSON object of reasonable size into v.
func decodeReportBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxReportBody))
	err := dec.Decode(v)
	if err == nil && dec.Decode(&struct{}{}) != io.EOF {
		err = errors.New("trailing data after the JSON object")
	}
	if err == nil {
		return true
	}
	var tooLarge *http.MaxBytesError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "the report is too large", nil)
	case errors.As(err, &typeErr) && typeErr.Field != "":
		msg := typeErr.Field + " has the wrong type"
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", msg, []ErrorDetail{{Field: typeErr.Field, Message: msg}})
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", nil)
	}
	return false
}

func writeReportError(w http.ResponseWriter, err error) {
	var invalid *service.ReportInvalidError
	switch {
	case errors.As(err, &invalid):
		details := make([]ErrorDetail, len(invalid.Fields))
		for i, f := range invalid.Fields {
			details[i] = ErrorDetail{Field: f.Field, Message: f.Message}
		}
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", invalid.Error(), details)
	case errors.Is(err, service.ErrReportNotFound):
		writeError(w, http.StatusNotFound, "not_found", "report not found", nil)
	default:
		writeError(w, http.StatusInternalServerError, "server_error", "could not handle the report", nil)
	}
}

func reportID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid report ID", nil)
		return 0, false
	}
	return id, true
}

// @Summary     Send a report
// @Description Report a problem to the admins, e.g. a student's behaviour. Anyone signed in (student, mentor,
// @Description lecturer, admin) can send one. Only admins can read reports; the sender gets their own report back.
// @Tags        reports
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       request body dto.SendReportRequest true "subject (1-200 characters) and message (1-5000)"
// @Success     201 {object} dto.SentReportResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     401 {object} handlers.ErrorResponse
// @Failure     413 {object} handlers.ErrorResponse
// @Failure     422 {object} handlers.ErrorResponse "every problem with the form, in details"
// @Router      /reports [post]
func (h *ReportHandlers) Send(w http.ResponseWriter, r *http.Request) {
	var req dto.SendReportRequest
	if !decodeReportBody(w, r, &req) {
		return
	}
	rep, err := h.svc.Send(r.Context(), middleware.CtxUserID(r.Context()), req.Subject, req.Message)
	if err != nil {
		writeReportError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dto.SentReportResponse{ID: rep.ID, Subject: rep.Subject, Message: rep.Message, CreatedAt: rep.CreatedAt})
}

// @Summary     List reports
// @Description Every report, newest first, with who sent it (admin only).
// @Tags        reports
// @Produce     json
// @Security    BearerAuth
// @Param       status      query string false "open or resolved"
// @Param       role        query string false "the sender has this role: student, mentor, lecturer or admin"
// @Param       reporter_id query int    false "sent by this user"
// @Param       q           query string false "words in the subject or the message"
// @Success     200 {array} dto.ReportResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     422 {object} handlers.ErrorResponse
// @Router      /reports [get]
func (h *ReportHandlers) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.ReportFilter{Status: q.Get("status"), Role: q.Get("role"), Query: q.Get("q")}
	if raw := q.Get("reporter_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			writeError(w, http.StatusBadRequest, "bad_request", "reporter_id must be a user ID", nil)
			return
		}
		f.ReporterID = id
	}
	list, err := h.svc.List(r.Context(), f)
	if err != nil {
		writeReportError(w, err)
		return
	}
	resp := make([]dto.ReportResponse, len(list))
	for i := range list {
		resp[i] = toReportResponse(&list[i])
	}
	writeJSON(w, http.StatusOK, resp)
}

// @Summary     Report counts
// @Description How many reports are open and how many resolved, e.g. for a badge (admin only).
// @Tags        reports
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} dto.ReportCountsResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Router      /reports/summary [get]
func (h *ReportHandlers) Summary(w http.ResponseWriter, r *http.Request) {
	open, resolved, err := h.svc.Counts(r.Context())
	if err != nil {
		writeReportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto.ReportCountsResponse{Open: open, Resolved: resolved, Total: open + resolved})
}

// @Summary     Get a report
// @Description One report with who sent it and how it was handled (admin only).
// @Tags        reports
// @Produce     json
// @Security    BearerAuth
// @Param       id path int true "Report ID"
// @Success     200 {object} dto.ReportResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /reports/{id} [get]
func (h *ReportHandlers) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := reportID(w, r)
	if !ok {
		return
	}
	rep, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeReportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toReportResponse(rep))
}

// @Summary     Handle a report
// @Description Mark a report resolved or open again, and/or write a note on it (admin only). Resolving records who
// @Description resolved it and when; reopening clears that. An empty admin_note removes the note.
// @Tags        reports
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       id path int true "Report ID"
// @Param       request body dto.UpdateReportRequest true "status and/or admin_note"
// @Success     200 {object} dto.ReportResponse
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Failure     422 {object} handlers.ErrorResponse
// @Router      /reports/{id} [patch]
func (h *ReportHandlers) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := reportID(w, r)
	if !ok {
		return
	}
	var req dto.UpdateReportRequest
	if !decodeReportBody(w, r, &req) {
		return
	}
	rep, err := h.svc.Update(r.Context(), middleware.CtxUserID(r.Context()), id, req.Status, req.AdminNote)
	if err != nil {
		writeReportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toReportResponse(rep))
}

// @Summary     Delete a report
// @Description Removes a report for good (admin only).
// @Tags        reports
// @Security    BearerAuth
// @Param       id path int true "Report ID"
// @Success     204
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /reports/{id} [delete]
func (h *ReportHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id, ok := reportID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeReportError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
