CREATE TABLE activity_log (
    id          BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    category    TEXT NOT NULL,
    action      TEXT NOT NULL,
    actor       TEXT NOT NULL,
    summary     TEXT NOT NULL
);

CREATE INDEX activity_log_occurred_at_idx ON activity_log (occurred_at DESC);
