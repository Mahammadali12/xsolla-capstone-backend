-- 029_events_created_by: who made an event, so its creator can change and delete it (also an event without a week).
-- NULL for events made before this.
ALTER TABLE events ADD COLUMN IF NOT EXISTS created_by BIGINT NULL REFERENCES users(id) ON DELETE SET NULL;
