-- A patch job cannot exist without its kind: removed with its targets.
DELETE FROM jobs WHERE kind = 'patch';
ALTER TABLE job_targets DROP COLUMN batch;
ALTER TABLE job_targets DROP COLUMN heartbeat_at;
ALTER TABLE jobs DROP COLUMN heartbeat_at;
ALTER TABLE jobs DROP CONSTRAINT jobs_kind_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_kind_check CHECK (kind IN ('run', 'task', 'plan'));
