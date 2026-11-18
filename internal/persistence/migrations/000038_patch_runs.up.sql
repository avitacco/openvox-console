-- Patch runs (add-patch-runs).
--
-- A patch run is a job of its own kind. It reports progress rather than
-- waiting on one request, so a job and each of its targets carry the time
-- progress was last reported: the stale-job reaper judges a patch job by
-- that, not by its age. A target also records which batch it was patched
-- in.
ALTER TABLE jobs DROP CONSTRAINT jobs_kind_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_kind_check CHECK (kind IN ('run', 'task', 'plan', 'patch'));
ALTER TABLE jobs ADD COLUMN heartbeat_at TIMESTAMPTZ;
ALTER TABLE job_targets ADD COLUMN heartbeat_at TIMESTAMPTZ;
ALTER TABLE job_targets ADD COLUMN batch INT;

