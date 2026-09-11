CREATE TABLE jobs (
    id           BIGSERIAL PRIMARY KEY,
    kind         TEXT NOT NULL CHECK (kind IN ('run', 'task', 'plan')),
    task_name    TEXT,
    plan_name    TEXT,
    params       JSONB,
    status       TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    triggered_by TEXT NOT NULL,
    started_at   TIMESTAMPTZ NOT NULL,
    finished_at  TIMESTAMPTZ
);

CREATE INDEX jobs_started_at_idx ON jobs (started_at DESC);

CREATE TABLE job_targets (
    id           BIGSERIAL PRIMARY KEY,
    job_id       BIGINT NOT NULL REFERENCES jobs (id) ON DELETE CASCADE,
    certname     TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    exit_code    INT,
    output       TEXT,
    error_detail TEXT,
    report_hash  TEXT
);

CREATE INDEX job_targets_job_id_idx ON job_targets (job_id);
