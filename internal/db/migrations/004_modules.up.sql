-- 004_modules: modules (formerly courses; grade = red/yellow/green)
CREATE TABLE IF NOT EXISTS modules (
    id          BIGSERIAL PRIMARY KEY,
    title       TEXT NOT NULL,
    description TEXT,
    created_by  BIGINT NOT NULL REFERENCES users(id),
    grade       TEXT NULL CHECK (grade IN ('red', 'yellow', 'green')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_modules_created_by ON modules (created_by);
CREATE INDEX IF NOT EXISTS idx_modules_deleted_at ON modules (deleted_at);
