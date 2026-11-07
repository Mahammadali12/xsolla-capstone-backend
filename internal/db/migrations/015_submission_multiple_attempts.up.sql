-- Allow multiple submissions per student per assignment.
-- Removes the unique index that enforced a single attempt.
DROP INDEX IF EXISTS idx_submissions_unique;
