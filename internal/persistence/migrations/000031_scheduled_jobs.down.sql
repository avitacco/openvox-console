ALTER TABLE jobs DROP COLUMN schedule_name;
ALTER TABLE jobs DROP COLUMN schedule_id;
ALTER TABLE jobs DROP COLUMN target_source;
DROP TABLE scheduled_job_firings;
DROP TABLE scheduled_jobs;
