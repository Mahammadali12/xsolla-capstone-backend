package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Team represents a team row.
type Team struct {
	ID         int64
	Name       string
	MentorID   *int64
	StudentIDs []int64
	CreatedAt  time.Time
}

// TeamInput is the input for creating/updating a team.
type TeamInput struct {
	MentorID   *int64  `json:"mentor_id"`
	StudentIDs []int64 `json:"student_ids"`
}

// TeamMember is a brief representation of a user in a team context.
type TeamMember struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// TeamDetail is the full detail of a team for the read-side endpoints.
type TeamDetail struct {
	ID          int64        `json:"id"`
	Name        string       `json:"name"`
	Mentor      *TeamMember  `json:"mentor,omitempty"`
	Members     []TeamMember `json:"members"`
	CohortSize  int          `json:"cohort_size"`
	TeamCount   int          `json:"team_count"`
}

// UserBrief is a brief user representation for validation/lookup.
type UserBrief struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Email string  `json:"email"`
	Roles []string `json:"roles"`
}

// ErrTeamNotFound is returned when a team does not exist.
var ErrTeamNotFound = errors.New("team not found")

// ErrStudentNotFound is returned when a student is not found or not active.
var ErrStudentNotFound = errors.New("student not found or not active")

// ErrMentorNotFound is returned when a mentor is not found or not active.
var ErrMentorNotFound = errors.New("mentor not found or not active")

// ErrDuplicateStudent is returned when a student appears in multiple teams.
var ErrDuplicateStudent = errors.New("student already in another team")

// ErrDuplicateMentor is returned when a mentor leads multiple teams.
var ErrDuplicateMentor = errors.New("mentor already leads another team")

// ErrInvalidTeamRole is returned when a user has the wrong role for a team position.
var ErrInvalidTeamRole = errors.New("user does not have the required role for this team position")

// GetAllTeams returns all teams with their members.
func (s *StoreImpl) GetAllTeams(ctx context.Context) ([]Team, error) {
	rows, err := s.DB().DBTX.QueryContext(ctx,
		`SELECT t.id, t.name, t.mentor_id, tm.student_id
		 FROM teams t
		 LEFT JOIN team_members tm ON tm.team_id = t.id
		 ORDER BY t.id, tm.student_id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type teamRow struct {
		ID         int64
		Name       string
		MentorID   *int64
		StudentID  sql.NullInt64
	}
	teamsMap := map[int64]*Team{}
	order := []int64{}

	for rows.Next() {
		var tr teamRow
		if err := rows.Scan(&tr.ID, &tr.Name, &tr.MentorID, &tr.StudentID); err != nil {
			return nil, err
		}
		if _, ok := teamsMap[tr.ID]; !ok {
			teamsMap[tr.ID] = &Team{
				ID:         tr.ID,
				Name:       tr.Name,
				MentorID:   tr.MentorID,
				StudentIDs: []int64{},
			}
			order = append(order, tr.ID)
		}
		if tr.StudentID.Valid {
			teamsMap[tr.ID].StudentIDs = append(teamsMap[tr.ID].StudentIDs, tr.StudentID.Int64)
		}
	}

	teams := make([]Team, 0, len(order))
	for _, id := range order {
		teams = append(teams, *teamsMap[id])
	}
	return teams, nil
}

// ReplaceTeams atomically replaces all teams. Names are generated Team 1..n.
// [] means "no teams".
func (s *StoreImpl) ReplaceTeams(ctx context.Context, teams []TeamInput) ([]Team, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	q := WithTx(tx)

	// Delete all existing team memberships and teams.
	if _, err := q.DBTX.ExecContext(ctx, `DELETE FROM team_members`); err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err := q.DBTX.ExecContext(ctx, `DELETE FROM teams`); err != nil {
		tx.Rollback()
		return nil, err
	}

	result := make([]Team, 0, len(teams))
	for i, input := range teams {
		name := fmt.Sprintf("Team %d", i+1)
		var teamID int64
		err := q.DBTX.QueryRowContext(ctx,
			`INSERT INTO teams (name, mentor_id) VALUES ($1, $2) RETURNING id`,
			name, input.MentorID,
		).Scan(&teamID)
		if err != nil {
			tx.Rollback()
			return nil, err
		}
		team := Team{
			ID:         teamID,
			Name:       name,
			MentorID:   input.MentorID,
			StudentIDs: []int64{},
		}
		for _, sid := range input.StudentIDs {
			if _, err := q.DBTX.ExecContext(ctx,
				`INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`,
				teamID, sid,
			); err != nil {
				tx.Rollback()
				return nil, err
			}
			team.StudentIDs = append(team.StudentIDs, sid)
		}
		result = append(result, team)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// AddTeamStudents adds students to a team.
// Each student must not already belong to another team (no auto-removal).
func (s *StoreImpl) AddTeamStudents(ctx context.Context, teamID int64, studentIDs []int64) error {
	// Validate team exists.
	teams, err := s.GetAllTeams(ctx)
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
		users, err := s.GetActiveUsersByIDs(ctx, studentIDs)
		if err != nil {
			return err
		}
		if len(users) != len(studentIDs) {
			return ErrInvalidTeamRole
		}
		for _, u := range users {
			if !slices.Contains(u.Roles, "student") {
				return ErrInvalidTeamRole
			}
		}
	}

	// Check that no student is already in another team.
	rows, err := s.DB().QueryContext(ctx,
		`SELECT student_id, team_id FROM team_members WHERE student_id = ANY($1::bigint[])`,
		studentIDs,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	alreadyInTeam := make(map[int64]int64)
	for rows.Next() {
		var studentID, existingTeamID int64
		if err := rows.Scan(&studentID, &existingTeamID); err != nil {
			return err
		}
		if existingTeamID != teamID {
			return fmt.Errorf("student %d is already in team %d", studentID, existingTeamID)
		}
		alreadyInTeam[studentID] = existingTeamID
	}

	// Insert students into the team (skip those already in this team).
	for _, studentID := range studentIDs {
		if alreadyInTeam[studentID] == teamID {
			continue
		}
		if _, err := s.DB().ExecContext(ctx, `INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`, teamID, studentID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteTeamStudents removes students from a team.
func (s *StoreImpl) DeleteTeamStudents(ctx context.Context, teamID int64, studentIDs []int64) error {
	if len(studentIDs) == 0 {
		return nil
	}
	// Build a dynamic IN clause for deleting multiple students at once.
	args := make([]any, 0, len(studentIDs)+1)
	placeholders := make([]string, 0, len(studentIDs))
	for i, sid := range studentIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, sid)
	}
	args = append(args, teamID)
	query := fmt.Sprintf(
		`DELETE FROM team_members WHERE team_id = $%d AND student_id IN (%s)`,
		len(args), strings.Join(placeholders, ","),
	)
	if _, err := s.DB().ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return nil
}

// DeleteTeamStudent removes a single student from a team.
func (s *StoreImpl) DeleteTeamStudent(ctx context.Context, teamID int64, studentID int64) error {
	_, err := s.DB().ExecContext(ctx, `DELETE FROM team_members WHERE team_id = $1 AND student_id = $2`, teamID, studentID)
	return err
}

// DeleteTeam removes one team. Its member list goes with it; past mentor reviews keep their text (team_id becomes
// NULL), and the students and the mentor are not touched.
func (s *StoreImpl) DeleteTeam(ctx context.Context, id int64) error {
	res, err := s.DB().ExecContext(ctx, `DELETE FROM teams WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTeamNotFound
	}
	return nil
}

// PutTeamMentor sets or removes a team's mentor. If the mentor leads another
// team, they are taken off it (one team per mentor).
func (s *StoreImpl) PutTeamMentor(ctx context.Context, teamID int64, mentorID *int64) error {
	if mentorID == nil {
		_, err := s.DB().ExecContext(ctx, `UPDATE teams SET mentor_id = NULL WHERE id = $1`, teamID)
		return err
	}
	// Take mentor off their other team first.
	if _, err := s.DB().ExecContext(ctx, `UPDATE teams SET mentor_id = NULL WHERE mentor_id = $1 AND id != $2`, *mentorID, teamID); err != nil {
		return err
	}
	_, err := s.DB().ExecContext(ctx, `UPDATE teams SET mentor_id = $1 WHERE id = $2`, *mentorID, teamID)
	return err
}

// CreateTeam creates a single team. name must be non-empty.
func (s *StoreImpl) CreateTeam(ctx context.Context, name *string, mentorID *int64, studentIDs []int64) (*Team, error) {
	var teamID int64
	err := s.DB().QueryRowContext(ctx,
		`INSERT INTO teams (name, mentor_id) VALUES ($1, $2) RETURNING id`,
		*name, mentorID,
	).Scan(&teamID)
	if err != nil {
		return nil, err
	}
	team := &Team{
		ID:         teamID,
		Name:       *name,
		MentorID:   mentorID,
		StudentIDs: []int64{},
	}
	for _, sid := range studentIDs {
		if _, err := s.DB().ExecContext(ctx,
			`INSERT INTO team_members (team_id, student_id) VALUES ($1, $2)`,
			teamID, sid,
		); err != nil {
			return nil, err
		}
		team.StudentIDs = append(team.StudentIDs, sid)
	}
	return team, nil
}

// GetStudentTeams returns student_id -> team_id for students already in a team.
func (s *StoreImpl) GetStudentTeams(ctx context.Context, studentIDs []int64) (map[int64]int64, error) {
	rows, err := s.DB().QueryContext(ctx,
		`SELECT student_id, team_id FROM team_members WHERE student_id = ANY($1::bigint[])`,
		studentIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int64]int64)
	for rows.Next() {
		var studentID, teamID int64
		if err := rows.Scan(&studentID, &teamID); err != nil {
			return nil, err
		}
		result[studentID] = teamID
	}
	return result, nil
}

// PatchTeam updates a single team's name and/or mentor_id in place.
func (s *StoreImpl) PatchTeam(ctx context.Context, id int64, name *string, mentorID *int64) (*Team, error) {
	var team Team
	var mID sql.NullInt64
	err := s.DB().PatchTeam(ctx, id, name, mentorID).Scan(
		&team.ID, &team.Name, &mID, &team.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTeamNotFound
		}
		return nil, err
	}
	team.MentorID = nil
	if mID.Valid {
		team.MentorID = &mID.Int64
	}
	return &team, nil
}

// GetMyTeam returns the team detail for a student.
func (s *StoreImpl) GetMyTeam(ctx context.Context, studentID int64) (*TeamDetail, error) {
	// Get the student's team id.
	var teamID sql.NullInt64
	err := s.DB().GetUserTeam(ctx, studentID).Scan(&teamID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTeamNotFound
		}
		return nil, err
	}
	if !teamID.Valid {
		return nil, ErrTeamNotFound
	}
	return s.getTeamDetail(ctx, teamID.Int64)
}

// GetMentorTeamDetail returns the team detail for a mentor.
func (s *StoreImpl) GetMentorTeamDetail(ctx context.Context, mentorID int64) (*TeamDetail, error) {
	var teamID sql.NullInt64
	err := s.DB().GetMentorTeam(ctx, mentorID).Scan(&teamID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTeamNotFound
		}
		return nil, err
	}
	if !teamID.Valid {
		return nil, ErrTeamNotFound
	}
	return s.getTeamDetail(ctx, teamID.Int64)
}

// CountTeams returns the total number of teams.
func (s *StoreImpl) CountTeams(ctx context.Context) (int, error) {
	var count int
	err := s.DB().CountTeams(ctx).Scan(&count)
	return count, err
}

// CountActiveStudents returns count of active students.
func (s *StoreImpl) CountActiveStudents(ctx context.Context) (int, error) {
	var count int
	err := s.DB().CountActiveStudents(ctx).Scan(&count)
	return count, err
}

// GetActiveUsersByIDs returns active users by ids with their roles.
func (s *StoreImpl) GetActiveUsersByIDs(ctx context.Context, ids []int64) ([]UserBrief, error) {
	if len(ids) == 0 {
		return []UserBrief{}, nil
	}
	rows, err := s.DB().GetActiveUsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserBrief
	for rows.Next() {
		var u UserBrief
		var roles []string
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, pq.Array(&roles)); err != nil {
			return nil, err
		}
		u.Roles = roles
		users = append(users, u)
	}
	return users, nil
}

// getTeamDetail loads the full detail of a team including mentor and members.
func (s *StoreImpl) getTeamDetail(ctx context.Context, teamID int64) (*TeamDetail, error) {
	// Get team info.
	var team Team
	var mentorID sql.NullInt64
	err := s.DB().QueryRowContext(ctx,
		`SELECT id, name, mentor_id, created_at FROM teams WHERE id = $1`,
		teamID,
	).Scan(&team.ID, &team.Name, &mentorID, &team.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTeamNotFound
		}
		return nil, err
	}
	team.MentorID = nil
	if mentorID.Valid {
		team.MentorID = &mentorID.Int64
	}

	// Get members.
	memberRows, err := s.DB().GetTeamMembers(ctx, teamID)
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()

	var memberIDs []int64
	for memberRows.Next() {
		var sid int64
		if err := memberRows.Scan(&sid); err != nil {
			return nil, err
		}
		memberIDs = append(memberIDs, sid)
	}
	team.StudentIDs = memberIDs

	// Get user details for members and mentor.
	var allIDs []int64
	allIDs = append(allIDs, memberIDs...)
	if team.MentorID != nil {
		allIDs = append(allIDs, *team.MentorID)
	}
	users, err := s.GetActiveUsersByIDs(ctx, allIDs)
	if err != nil {
		return nil, err
	}
	userMap := make(map[int64]UserBrief)
	for _, u := range users {
		userMap[u.ID] = u
	}

	detail := &TeamDetail{
		ID:         team.ID,
		Name:       team.Name,
		Members:    []TeamMember{},
	}
	if team.MentorID != nil {
		if m, ok := userMap[*team.MentorID]; ok {
			detail.Mentor = &TeamMember{ID: m.ID, Name: m.Name, Email: m.Email}
		}
	}
	for _, sid := range memberIDs {
		if u, ok := userMap[sid]; ok {
			detail.Members = append(detail.Members, TeamMember{ID: u.ID, Name: u.Name, Email: u.Email})
		}
	}

	// Cohort size and team count.
	detail.CohortSize, err = s.CountActiveStudents(ctx)
	if err != nil {
		return nil, err
	}
	detail.TeamCount, err = s.CountTeams(ctx)
	if err != nil {
		return nil, err
	}

	return detail, nil
}
