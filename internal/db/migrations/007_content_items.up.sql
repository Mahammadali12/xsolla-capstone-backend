-- 007_content_items: flat content items (file / assignment / link; no sections)
CREATE TABLE IF NOT EXISTS content_items (
    id          BIGSERIAL PRIMARY KEY,
    module_id   BIGINT NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    parent_id   BIGINT NULL REFERENCES content_items(id),
    type        TEXT NOT NULL CHECK (type IN ('file', 'assignment', 'link')),
    title       TEXT NOT NULL,
    description TEXT,
    position    INT NOT NULL DEFAULT 0,
    is_visible  BOOL NOT NULL DEFAULT true,
    file_id     BIGINT NULL REFERENCES files(id) ON DELETE SET NULL,
    url         TEXT NULL,
    created_by  BIGINT NULL REFERENCES users(id),
    due_at      TIMESTAMPTZ NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ NULL,
    CONSTRAINT content_items_file_check CHECK (
        (type = 'file' AND file_id IS NOT NULL)
        OR (type IN ('assignment', 'link') AND file_id IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_content_items_module_id ON content_items (module_id);
CREATE INDEX IF NOT EXISTS idx_content_items_parent_id ON content_items (parent_id);
CREATE INDEX IF NOT EXISTS idx_content_items_file_id ON content_items (file_id);
CREATE INDEX IF NOT EXISTS idx_content_items_url ON content_items (url) WHERE url IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_content_items_deleted_at ON content_items (deleted_at);
