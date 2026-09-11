CREATE TABLE deploys (
    id           BIGSERIAL PRIMARY KEY,
    ref          TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed')),
    started_at   TIMESTAMPTZ NOT NULL,
    finished_at  TIMESTAMPTZ,
    triggered_by TEXT NOT NULL,
    error_detail TEXT
);

CREATE INDEX deploys_started_at_idx ON deploys (started_at DESC);
