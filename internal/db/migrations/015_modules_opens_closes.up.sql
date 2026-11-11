-- 015_modules_opens_closes: add opens_at and closes_at to modules
ALTER TABLE modules ADD COLUMN opens_at TIMESTAMPTZ NULL;
ALTER TABLE modules ADD COLUMN closes_at TIMESTAMPTZ NULL;
ALTER TABLE modules ADD CONSTRAINT modules_closes_after_opens CHECK (closes_at IS NULL OR opens_at IS NULL OR closes_at > opens_at);
