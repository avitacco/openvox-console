-- Saved plans, and plans as a schedule's action
-- (add-plans-ui-and-account-policy).
--
-- A saved plan is a template: a name and an ordered list of steps, each
-- a Puppet run with its options or a task with its environment,
-- parameters and timeout. It names no nodes and holds no sensitive
-- value; both are given when it is run.
CREATE TABLE plans (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    steps       JSONB NOT NULL,
    created_by  TEXT NOT NULL,
    updated_by  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A scheduled plan keeps its own copy of the steps: it fires with its
-- owner's access, so it must not follow a saved plan somebody else can
-- edit. Sensitive values of its task steps are in sealed_params, as a
-- scheduled task's are, keyed by step.
ALTER TABLE scheduled_jobs ADD COLUMN plan_name TEXT;
ALTER TABLE scheduled_jobs ADD COLUMN steps JSONB;
ALTER TABLE scheduled_jobs DROP CONSTRAINT scheduled_jobs_kind_check;
ALTER TABLE scheduled_jobs ADD CONSTRAINT scheduled_jobs_kind_check CHECK (kind IN ('run', 'task', 'plan'));
