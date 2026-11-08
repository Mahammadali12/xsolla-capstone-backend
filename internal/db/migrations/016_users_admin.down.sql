-- 016_users_admin down
DROP TABLE IF EXISTS user_role_expiry;
DROP INDEX IF EXISTS users_email_live;
CREATE UNIQUE INDEX IF NOT EXISTS users_email_key ON users (email);
ALTER TABLE users
    DROP COLUMN IF EXISTS left_at;
ALTER TABLE users
    DROP COLUMN IF EXISTS deleted_at;
