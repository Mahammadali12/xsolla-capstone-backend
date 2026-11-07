-- Restore the single-attempt constraint.
CREATE UNIQUE INDEX IF NOT EXISTS idx_submissions_unique
    ON submissions (content_item_id, student_id) WHERE deleted_at IS NULL;
