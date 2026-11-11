-- 008_assignments: assignment extension (no max_points)
CREATE TABLE IF NOT EXISTS assignments (
    content_item_id BIGINT PRIMARY KEY REFERENCES content_items(id) ON DELETE CASCADE,
    due_at          TIMESTAMPTZ NULL,
    deleted_at      TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_assignments_deleted_at ON assignments (deleted_at);
