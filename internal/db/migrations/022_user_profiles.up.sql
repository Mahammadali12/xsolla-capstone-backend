-- 022_user_profiles: the "about me" part of a person's profile (picture, bio, contact handles, who may see them).
-- A row is created the first time the person saves something; until then the defaults below apply.
CREATE TABLE IF NOT EXISTS user_profiles (
    user_id       BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    bio           TEXT NOT NULL DEFAULT '' CHECK (char_length(bio) <= 300),
    slack_handle  TEXT NOT NULL DEFAULT '' CHECK (char_length(slack_handle) <= 40),
    gitlab_handle TEXT NOT NULL DEFAULT '' CHECK (char_length(gitlab_handle) <= 40),
    visibility    TEXT NOT NULL DEFAULT 'everyone' CHECK (visibility IN ('everyone', 'staff_only')),
    -- object storage key of the picture, "avatars/<32 hex>.<jpg|png|webp>"; the random part is the public file name
    avatar_key    TEXT NULL UNIQUE,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
