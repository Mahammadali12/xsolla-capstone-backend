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
	"xsolla-capstone-api/internal/service"
	"xsolla-capstone-api/internal/store"
)

// TeamService is the interface that team handlers depend on.
type TeamService interface {
	ListTeams(ctx context.Context) ([]store.Team, error)
	ReplaceTeams(ctx context.Context, inputs []store.TeamInput) ([]store.Team, error)
	AddTeamStudents(ctx context.Context, teamID int64, studentIDs []int64) error
	DeleteTeamStudents(ctx context.Context, teamID int64, studentIDs []int64) error
	DeleteTeamStudent(ctx context.Context, teamID int64, studentID int64) error
	DeleteTeam(ctx context.Context, id int64) error
	PutTeamMentor(ctx context.Context, teamID int64, mentorID *int64) error
	PatchTeam(ctx context.Context, id int64, name *string, mentorID *int64) (*store.Team, error)
	CreateTeam(ctx context.Context, name *string, mentorID *int64, studentIDs []int64) (*store.Team, error)
	GetMyTeam(ctx context.Context, studentID int64) (*store.TeamDetail, error)
	GetMentorTeam(ctx context.Context, mentorID int64) (*store.TeamDetail, error)
	Counts(ctx context.Context) (teamCount int, studentCount int, err error)
}

type TeamHandlers struct {
	svc TeamService
	avatarSupport
}

// NewTeamHandlers creates team handlers backed by the given service.
func NewTeamHandlers(svc TeamService) *TeamHandlers {
	return &TeamHandlers{svc: svc}
}

// GET /teams — admin only.
func (h *TeamHandlers) ListTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := h.svc.ListTeams(r.Context())
	if err != nil {
		log.Printf("team handler: ListTeams error: %v", err)
		writeError(w, 500, "server_error", "could not list teams", nil)
		return
	}

	resp := make([]dto.TeamResponse, len(teams))
	for i, t := range teams {
		resp[i] = dto.TeamResponse{
			ID:         t.ID,
			Name:       t.Name,
			MentorID:   t.MentorID,
			StudentIDs: t.StudentIDs,
		}
	}
	writeJSON(w, 200, resp)
}

// PUT /teams — admin only. Replace every team in one transaction.
func (h *TeamHandlers) ReplaceTeams(w http.ResponseWriter, r *http.Request) {
	var req dto.PutTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	inputs := make([]store.TeamInput, len(req.Teams))
	for i, t := range req.Teams {
		inputs[i] = store.TeamInput{
			MentorID:   t.MentorID,
			StudentIDs: t.StudentIDs,
		}
	}

	teams, err := h.svc.ReplaceTeams(r.Context(), inputs)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTeamConfig),
			errors.Is(err, service.ErrDuplicateStudent),
			errors.Is(err, service.ErrDuplicateMentor):
			writeError(w, 422, "validation_failed", err.Error(), nil)
		default:
			log.Printf("team handler: ReplaceTeams error: %v", err)
			writeError(w, 500, "server_error", "could not replace teams", nil)
		}
		return
	}

	resp := make([]dto.TeamResponse, len(teams))
	for i, t := range teams {
		resp[i] = dto.TeamResponse{
			ID:         t.ID,
			Name:       t.Name,
			MentorID:   t.MentorID,
			StudentIDs: t.StudentIDs,
		}
	}
	writeJSON(w, 200, resp)
}

// POST /teams — admin only. Create a single team.
// name is required; students must not already belong to another team.
func (h *TeamHandlers) CreateTeam(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	team, err := h.svc.CreateTeam(r.Context(), req.Name, req.MentorID, req.StudentIDs)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidTeamConfig),
			errors.Is(err, service.ErrStudentInAnotherTeam):
			writeError(w, 422, "validation_failed", err.Error(), nil)
		default:
			log.Printf("team handler: CreateTeam error: %v", err)
			writeError(w, 500, "server_error", "could not create team", nil)
		}
		return
	}

	writeJSON(w, 201, dto.TeamResponse{
		ID:         team.ID,
		Name:       team.Name,
		MentorID:   team.MentorID,
		StudentIDs: team.StudentIDs,
	})
}

// POST /teams/{id}/student — admin only. Add students to a team.
// Students must not already belong to another team (no auto-removal).
func (h *TeamHandlers) AddTeamStudents(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid team ID", nil)
		return
	}

	var req dto.PutTeamStudentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	if err := h.svc.AddTeamStudents(r.Context(), teamID, req.StudentIDs); err != nil {
		switch {
		case errors.Is(err, store.ErrTeamNotFound):
			writeError(w, 404, "not_found", "team not found", nil)
		case errors.Is(err, service.ErrInvalidTeamConfig):
			writeError(w, 422, "validation_failed", err.Error(), nil)
		default:
			log.Printf("team handler: AddTeamStudents error: %v", err)
			writeError(w, 500, "server_error", "could not update team students", nil)
		}
		return
	}
	w.WriteHeader(204)
}

// @Summary     Delete a team
// @Description Deletes one team (admin only). Its students and its mentor stay; they are just no longer in a team.
// @Description Past mentor reviews are kept.
// @Tags        teams
// @Security    BearerAuth
// @Param       id path int true "Team ID"
// @Success     204
// @Failure     400 {object} handlers.ErrorResponse
// @Failure     403 {object} handlers.ErrorResponse
// @Failure     404 {object} handlers.ErrorResponse
// @Router      /teams/{id} [delete]
func (h *TeamHandlers) DeleteTeam(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid team ID", nil)
		return
	}
	if err := h.svc.DeleteTeam(r.Context(), teamID); err != nil {
		switch {
		case errors.Is(err, store.ErrTeamNotFound):
			writeError(w, 404, "not_found", "team not found", nil)
		default:
			log.Printf("team handler: DeleteTeam error: %v", err)
			writeError(w, 500, "server_error", "could not delete team", nil)
		}
		return
	}
	w.WriteHeader(204)
}

// DELETE /teams/{id}/students/{studentId} — admin only. Remove one student from a team.
func (h *TeamHandlers) DeleteTeamStudent(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid team ID", nil)
		return
	}
	studentID, err := strconv.ParseInt(r.PathValue("studentId"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid student ID", nil)
		return
	}

	if err := h.svc.DeleteTeamStudent(r.Context(), teamID, studentID); err != nil {
		switch {
		case errors.Is(err, store.ErrTeamNotFound):
			writeError(w, 404, "not_found", "team not found", nil)
		default:
			log.Printf("team handler: DeleteTeamStudent error: %v", err)
			writeError(w, 500, "server_error", "could not remove student from team", nil)
		}
		return
	}
	w.WriteHeader(204)
}

// DELETE /teams/{id}/students — admin only. Remove students from a team.
func (h *TeamHandlers) DeleteTeamStudents(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid team ID", nil)
		return
	}

	var req dto.DeleteTeamStudentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	if err := h.svc.DeleteTeamStudents(r.Context(), teamID, req.StudentIDs); err != nil {
		switch {
		case errors.Is(err, store.ErrTeamNotFound):
			writeError(w, 404, "not_found", "team not found", nil)
		case errors.Is(err, service.ErrInvalidTeamConfig):
			writeError(w, 422, "validation_failed", err.Error(), nil)
		default:
			log.Printf("team handler: DeleteTeamStudents error: %v", err)
			writeError(w, 500, "server_error", "could not remove team students", nil)
		}
		return
	}
	w.WriteHeader(204)
}

// PATCH /teams/{id} — admin only. Update a single team's name and/or mentor_id in place.
func (h *TeamHandlers) PatchTeam(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid team ID", nil)
		return
	}

	var req dto.PatchTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	team, err := h.svc.PatchTeam(r.Context(), teamID, req.Name, req.MentorID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTeamNotFound):
			writeError(w, 404, "not_found", "team not found", nil)
		case errors.Is(err, service.ErrInvalidTeamConfig):
			writeError(w, 422, "validation_failed", err.Error(), nil)
		default:
			log.Printf("team handler: PatchTeam error: %v", err)
			writeError(w, 500, "server_error", "could not update team", nil)
		}
		return
	}

	writeJSON(w, 200, dto.TeamResponse{
		ID:         team.ID,
		Name:       team.Name,
		MentorID:   team.MentorID,
		StudentIDs: team.StudentIDs,
	})
}

// PUT /teams/{id}/mentor — admin only. Set or remove a team's mentor.
func (h *TeamHandlers) PutTeamMentor(w http.ResponseWriter, r *http.Request) {
	teamID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "bad_request", "invalid team ID", nil)
		return
	}

	var req dto.PutTeamMentorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad_request", "invalid JSON body", nil)
		return
	}

	if err := h.svc.PutTeamMentor(r.Context(), teamID, req.MentorID); err != nil {
		switch {
		case errors.Is(err, store.ErrTeamNotFound):
			writeError(w, 404, "not_found", "team not found", nil)
		case errors.Is(err, service.ErrInvalidTeamConfig):
			writeError(w, 422, "validation_failed", err.Error(), nil)
		default:
			log.Printf("team handler: PutTeamMentor error: %v", err)
			writeError(w, 500, "server_error", "could not update team mentor", nil)
		}
		return
	}
	w.WriteHeader(204)
}

// GET /teams/me — student. Get the student's own team detail.
func (h *TeamHandlers) GetMyTeam(w http.ResponseWriter, r *http.Request) {
	studentID := middleware.CtxUserID(r.Context())
	if studentID == 0 {
		writeError(w, 401, "unauthorized", "missing user context", nil)
		return
	}

	detail, err := h.svc.GetMyTeam(r.Context(), studentID)
	if err != nil {
		if errors.Is(err, store.ErrTeamNotFound) {
			writeError(w, 404, "not_found", "no team found", nil)
		} else {
			log.Printf("team handler: GetMyTeam error: %v", err)
			writeError(w, 500, "server_error", "could not get team", nil)
		}
		return
	}

	resp := dto.TeamDetailResponse{
		ID:         detail.ID,
		Name:       detail.Name,
		Mentor:     toUserBrief(detail.Mentor),
		Members:    toUserBriefs(detail.Members),
		CohortSize: detail.CohortSize,
		TeamCount:  detail.TeamCount,
	}
	h.fillBriefs(r, resp.Mentor, resp.Members)
	writeJSON(w, 200, resp)
}

// GET /mentors/me/team — mentor. Get the mentor's team detail.
func (h *TeamHandlers) GetMentorTeam(w http.ResponseWriter, r *http.Request) {
	mentorID := middleware.CtxUserID(r.Context())
	if mentorID == 0 {
		writeError(w, 401, "unauthorized", "missing user context", nil)
		return
	}

	detail, err := h.svc.GetMentorTeam(r.Context(), mentorID)
	if err != nil {
		if errors.Is(err, store.ErrTeamNotFound) {
			writeError(w, 404, "not_found", "no team found", nil)
		} else {
			log.Printf("team handler: GetMentorTeam error: %v", err)
			writeError(w, 500, "server_error", "could not get team", nil)
		}
		return
	}

	resp := dto.TeamDetailResponse{
		ID:         detail.ID,
		Name:       detail.Name,
		Mentor:     toUserBrief(detail.Mentor),
		Members:    toUserBriefs(detail.Members),
		CohortSize: detail.CohortSize,
		TeamCount:  detail.TeamCount,
	}
	h.fillBriefs(r, resp.Mentor, resp.Members)
	writeJSON(w, 200, resp)
}

func toUserBrief(m *store.TeamMember) *dto.UserBrief {
	if m == nil {
		return nil
	}
	return &dto.UserBrief{ID: m.ID, Name: m.Name, Email: m.Email}
}

func toUserBriefs(members []store.TeamMember) []dto.UserBrief {
	resp := make([]dto.UserBrief, len(members))
	for i, m := range members {
		resp[i] = dto.UserBrief{ID: m.ID, Name: m.Name, Email: m.Email}
	}
	return resp
}
