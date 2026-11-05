CREATE TABLE IF NOT EXISTS submission_files (
    submission_id BIGINT NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    file_id       BIGINT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    PRIMARY KEY (submission_id, file_id)
);

CREATE INDEX IF NOT EXISTS idx_submission_files_file_id ON submission_files (file_id);
