DROP INDEX deploys_environment_idx;

ALTER TABLE deploys
    DROP COLUMN size_bytes,
    DROP COLUMN environment;
