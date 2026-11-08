-- 019_content_item_created_by down
ALTER TABLE content_items
    DROP COLUMN IF EXISTS created_by;
