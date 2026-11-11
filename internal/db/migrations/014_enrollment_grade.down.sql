-- 014_enrollment_grade: down
ALTER TABLE enrollments
    DROP COLUMN IF EXISTS grade;

DROP INDEX IF EXISTS idx_enrollments_grade;
