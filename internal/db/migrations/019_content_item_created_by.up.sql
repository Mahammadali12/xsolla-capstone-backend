-- 019_content_item_created_by: add created_by column to content_items
ALTER TABLE content_items
    ADD COLUMN IF NOT EXISTS created_by bigint NULL REFERENCES users(id);
