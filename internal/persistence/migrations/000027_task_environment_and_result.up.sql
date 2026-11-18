-- The code environment a task job's task was taken from, and each
-- target's structured task result (add-task-execution). Both nullable:
-- runs and plans have no environment, and only task targets a result.
ALTER TABLE jobs ADD COLUMN environment TEXT;
ALTER TABLE job_targets ADD COLUMN result JSONB;
