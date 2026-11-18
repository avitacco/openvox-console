-- Plans from environment code (add-code-plans).
--
-- A code plan is a job of its own kind: it has no targets of its own, its
-- task steps are child jobs (parent_job_id, step_order), and it keeps a
-- trace of every step it ran, the value it returned and, when it failed,
-- why. plan_name is module::plan and environment is where it was found;
-- params are the parameters given, with sensitive values redacted. Like a
-- patch run it reports progress through heartbeat_at.
ALTER TABLE jobs DROP CONSTRAINT jobs_kind_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_kind_check CHECK (kind IN ('run', 'task', 'plan', 'patch', 'codeplan'));
ALTER TABLE jobs ADD COLUMN plan_trace JSONB;
ALTER TABLE jobs ADD COLUMN result JSONB;
ALTER TABLE jobs ADD COLUMN error TEXT NOT NULL DEFAULT '';
