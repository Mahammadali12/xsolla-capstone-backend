-- 017_normalize_user_roles down: restore denormalized users.role + users.roles[]

-- Restore columns on users.
ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'student';
ALTER TABLE users ADD COLUMN IF NOT EXISTS roles TEXT[] NOT NULL DEFAULT '{}';

-- Backfill roles array from user_roles.
UPDATE users u
SET roles = sub.roles
FROM (
    SELECT user_id, array_agg(role) AS roles
    FROM user_roles
    GROUP BY user_id
) sub
WHERE u.id = sub.user_id;

-- Recreate user_role_expiry from user_roles rows that have an expiry.
CREATE TABLE IF NOT EXISTS user_role_expiry (
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       text   NOT NULL CHECK (role IN ('student','mentor','lecturer','admin')),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, role)
);
INSERT INTO user_role_expiry (user_id, role, expires_at)
SELECT user_id, role, expires_at FROM user_roles WHERE expires_at IS NOT NULL;

-- Drop the junction table.
DROP TABLE IF EXISTS user_roles;
