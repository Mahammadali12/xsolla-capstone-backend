package dto

// TeamResponse is the team object for GET /teams and PUT /teams.
type TeamResponse struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	MentorID   *int64  `json:"mentor_id"`
	StudentIDs []int64 `json:"student_ids"`
}

// TeamInput is the payload for PUT /teams team entries.
type TeamInput struct {
	MentorID   *int64  `json:"mentor_id"`
	StudentIDs []int64 `json:"student_ids"`
}

// PutTeamRequest is the payload for PUT /teams.
type PutTeamRequest struct {
	Teams []TeamInput `json:"teams"`
}

// PutTeamStudentsRequest is the payload for PUT /teams/:id/student.
type PutTeamStudentsRequest struct {
	StudentIDs []int64 `json:"student_ids"`
}

// DeleteTeamStudentsRequest is the payload for DELETE /teams/:id/students.
type DeleteTeamStudentsRequest struct {
	StudentIDs []int64 `json:"student_ids"`
}

// PutTeamMentorRequest is the payload for PUT /teams/:id/mentor.
type PutTeamMentorRequest struct {
	MentorID *int64 `json:"mentor_id"`
}

// PatchTeamRequest is the payload for PATCH /teams/:id.
type PatchTeamRequest struct {
	Name     *string `json:"name,omitempty"`
	MentorID *int64  `json:"mentor_id,omitempty"`
}

// TeamDetailResponse is the payload for GET /teams/me and GET /mentors/me/team.
type TeamDetailResponse struct {
	ID          int64            `json:"id"`
	Name        string           `json:"name"`
	Mentor      *UserBrief       `json:"mentor,omitempty"`
	Members     []UserBrief      `json:"members"`
	CohortSize  int              `json:"cohort_size"`
	TeamCount   int              `json:"team_count"`
}

// UserBrief is a brief user representation for team responses.
type UserBrief struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
