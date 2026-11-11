-- 012_teams: teams and team members
CREATE TABLE IF NOT EXISTS teams (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL,
    mentor_id  BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX teams_one_team_per_mentor ON teams (mentor_id) WHERE mentor_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS team_members (
    team_id    BIGINT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    student_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, student_id),
    UNIQUE (student_id)
);
