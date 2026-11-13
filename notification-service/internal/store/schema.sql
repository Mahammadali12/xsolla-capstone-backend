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
