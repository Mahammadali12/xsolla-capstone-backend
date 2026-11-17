-- 030_broadcasts: announcements an admin sends to everyone or to one group. Each is delivered by the notification
-- service (event announcement.created) as an in-app notification and an email.
CREATE TABLE IF NOT EXISTS broadcasts (
    id         BIGSERIAL PRIMARY KEY,
    message    TEXT NOT NULL CHECK (char_length(message) BETWEEN 1 AND 500),
    link       TEXT NOT NULL DEFAULT '',
    audience   TEXT NOT NULL CHECK (audience IN ('all', 'students', 'mentors', 'lecturers')),
    sent_by    BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    recipients INTEGER NOT NULL DEFAULT 0,
    sent_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_broadcasts_sent_at ON broadcasts (sent_at DESC);
