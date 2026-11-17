-- 026_enrollment_graded_by: down
ALTER TABLE enrollments
    DROP COLUMN IF EXISTS graded_by;
