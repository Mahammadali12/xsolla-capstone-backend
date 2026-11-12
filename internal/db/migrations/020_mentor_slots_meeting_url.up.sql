-- 020_mentor_slots_meeting_url: the Google Meet link created when a 1:1 is booked
ALTER TABLE mentor_slots ADD COLUMN IF NOT EXISTS meeting_url TEXT NULL;
