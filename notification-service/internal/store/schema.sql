CREATE TABLE IF NOT EXISTS deliveries (
    id           BIGSERIAL PRIMARY KEY,
    producer     TEXT NOT NULL,
    event_id     BIGINT NOT NULL,
    event_type   TEXT NOT NULL,
    recipient_id BIGINT NOT NULL,
    channel      TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('pending', 'sent', 'failed')),
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at      TIMESTAMPTZ,
    UNIQUE (producer, event_id, recipient_id, channel)
);

-- In-app notifications (the in_app channel): the rendered subject and body, kept
-- for the recipient. The LMS API lists a user's unread rows and sets read_at.
CREATE TABLE IF NOT EXISTS notifications (
    id           BIGSERIAL PRIMARY KEY,
    producer     TEXT NOT NULL,
    event_id     BIGINT NOT NULL,
    event_type   TEXT NOT NULL,
    recipient_id BIGINT NOT NULL,
    title        TEXT NOT NULL,
    body         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL,
    read_at      TIMESTAMPTZ,
    UNIQUE (producer, event_id, recipient_id)
);

CREATE INDEX IF NOT EXISTS notifications_unread
    ON notifications (recipient_id, created_at DESC) WHERE read_at IS NULL;
