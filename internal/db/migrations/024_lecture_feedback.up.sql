-- 024_lecture_feedback: what a student thinks of one lecture (an event of kind 'lecture'). One per student and
-- lecture. Only admins read them. The questions are defined in internal/service/lecture_feedback_service.go.
CREATE TABLE IF NOT EXISTS lecture_feedback (
    id           BIGSERIAL PRIMARY KEY,
    event_id     BIGINT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    student_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    overall      SMALLINT NOT NULL CHECK (overall BETWEEN 1 AND 5),
    clarity      SMALLINT NOT NULL CHECK (clarity BETWEEN 1 AND 5),
    practice     SMALLINT NOT NULL CHECK (practice BETWEEN 1 AND 5),
    confidence   SMALLINT NOT NULL CHECK (confidence BETWEEN 1 AND 5),
    pace         TEXT NOT NULL CHECK (pace IN ('too_slow', 'just_right', 'too_fast')),
    liked        TEXT NULL CHECK (char_length(liked) BETWEEN 1 AND 1000),
    improve      TEXT NULL CHECK (char_length(improve) BETWEEN 1 AND 1000),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT lecture_feedback_one_per_lecture UNIQUE (event_id, student_id)
);

CREATE INDEX IF NOT EXISTS idx_lecture_feedback_student ON lecture_feedback (student_id);
