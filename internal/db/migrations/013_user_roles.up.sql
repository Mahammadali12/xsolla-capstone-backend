ALTER TABLE users
    ADD COLUMN IF NOT EXISTS roles TEXT[] NOT NULL DEFAULT '{}';

-- Backfill roles from the single role column for existing users.
UPDATE users
SET roles = ARRAY[role]
WHERE roles = '{}';

-- Keep the role column for backward compatibility but make it nullable;
-- new code reads roles[] instead.
ALTER TABLE users ALTER COLUMN role DROP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_roles ON users USING GIN (roles);
