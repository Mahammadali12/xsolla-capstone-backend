-- 031_mentor_discussions: a talk a mentor sets up with one or more students of their team (to discuss something). It is
-- NOT a 1:1: it never moves a deadline, has no booking and may be on any day. One row per invited student; the rows
-- made together share batch_id, so the mentor sees (and removes) the talk once.
CREATE SEQUENCE IF NOT EXISTS mentor_discussion_batches;

CREATE TABLE IF NOT EXISTS mentor_discussions (
    id         BIGSERIAL PRIMARY KEY,
    batch_id   BIGINT NOT NULL,
    mentor_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    student_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 100),
    note       TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL CHECK (ends_at > starts_at),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_mentor_discussions_mentor ON mentor_discussions (mentor_id, starts_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_mentor_discussions_student ON mentor_discussions (student_id, starts_at) WHERE deleted_at IS NULL;
