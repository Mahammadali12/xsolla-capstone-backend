ALTER TABLE content_items
    ADD COLUMN IF NOT EXISTS url TEXT NULL;

-- Allow 'link' as a content item type.
ALTER TABLE content_items
    DROP CONSTRAINT IF EXISTS content_items_type_check,
    ADD CONSTRAINT content_items_type_check CHECK (type IN ('file','assignment','link'));

-- file requires file_id; assignment and link require neither file_id nor parent_id.
ALTER TABLE content_items
    DROP CONSTRAINT IF EXISTS content_items_file_check,
    ADD CONSTRAINT content_items_file_check
    CHECK (
        (type = 'file' AND file_id IS NOT NULL)
        OR (type IN ('assignment','link') AND file_id IS NULL)
    );

CREATE INDEX IF NOT EXISTS idx_content_items_url ON content_items (url) WHERE url IS NOT NULL;
