-- Remove all link content items before reverting the constraint.
DELETE FROM content_items WHERE type = 'link';

ALTER TABLE content_items
    DROP CONSTRAINT IF EXISTS content_items_file_check,
    ADD CONSTRAINT content_items_file_check
    CHECK (
        (type = 'file' AND file_id IS NOT NULL)
        OR (type != 'file' AND file_id IS NULL)
    );

ALTER TABLE content_items
    DROP CONSTRAINT IF EXISTS content_items_type_check,
    ADD CONSTRAINT content_items_type_check CHECK (type IN ('file','assignment'));

ALTER TABLE content_items
    DROP COLUMN IF EXISTS url;

DROP INDEX IF EXISTS idx_content_items_url;
