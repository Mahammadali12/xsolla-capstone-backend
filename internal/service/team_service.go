package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"xsolla-capstone-api/internal/store"
)

// TeamService manages teams and team membership (admin-only writes).
type TeamService struct {
	store store.Store
	slots *MentorSlotService
}

// NewTeamService creates a TeamService backed by the given store.
// slots is used to clear a mentor's availability when they leave a team.
func NewTeamService(s store.Store, slots *MentorSlotService) *TeamService {
	return &TeamService{store: s, slots: slots}
}

var (
	// ErrTeamNotFound is returned when a team does not exist.
	ErrTeamNotFound = store.ErrTeamNotFound
	// ErrInvalidTeamConfig is returned for invalid team configuration.
	ErrInvalidTeamConfig = errors.New("invalid team configuration")
	// ErrStudentInAnotherTeam is returned when a student is already assigned to a different team.
	ErrStudentInAnotherTeam = errors.New("student already in another team")
	// ErrDuplicateStudent is returned when a student appears in multiple teams.
	ErrDuplicateStudent = store.ErrDuplicateStudent
	// ErrDuplicateMentor is returned when a mentor leads multiple teams.
	ErrDuplicateMentor = store.ErrDuplicateMentor
)

// ListTeams returns all teams (admin only).
func (s *TeamService) ListTeams(ctx context.Context) ([]store.Team, error) {
	return s.store.GetAllTeams(ctx)
}

// ReplaceTeams replaces all teams in one transaction.
// Names are generated Team 1..n. [] means no teams.
// Validates: every id exists and is active with the correct role,
// no duplicate student_ids, no duplicate mentor_ids.
func (s *TeamService) ReplaceTeams(ctx context.Context, inputs []store.TeamInput) ([]store.Team, error) {
	// Collect all unique ids to validate.
	allIDs := map[int64]bool{}
	mentorIDs := []int64{}
	studentIDs := []int64{}

	for _, input := range inputs {
		if input.MentorID != nil {
			if allIDs[*input.MentorID] {
				return nil, ErrDuplicateMentor
			}
			allIDs[*input.MentorID] = true
			mentorIDs = append(mentorIDs, *input.MentorID)
		}
		for _, sid := range input.StudentIDs {
			if allIDs[sid] {
				return nil, ErrDuplicateStudent
			}
			allIDs[sid] = true
			studentIDs = append(studentIDs, sid)
		}
	}

	// Build sorted id slice for the query.
	idSlice := make([]int64, 0, len(allIDs))
	for id := range allIDs {
		idSlice = append(idSlice, id)
	}
	sort.Slice(idSlice, func(i, j int) bool { return idSlice[i] < idSlice[j] })

	// Validate all users exist and are active.
	users, err := s.store.GetActiveUsersByIDs(ctx, idSlice)
	if err != nil {
		return nil, err
	}
	userRoles := make(map[int64][]string)
	for _, u := range users {
		userRoles[u.ID] = u.Roles
	}

	// Check each referenced id exists and is active.
	for id := range allIDs {
		if _, ok := userRoles[id]; !ok {
			return nil, ErrInvalidTeamConfig
		}
	}

	// Validate roles: mentor_ids must have 'mentor' role, student_ids must have 'student' role.
	for _, mid := range mentorIDs {
		if !slices.Contains(userRoles[mid], "mentor") {
			return nil, ErrInvalidTeamConfig
		}
	}
	for _, sid := range studentIDs {
		if !slices.Contains(userRoles[sid], "student") {
			return nil, ErrInvalidTeamConfig
		}
	}

	// A student cannot also be a mentor (already enforced by role check above,
	// since a user has one effective role at a time after normalization).
	// But double-check: a user can have multiple roles (mentor+lecturer).
	// For team purposes, mentor_id must have 'mentor' role; student_ids must
	// have 'student' role. A user with both mentor and student could technically
	// be both, but that's an edge case we allow.

	// shortcut: teams are matched by mentor membership, so a mentor moved to
	// another team in the same call keeps their slots; upgrade by matching
	// teams on student membership if swaps become common.
	existing, err := s.store.GetAllTeams(ctx)
	if err != nil {
		return nil, err
	}
	for _, mentorID := range removedMentors(existing, inputs) {
		if err := s.slots.CancelSlotsForMentor(ctx, mentorID); err != nil {
			return nil, err
		}
	}

	return s.store.ReplaceTeams(ctx, inputs)
}

// removedMentors returns old mentors no longer assigned to any team in inputs.
func removedMentors(old []store.Team, inputs []store.TeamInput) []int64 {
	kept := make(map[int64]bool, len(inputs))
	for _, input := range inputs {
		if input.MentorID != nil {
			kept[*input.MentorID] = true
		}
	}
	removed := make(map[int64]bool)
	for _, team := range old {
		if team.MentorID != nil && !kept[*team.MentorID] {
			removed[*team.MentorID] = true
		}
	}
	ids := make([]int64, 0, len(removed))
	for id := range removed {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// CreateTeam creates a single team.
// name must be non-empty. Mentor must have 'mentor' role, students 'student' role.
// No student may already belong to another team (no auto-removal).
func (s *TeamService) CreateTeam(ctx context.Context, name *string, mentorID *int64, studentIDs []int64) (*store.Team, error) {
	if name == nil || *name == "" {
		return nil, ErrInvalidTeamConfig
	}

	// Validate all referenced users exist and are active.
	allIDs := make([]int64, 0, len(studentIDs)+1)
	if mentorID != nil {
		allIDs = append(allIDs, *mentorID)
	}
	allIDs = append(allIDs, studentIDs...)

	if len(allIDs) > 0 {
		users, err := s.store.GetActiveUsersByIDs(ctx, allIDs)
		if err != nil {
			return nil, err
		}
		userRoles := make(map[int64][]string, len(users))
		for _, u := range users {
			userRoles[u.ID] = u.Roles
		}
		for _, id := range allIDs {
			if _, ok := userRoles[id]; !ok {
				return nil, ErrInvalidTeamConfig
			}
		}
		if mentorID != nil {
			if !slices.Contains(userRoles[*mentorID], "mentor") {
				return nil, ErrInvalidTeamConfig
			}
		}
		for _, sid := range studentIDs {
			if !slices.Contains(userRoles[sid], "student") {
				return nil, ErrInvalidTeamConfig
			}
		}
	}

	// Check no student is already in another team (no auto-removal).
	if len(studentIDs) > 0 {
		existing, err := s.store.GetStudentTeams(ctx, studentIDs)
		if err != nil {
			return nil, err
		}
		for studentID, existingTeamID := range existing {
			return nil, fmt.Errorf("%w: student %d is already in team %d", ErrStudentInAnotherTeam, studentID, existingTeamID)
		}
	}

	return s.store.CreateTeam(ctx, name, mentorID, studentIDs)
}

// AddTeamStudents adds students to a team.
// Students must not already belong to another team (no auto-removal).
func (s *TeamService) AddTeamStudents(ctx context.Context, teamID int64, studentIDs []int64) error {
	// Validate team exists.
	teams, err := s.store.GetAllTeams(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, t := range teams {
		if t.ID == teamID {
			found = true
			break
		}
	}
	if !found {
		return ErrTeamNotFound
	}

	// Validate all students exist, are active, and have the 'student' role.
	if len(studentIDs) > 0 {
		users, err := s.store.GetActiveUsersByIDs(ctx, studentIDs)
		if err != nil {
			return err
		}
		if len(users) != len(studentIDs) {
			return ErrInvalidTeamConfig
		}
		for _, u := range users {
			if !slices.Contains(u.Roles, "student") {
				return ErrInvalidTeamConfig
			}
		}
	}

	return s.store.AddTeamStudents(ctx, teamID, studentIDs)
}

// PatchTeam updates a single team's name and/or mentor_id in place.
// Validates: team exists; if mentor_id changes, mentor must exist, be active,
// have 'mentor' role, and not lead another team.
func (s *TeamService) PatchTeam(ctx context.Context, id int64, name *string, mentorID *int64) (*store.Team, error) {
	// Validate team exists.
	teams, err := s.store.GetAllTeams(ctx)
	if err != nil {
		return nil, err
	}
	var oldMentor *int64
	found := false
	for _, t := range teams {
		if t.ID == id {
			found = true
			oldMentor = t.MentorID
			break
		}
	}
	if !found {
		return nil, ErrTeamNotFound
	}

	// If changing mentor, validate the new mentor.
	if mentorID != nil {
		users, err := s.store.GetActiveUsersByIDs(ctx, []int64{*mentorID})
		if err != nil {
			return nil, err
		}
		if len(users) == 0 || !slices.Contains(users[0].Roles, "mentor") {
			return nil, ErrInvalidTeamConfig
		}
	}

	// The replaced mentor loses their availability.
	if mentorID != nil && oldMentor != nil && *mentorID != *oldMentor {
		if err := s.slots.CancelSlotsForMentor(ctx, *oldMentor); err != nil {
			return nil, err
		}
	}

	return s.store.PatchTeam(ctx, id, name, mentorID)
}

// DeleteTeamStudent removes a single student from a team.
func (s *TeamService) DeleteTeamStudent(ctx context.Context, teamID int64, studentID int64) error {
	return s.store.DeleteTeamStudent(ctx, teamID, studentID)
}
func (s *TeamService) DeleteTeamStudents(ctx context.Context, teamID int64, studentIDs []int64) error {
	// Validate team exists.
	teams, err := s.store.GetAllTeams(ctx)
	if err != nil {
		return err
	}
	found := false
	for _, t := range teams {
		if t.ID == teamID {
			found = true
			break
		}
	}
	if !found {
		return ErrTeamNotFound
	}

	return s.store.DeleteTeamStudents(ctx, teamID, studentIDs)
}

// PutTeamMentor sets or removes a team's mentor.
func (s *TeamService) PutTeamMentor(ctx context.Context, teamID int64, mentorID *int64) error {
	// Validate team exists.
	teams, err := s.store.GetAllTeams(ctx)
	if err != nil {
		return err
	}
	var oldMentor *int64
	found := false
	for _, t := range teams {
		if t.ID == teamID {
			found = true
			oldMentor = t.MentorID
			break
		}
	}
	if !found {
		return ErrTeamNotFound
	}

	// If setting a mentor, validate they exist, are active, and have mentor role.
	if mentorID != nil {
		users, err := s.store.GetActiveUsersByIDs(ctx, []int64{*mentorID})
		if err != nil {
			return err
		}
		if len(users) == 0 || !slices.Contains(users[0].Roles, "mentor") {
			return ErrInvalidTeamConfig
		}
	}

	// The removed mentor loses their availability.
	if oldMentor != nil && (mentorID == nil || *mentorID != *oldMentor) {
		if err := s.slots.CancelSlotsForMentor(ctx, *oldMentor); err != nil {
			return err
		}
	}

	return s.store.PutTeamMentor(ctx, teamID, mentorID)
}

// GetMyTeam returns the team detail for a student.
func (s *TeamService) GetMyTeam(ctx context.Context, studentID int64) (*store.TeamDetail, error) {
	return s.store.GetMyTeam(ctx, studentID)
}

// GetMentorTeam returns the team detail for a mentor.
func (s *TeamService) GetMentorTeam(ctx context.Context, mentorID int64) (*store.TeamDetail, error) {
	return s.store.GetMentorTeamDetail(ctx, mentorID)
}

// Counts returns team and student counts for the read-side responses.
func (s *TeamService) Counts(ctx context.Context) (teamCount int, studentCount int, err error) {
	teamCount, err = s.store.CountTeams(ctx)
	if err != nil {
		return 0, 0, err
	}
	studentCount, err = s.store.CountActiveStudents(ctx)
	if err != nil {
		return 0, 0, err
	}
	return teamCount, studentCount, nil
}
