-- 018_outbox_events: transactional outbox for domain events
CREATE TABLE IF NOT EXISTS outbox_events (
    id           BIGSERIAL PRIMARY KEY,
    event_type   TEXT NOT NULL,
    payload      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ NULL,
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT NULL
);

CREATE INDEX IF NOT EXISTS idx_outbox_events_unpublished
    ON outbox_events (id) WHERE published_at IS NULL;
