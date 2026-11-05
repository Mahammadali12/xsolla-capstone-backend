CREATE TABLE IF NOT EXISTS files (
    id          BIGSERIAL PRIMARY KEY,
    uploaded_by BIGINT NOT NULL REFERENCES users(id),
    original_name TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    deleted_at  TIMESTAMPTZ NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_files_uploaded_by ON files (uploaded_by);
CREATE INDEX IF NOT EXISTS idx_files_deleted_at ON files (deleted_at);
