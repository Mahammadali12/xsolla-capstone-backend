-- 026_enrollment_graded_by: record who gave an enrollment's grade
ALTER TABLE enrollments
    ADD COLUMN IF NOT EXISTS graded_by BIGINT NULL REFERENCES users(id) ON DELETE SET NULL;
