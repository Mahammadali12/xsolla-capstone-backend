-- 028_reports: anyone signed in can report a problem (e.g. a student's behaviour); only admins read and handle them
CREATE TABLE IF NOT EXISTS reports (
    id          BIGSERIAL PRIMARY KEY,
    reporter_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    subject     TEXT NOT NULL CHECK (char_length(subject) BETWEEN 1 AND 200),
    message     TEXT NOT NULL CHECK (char_length(message) BETWEEN 1 AND 5000),
    status      TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    admin_note  TEXT NULL CHECK (admin_note IS NULL OR char_length(admin_note) BETWEEN 1 AND 2000),
    resolved_by BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    resolved_at TIMESTAMPTZ NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reports_created_at ON reports (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reports_open ON reports (created_at DESC) WHERE status = 'open';
