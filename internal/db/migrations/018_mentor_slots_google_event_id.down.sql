-- 018_mentor_slots_google_event_id.down
ALTER TABLE mentor_slots DROP COLUMN IF EXISTS google_event_id;
