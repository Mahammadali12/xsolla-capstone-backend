-- 006_enrollments: module enrollments
CREATE TABLE IF NOT EXISTS enrollments (
    id         BIGSERIAL PRIMARY KEY,
    module_id  BIGINT NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_enrollments_unique ON enrollments (module_id, user_id);
CREATE INDEX IF NOT EXISTS idx_enrollments_module_id ON enrollments (module_id);
CREATE INDEX IF NOT EXISTS idx_enrollments_user_id ON enrollments (user_id);
