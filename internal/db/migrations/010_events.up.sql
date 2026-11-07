CREATE TABLE IF NOT EXISTS events (
    id          BIGSERIAL PRIMARY KEY,
    title       TEXT NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('lecture', 'meeting', 'off-campus')),
    starts_at   TIMESTAMPTZ NOT NULL,
    ends_at     TIMESTAMPTZ NOT NULL,
    all_day     BOOL NOT NULL DEFAULT FALSE,
    location    TEXT NULL,
    description TEXT NULL,
    module_id   BIGINT NULL REFERENCES modules(id),
    deleted_at  TIMESTAMPTZ NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_events_module_id ON events (module_id);
CREATE INDEX IF NOT EXISTS idx_events_starts_at ON events (starts_at);
CREATE INDEX IF NOT EXISTS idx_events_deleted_at ON events (deleted_at);

-- Constraint: ends_at must be after starts_at
ALTER TABLE events ADD CONSTRAINT chk_events_ends_after_starts CHECK (ends_at > starts_at);
