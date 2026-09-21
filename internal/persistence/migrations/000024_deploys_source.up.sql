-- Deploy attempts recorded before multi-source support all came from
-- the single CONSOLE_CONTROL_REPO_URL control repo, which is exactly
-- what the default names - so existing rows backfill correctly.
--
-- The DEFAULT is kept after the backfill rather than dropped: it is
-- what lets the previous binary's INSERT, which does not mention
-- source, keep working against this schema, making a rollback of the
-- application safe without also reverting the migration.
ALTER TABLE deploys ADD COLUMN source TEXT NOT NULL DEFAULT 'control';

CREATE INDEX deploys_source_idx ON deploys (source);
