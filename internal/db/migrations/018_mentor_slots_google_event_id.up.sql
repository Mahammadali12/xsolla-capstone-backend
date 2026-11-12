-- 018_mentor_slots_google_event_id: Google Calendar event created when a slot is booked
ALTER TABLE mentor_slots ADD COLUMN IF NOT EXISTS google_event_id TEXT NULL;
