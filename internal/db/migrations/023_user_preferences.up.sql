-- 023_user_preferences: look and accessibility settings (text size, theme, ...). Only the person sees them.
-- One JSON object, so a new option needs no migration; the service validates keys and values on write.
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id    BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    prefs      JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(prefs) = 'object'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
