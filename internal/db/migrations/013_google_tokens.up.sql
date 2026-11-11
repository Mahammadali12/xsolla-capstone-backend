-- 013_google_tokens: encrypted Google OAuth refresh tokens
CREATE TABLE IF NOT EXISTS google_tokens (
    user_id                 BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    encrypted_refresh_token TEXT NOT NULL,
    token_expires_at        TIMESTAMPTZ,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id)
);
