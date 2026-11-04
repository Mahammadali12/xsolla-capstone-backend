CREATE TABLE IF NOT EXISTS content_items (
    id          BIGSERIAL PRIMARY KEY,
    course_id   BIGINT NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    parent_id   BIGINT NULL REFERENCES content_items(id),
    type        TEXT NOT NULL CHECK (type IN ('section','file','assignment')),
    title       TEXT NOT NULL,
    description TEXT,
    position    INT NOT NULL DEFAULT 0,
    is_visible  BOOL NOT NULL DEFAULT true,
    file_id     BIGINT NULL,
    deleted_at  TIMESTAMPTZ NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_content_items_course_id ON content_items (course_id);
CREATE INDEX IF NOT EXISTS idx_content_items_parent_id ON content_items (parent_id);
CREATE INDEX IF NOT EXISTS idx_content_items_file_id ON content_items (file_id);
CREATE INDEX IF NOT EXISTS idx_content_items_deleted_at ON content_items (deleted_at);
