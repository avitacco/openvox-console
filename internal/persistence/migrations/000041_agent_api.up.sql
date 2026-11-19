-- AI agent access (add-agent-api).
--
-- agent_tokens is the registry of tokens issued to agents. The signed
-- token is never stored; jti ties a presented token to its row, and
-- expires_at is what the token's own exp claim says. node_groups and
-- task_patterns are the scope the token was issued with, fixed for its
-- life; owner is the user who issued it, who may not approve its
-- requests.
CREATE TABLE agent_tokens (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT        NOT NULL UNIQUE,
    jti           TEXT        NOT NULL UNIQUE,
    owner         TEXT        NOT NULL,
    permissions   TEXT[]      NOT NULL,
    node_groups   BIGINT[]    NOT NULL DEFAULT '{}',
    task_patterns TEXT[]      NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL
);

-- A change an agent asked for, held until a person decides it. It outlives
-- its token: revoking a token fails its pending requests (the rbac store does
-- it in the same transaction) and leaves every request as a record, with the
-- token's name kept in token_name. What will
-- run is frozen here: body is the request as the console's own endpoint
-- would take it, with sensitive values redacted, and sealed holds those
-- values encrypted with the secrets key (null when there are none).
-- targets are the nodes resolved at submission. status is pending,
-- approved, rejected, expired or failed; a decision is one conditional
-- UPDATE, which is what makes two approvers start one job.
CREATE TABLE agent_requests (
    id          BIGSERIAL PRIMARY KEY,
    token_id    BIGINT      REFERENCES agent_tokens (id) ON DELETE SET NULL,
    token_name  TEXT        NOT NULL,
    kind        TEXT        NOT NULL CHECK (kind IN ('run', 'task', 'plan', 'patch', 'codeplan')),
    summary     TEXT        NOT NULL,
    body        JSONB       NOT NULL,
    sealed      BYTEA,
    targets     TEXT[]      NOT NULL DEFAULT '{}',
    status      TEXT        NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'failed')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    decided_by  TEXT,
    decided_at  TIMESTAMPTZ,
    note        TEXT        NOT NULL DEFAULT '',
    job_id      BIGINT,
    failure     TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX agent_requests_status_idx ON agent_requests (status, expires_at);
CREATE INDEX agent_requests_token_idx ON agent_requests (token_id, created_at DESC);

-- A job started by approving a request says which one, through which
-- token, and who approved it.
ALTER TABLE jobs ADD COLUMN agent_request_id BIGINT;
ALTER TABLE jobs ADD COLUMN agent_token TEXT;
ALTER TABLE jobs ADD COLUMN approved_by TEXT;
