DROP INDEX jobs_parent_job_id_idx;
ALTER TABLE jobs DROP COLUMN step_order;
ALTER TABLE jobs DROP COLUMN parent_job_id;
