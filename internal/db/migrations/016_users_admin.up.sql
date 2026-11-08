-- 016_users_admin: inactive/deleted users + temporary role expiries
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS left_at timestamptz NULL;
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS deleted_at timestamptz NULL;

-- Case-insensitive email uniqueness among live accounts only (deleted accounts free their email).
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_key;
DROP INDEX IF EXISTS users_email_key;
CREATE UNIQUE INDEX users_email_live ON users (lower(email)) WHERE deleted_at IS NULL;

-- Temporary role end dates (admin sets via PUT /users/:id/roles).
CREATE TABLE IF NOT EXISTS user_role_expiry (
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       text   NOT NULL CHECK (role IN ('student','mentor','lecturer','admin')),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, role)
);
CREATE INDEX IF NOT EXISTS idx_user_role_expiry_user ON user_role_expiry (user_id);
