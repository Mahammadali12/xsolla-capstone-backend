-- 014_enrollment_grade: add per-student module grade (red/yellow/green) to enrollments
ALTER TABLE enrollments
    ADD COLUMN IF NOT EXISTS grade TEXT NULL CHECK (grade IN ('red', 'yellow', 'green'));

CREATE INDEX IF NOT EXISTS idx_enrollments_grade ON enrollments (grade) WHERE grade IS NOT NULL;
