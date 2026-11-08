-- 018_teams: teams and team_members junction tables
CREATE TABLE IF NOT EXISTS teams (
    id         bigserial PRIMARY KEY,
    name       text   NOT NULL,
    mentor_id  bigint NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX teams_one_team_per_mentor ON teams (mentor_id) WHERE mentor_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS team_members (
    team_id    bigint NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    student_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, student_id),
    UNIQUE (student_id)                                                   -- one team per student
);
