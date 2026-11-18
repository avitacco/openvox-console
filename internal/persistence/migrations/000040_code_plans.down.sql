-- A code plan cannot exist without its kind: removed with its step jobs.
DELETE FROM jobs WHERE kind = 'codeplan';
ALTER TABLE jobs DROP COLUMN error;
ALTER TABLE jobs DROP COLUMN result;
ALTER TABLE jobs DROP COLUMN plan_trace;
ALTER TABLE jobs DROP CONSTRAINT jobs_kind_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_kind_check CHECK (kind IN ('run', 'task', 'plan', 'patch'));
