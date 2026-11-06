ALTER TABLE content_items
    DROP CONSTRAINT IF EXISTS content_items_type_check,
    ADD CONSTRAINT content_items_type_check CHECK (type IN ('section','file','assignment'));
