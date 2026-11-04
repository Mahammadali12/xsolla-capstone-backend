CREATE TABLE IF NOT EXISTS assignments (
    content_item_id BIGINT PRIMARY KEY REFERENCES content_items(id) ON DELETE CASCADE,
    due_at          TIMESTAMPTZ NULL,
    max_points      INT NOT NULL DEFAULT 0 CHECK (max_points >= 0),
    deleted_at      TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_assignments_deleted_at ON assignments (deleted_at);
