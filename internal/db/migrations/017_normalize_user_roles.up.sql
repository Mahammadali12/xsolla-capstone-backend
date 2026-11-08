-- 017_normalize_user_roles: replace denormalized users.role + users.roles[]
-- with a proper user_roles junction table; fold user_role_expiry into expires_at

CREATE TABLE IF NOT EXISTS user_roles (
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       text   NOT NULL CHECK (role IN ('student','mentor','lecturer','admin')),
    expires_at timestamptz NULL,
    PRIMARY KEY (user_id, role)
);
CREATE INDEX IF NOT EXISTS idx_user_roles_user ON user_roles (user_id);

-- Backfill roles from users.roles array + user_role_expiry expiry dates.
-- Legacy 'teacher' role maps to 'lecturer' (backend lecturer = old teacher minus grading).
INSERT INTO user_roles (user_id, role, expires_at)
SELECT u.id, CASE WHEN r.role = 'teacher' THEN 'lecturer' ELSE r.role END, e.expires_at
FROM users u
CROSS JOIN LATERAL unnest(u.roles) AS r(role)
LEFT JOIN user_role_expiry e ON e.user_id = u.id AND e.role = r.role;

-- user_role_expiry is folded into user_roles.expires_at; drop it.
DROP TABLE IF EXISTS user_role_expiry;

-- Remove denormalized columns from users.
ALTER TABLE users DROP COLUMN IF EXISTS roles;
ALTER TABLE users DROP COLUMN IF EXISTS role;
