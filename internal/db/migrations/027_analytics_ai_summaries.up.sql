-- 027_analytics_ai_summaries: generated summary cache. Only the summary and provenance are stored, never the
-- feedback records or the prompt payload.
CREATE TABLE IF NOT EXISTS analytics_ai_summaries (
    id                 BIGSERIAL PRIMARY KEY,
    subject_type       TEXT NOT NULL CHECK (subject_type IN ('student', 'mentor')),
    subject_id         BIGINT NOT NULL,
    period_from        DATE NOT NULL,
    period_to          DATE NOT NULL,
    source_fingerprint TEXT NOT NULL,
    prompt_version     TEXT NOT NULL,
    provider           TEXT NOT NULL,
    model              TEXT NOT NULL,
    summary_text       TEXT NOT NULL,
    source_count       INTEGER NOT NULL,
    generated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT analytics_ai_summaries_key UNIQUE
        (subject_type, subject_id, period_from, period_to, source_fingerprint, prompt_version)
);

CREATE INDEX IF NOT EXISTS idx_analytics_ai_summaries_subject
    ON analytics_ai_summaries (subject_type, subject_id, period_from, period_to, generated_at DESC);
