-- 029_events_created_by: down
ALTER TABLE events DROP COLUMN IF EXISTS created_by;
