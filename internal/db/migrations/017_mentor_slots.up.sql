-- 017_mentor_slots: bookable 1:1 times a mentor offers and students book
CREATE TABLE IF NOT EXISTS mentor_slots (
    id         BIGSERIAL PRIMARY KEY,
    mentor_id  BIGINT NOT NULL REFERENCES users(id),
    starts_at  TIMESTAMPTZ NOT NULL,
    ends_at    TIMESTAMPTZ NOT NULL,
    booked_by  BIGINT NULL REFERENCES users(id),
    deleted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_mentor_slots_ends_after_starts CHECK (ends_at > starts_at)
);

CREATE INDEX IF NOT EXISTS idx_mentor_slots_mentor_id ON mentor_slots(mentor_id);
CREATE INDEX IF NOT EXISTS idx_mentor_slots_starts_at ON mentor_slots(starts_at);
CREATE INDEX IF NOT EXISTS idx_mentor_slots_booked_by ON mentor_slots(booked_by);
CREATE INDEX IF NOT EXISTS idx_mentor_slots_deleted_at ON mentor_slots(deleted_at);
