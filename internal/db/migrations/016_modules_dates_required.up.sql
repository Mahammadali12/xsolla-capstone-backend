-- 016_modules_dates_required: backfill and make opens_at/closes_at mandatory
UPDATE modules SET opens_at = created_at WHERE opens_at IS NULL;
UPDATE modules SET closes_at = created_at + interval '1 year' WHERE closes_at IS NULL;
ALTER TABLE modules ALTER COLUMN opens_at SET NOT NULL;
ALTER TABLE modules ALTER COLUMN closes_at SET NOT NULL;
