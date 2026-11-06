-- Re-add section type constraint (reverse of 012)
ALTER TABLE content_items
    DROP CONSTRAINT IF EXISTS content_items_type_check,
    ADD CONSTRAINT content_items_type_check CHECK (type IN ('section','file','assignment'));

-- Rename modules back to courses (reverse of 011)
ALTER TABLE modules RENAME TO courses;

ALTER TABLE enrollments RENAME COLUMN module_id TO course_id;
ALTER TABLE content_items RENAME COLUMN module_id TO course_id;

ALTER INDEX IF EXISTS idx_modules_created_by RENAME TO idx_courses_created_by;
ALTER INDEX IF EXISTS idx_modules_deleted_at RENAME TO idx_courses_deleted_at;
ALTER INDEX IF EXISTS idx_content_items_module_id RENAME TO idx_content_items_course_id;
