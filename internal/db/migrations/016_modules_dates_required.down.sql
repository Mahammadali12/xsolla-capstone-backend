-- 016_modules_dates_required.down.sql
ALTER TABLE modules ALTER COLUMN closes_at DROP NOT NULL;
ALTER TABLE modules ALTER COLUMN opens_at DROP NOT NULL;
