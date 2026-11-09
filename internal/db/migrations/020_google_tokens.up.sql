-- 020_google_tokens: store encrypted Google OAuth refresh tokens per user
CREATE TABLE IF NOT EXISTS google_tokens (
    user_id                 bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    encrypted_refresh_token text NOT NULL,
    token_expires_at        timestamptz,
    updated_at              timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id)
);
