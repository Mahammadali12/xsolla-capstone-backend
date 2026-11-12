-- 019_events_meeting_url: the Google Meet link of a synced event (or a link the creator pasted)
ALTER TABLE events ADD COLUMN IF NOT EXISTS meeting_url TEXT NULL;
