-- 015_modules_opens_closes.down.sql
ALTER TABLE modules DROP CONSTRAINT IF EXISTS modules_closes_after_opens;
ALTER TABLE modules DROP COLUMN IF EXISTS closes_at;
ALTER TABLE modules DROP COLUMN IF EXISTS opens_at;
