-- Both nullable, and deliberately so.
--
-- size_bytes: every row predating this migration has no measured size,
-- and a deploy that fails before producing a tree never gets one. NULL
-- means "not measured", which must stay distinguishable from a real 0
-- - 0 would read as an empty repository.
--
-- environment: ref does not identify what a deploy deployed. It holds
-- the pushed commit SHA for webhook-triggered deploys and the branch
-- name for manual ones, so the environment was unrecoverable from a
-- row. Recording it is what lets an environment be attributed to the
-- repository that actually deployed it rather than guessed at by
-- prefix matching.
ALTER TABLE deploys
    ADD COLUMN size_bytes  BIGINT,
    ADD COLUMN environment TEXT;

CREATE INDEX deploys_environment_idx ON deploys (environment);
