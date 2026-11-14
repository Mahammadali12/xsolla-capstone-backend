-- 021_mentor_reviews: the weekly review each student writes about their mentor (one per student and module).
-- mentor_id is copied from the student's team when the review is sent, so the review stays with the mentor
-- it was about even if the team gets another mentor later.
CREATE TABLE IF NOT EXISTS mentor_reviews (
    id                BIGSERIAL PRIMARY KEY,
    module_id         BIGINT NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    student_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mentor_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team_id           BIGINT NULL REFERENCES teams(id) ON DELETE SET NULL,
    had_session       BOOLEAN NOT NULL,
    no_session_reason TEXT NULL
        CHECK (no_session_reason IN ('mentor-unavailable', 'student-unavailable', 'not-scheduled', 'other')),
    topics            TEXT[] NOT NULL DEFAULT '{}'
        CHECK (topics <@ ARRAY['goals', 'homework', 'lecture', 'project', 'career', 'other']::TEXT[]),
    other_topic       TEXT NULL CHECK (char_length(other_topic) BETWEEN 1 AND 120),
    rating_clarity    SMALLINT NULL CHECK (rating_clarity BETWEEN 1 AND 5),
    rating_engagement SMALLINT NULL CHECK (rating_engagement BETWEEN 1 AND 5),
    rating_comfort    SMALLINT NULL CHECK (rating_comfort BETWEEN 1 AND 5),
    rating_time       SMALLINT NULL CHECK (rating_time BETWEEN 1 AND 5),
    overall           SMALLINT NOT NULL CHECK (overall BETWEEN 1 AND 5),
    comment           TEXT NULL CHECK (char_length(comment) BETWEEN 1 AND 1000),
    submitted_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT mentor_reviews_one_per_week UNIQUE (module_id, student_id),
    -- with a session: topics and all four ratings, no reason; without one: a reason and nothing else
    CONSTRAINT mentor_reviews_session_answers CHECK (
        (had_session
            AND no_session_reason IS NULL
            AND cardinality(topics) > 0
            AND rating_clarity IS NOT NULL AND rating_engagement IS NOT NULL
            AND rating_comfort IS NOT NULL AND rating_time IS NOT NULL)
        OR
        (NOT had_session
            AND no_session_reason IS NOT NULL
            AND cardinality(topics) = 0
            AND other_topic IS NULL
            AND rating_clarity IS NULL AND rating_engagement IS NULL
            AND rating_comfort IS NULL AND rating_time IS NULL)
    ),
    -- the free-text topic exists exactly when "other" is ticked
    CONSTRAINT mentor_reviews_other_topic CHECK ((other_topic IS NOT NULL) = ('other' = ANY (topics)))
);

CREATE INDEX IF NOT EXISTS idx_mentor_reviews_student ON mentor_reviews (student_id);
CREATE INDEX IF NOT EXISTS idx_mentor_reviews_mentor ON mentor_reviews (mentor_id, module_id);
