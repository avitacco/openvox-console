ALTER TABLE jobs ADD COLUMN parent_job_id BIGINT REFERENCES jobs (id) ON DELETE CASCADE;
ALTER TABLE jobs ADD COLUMN step_order INT;

CREATE INDEX jobs_parent_job_id_idx ON jobs (parent_job_id, step_order);
