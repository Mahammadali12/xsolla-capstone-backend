CREATE TABLE IF NOT EXISTS submissions (
    id           BIGSERIAL PRIMARY KEY,
    content_item_id BIGINT NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
    student_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    text_body    TEXT,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    grade        NUMERIC CHECK (grade >= 0),
    feedback     TEXT,
    graded_by    BIGINT NULL REFERENCES users(id),
    graded_at    TIMESTAMPTZ NULL,
    deleted_at   TIMESTAMPTZ NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_submissions_unique 
    ON submissions (content_item_id, student_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_submissions_student ON submissions (student_id);
CREATE INDEX IF NOT EXISTS idx_submissions_deleted_at ON submissions (deleted_at);
