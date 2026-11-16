-- 025_enrollment_grade_feedback: the mentor's module feedback that comes with a week grade, and when the grade was
-- last written. A week graded before this has NULL in both.
ALTER TABLE enrollments
    ADD COLUMN IF NOT EXISTS grade_feedback TEXT NULL
        CHECK (grade_feedback IS NULL OR char_length(grade_feedback) BETWEEN 1 AND 1000),
    ADD COLUMN IF NOT EXISTS graded_at TIMESTAMPTZ NULL;
