CREATE TABLE IF NOT EXISTS quizzes (
    id                     BIGSERIAL PRIMARY KEY,
    module_id              BIGINT NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    source_content_item_id BIGINT NULL REFERENCES content_items(id) ON DELETE SET NULL,
    title                  TEXT NOT NULL,
    description            TEXT NOT NULL DEFAULT '',
    due_at                 TIMESTAMPTZ NULL,
    status                 TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('generating', 'ready', 'failed')),
    error_message          TEXT NOT NULL DEFAULT '',
    is_ai_generated        BOOLEAN NOT NULL DEFAULT FALSE,
    created_by             BIGINT NOT NULL REFERENCES users(id),
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at             TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_quizzes_module_id ON quizzes(module_id);
CREATE INDEX IF NOT EXISTS idx_quizzes_deleted_at ON quizzes(deleted_at);

CREATE TABLE IF NOT EXISTS quiz_questions (
    id            BIGSERIAL PRIMARY KEY,
    quiz_id       BIGINT NOT NULL REFERENCES quizzes(id) ON DELETE CASCADE,
    position      INT NOT NULL,
    prompt        TEXT NOT NULL,
    options       JSONB NOT NULL,
    correct_index INT NOT NULL CHECK (correct_index >= 0)
);

CREATE INDEX IF NOT EXISTS idx_quiz_questions_quiz_id ON quiz_questions(quiz_id, position);

CREATE TABLE IF NOT EXISTS quiz_attempts (
    id           BIGSERIAL PRIMARY KEY,
    quiz_id      BIGINT NOT NULL REFERENCES quizzes(id) ON DELETE CASCADE,
    student_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    answers      JSONB NOT NULL,
    score        INT NOT NULL,
    total        INT NOT NULL,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_quiz_attempts_quiz_student UNIQUE (quiz_id, student_id)
);

CREATE INDEX IF NOT EXISTS idx_quiz_attempts_quiz_id ON quiz_attempts(quiz_id);
CREATE INDEX IF NOT EXISTS idx_quiz_attempts_student_id ON quiz_attempts(student_id);
