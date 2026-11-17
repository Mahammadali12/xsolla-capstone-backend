-- 025_enrollment_grade_feedback: down
ALTER TABLE enrollments
    DROP COLUMN IF EXISTS grade_feedback,
    DROP COLUMN IF EXISTS graded_at;
