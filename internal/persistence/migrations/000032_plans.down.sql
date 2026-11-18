-- A schedule whose action is a plan cannot exist without the columns
-- that hold it: such schedules are deleted, with their firings.
DELETE FROM scheduled_jobs WHERE kind = 'plan';
ALTER TABLE scheduled_jobs DROP CONSTRAINT scheduled_jobs_kind_check;
ALTER TABLE scheduled_jobs ADD CONSTRAINT scheduled_jobs_kind_check CHECK (kind IN ('run', 'task'));
ALTER TABLE scheduled_jobs DROP COLUMN steps;
ALTER TABLE scheduled_jobs DROP COLUMN plan_name;
DROP TABLE plans;
