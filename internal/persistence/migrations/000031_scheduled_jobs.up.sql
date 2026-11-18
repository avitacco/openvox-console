-- Scheduled jobs, and where a job's targets came from
-- (add-pql-targeting-and-scheduled-jobs).
--
-- A schedule is a Puppet run or a task saved to start later, once or
-- repeatedly. Its targets are worked out each time it fires, and it
-- fires with its owner's permissions as they are at that moment.
--
-- next_run_at is the claim: an instance fires a schedule by moving
-- next_run_at forward with a compare-and-set update, so exactly one
-- instance wins each due time. Null means nothing further is due.
--
-- owner_kind is needed because a username and a service token's name
-- share one string space.
CREATE TABLE scheduled_jobs (
    id              BIGSERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('run', 'task')),
    run_options     JSONB,
    task_name       TEXT,
    environment     TEXT,
    timeout_seconds INT,
    -- A task's parameters with sensitive values redacted; the values
    -- themselves are in sealed_params, sealed with the secrets key.
    params          JSONB,
    sealed_params   BYTEA,
    target          JSONB NOT NULL,
    start_at        TIMESTAMPTZ NOT NULL,
    repeat_every    INT,
    repeat_unit     TEXT CHECK (repeat_unit IN ('minute', 'hour', 'day', 'week')),
    next_run_at     TIMESTAMPTZ,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    owner           TEXT NOT NULL,
    owner_kind      TEXT NOT NULL CHECK (owner_kind IN ('user', 'service_token')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((repeat_every IS NULL) = (repeat_unit IS NULL)),
    CHECK (repeat_every IS NULL OR repeat_every >= 1)
);

CREATE INDEX scheduled_jobs_due_idx ON scheduled_jobs (next_run_at) WHERE enabled AND next_run_at IS NOT NULL;

-- One row each time a schedule came due. 'firing' is a claim whose
-- outcome was never written: the instance stopped in between, and the
-- firing is not retried.
CREATE TABLE scheduled_job_firings (
    id          BIGSERIAL PRIMARY KEY,
    schedule_id BIGINT NOT NULL REFERENCES scheduled_jobs (id) ON DELETE CASCADE,
    due_at      TIMESTAMPTZ NOT NULL,
    fired_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    outcome     TEXT NOT NULL CHECK (outcome IN ('firing', 'started', 'skipped', 'missed')),
    detail      TEXT,
    job_id      BIGINT REFERENCES jobs (id) ON DELETE SET NULL
);

CREATE INDEX scheduled_job_firings_schedule_idx ON scheduled_job_firings (schedule_id, id DESC);

-- target_source: the query or node group a job's targets were resolved
-- from; null for a job given a list of nodes. schedule_name is kept
-- beside schedule_id so a job still names a schedule that was deleted.
ALTER TABLE jobs ADD COLUMN target_source JSONB;
ALTER TABLE jobs ADD COLUMN schedule_id BIGINT REFERENCES scheduled_jobs (id) ON DELETE SET NULL;
ALTER TABLE jobs ADD COLUMN schedule_name TEXT;
