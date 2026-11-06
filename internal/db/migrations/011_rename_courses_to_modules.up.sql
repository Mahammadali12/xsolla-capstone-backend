ALTER TABLE courses RENAME TO modules;

ALTER TABLE enrollments RENAME COLUMN course_id TO module_id;
ALTER TABLE content_items RENAME COLUMN course_id TO module_id;

ALTER INDEX IF EXISTS idx_courses_created_by RENAME TO idx_modules_created_by;
ALTER INDEX IF EXISTS idx_courses_deleted_at RENAME TO idx_modules_deleted_at;
ALTER INDEX IF EXISTS idx_content_items_course_id RENAME TO idx_content_items_module_id;

-- Remove section-type content items (sections are being removed; files/assignments only)
DELETE FROM content_items WHERE type = 'section';
