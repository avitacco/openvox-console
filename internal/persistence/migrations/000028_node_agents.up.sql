-- One row per node that has connected since client build tracking was
-- introduced (add-agent-auto-upgrade): which node-agent-client build it
-- last reported, and the state of the console's attempts to upgrade it.
--
-- Whether a node is out of date is NOT stored. It is worked out when
-- read, from this row and the client build the reading console embeds,
-- so deploying a newer console changes every node's state without a
-- write.
--
-- capable is false for a client that answered the build-identity
-- request with "unsupported": it predates this change, cannot upgrade
-- itself, and needs the install script re-run once.
--
-- upgrade_state:
--   idle       nothing in progress
--   requested  one console instance has told the node to upgrade to
--              target_digest and is waiting for it to report that
--              build. Moving a row into this state is a conditional
--              UPDATE, which is what keeps two instances from
--              instructing the same node.
--   failed     the last attempt failed; retried at next_attempt_at
--   exhausted  attempts at target_digest ran out; not retried until the
--              console serves a different build or an operator asks
CREATE TABLE node_agents (
    certname         TEXT PRIMARY KEY,
    capable          BOOLEAN NOT NULL DEFAULT false,
    digest           TEXT NOT NULL DEFAULT '',
    build_time       TIMESTAMPTZ,
    version          TEXT NOT NULL DEFAULT '',
    os               TEXT NOT NULL DEFAULT '',
    arch             TEXT NOT NULL DEFAULT '',
    install_method   TEXT NOT NULL DEFAULT '',
    accepts_upgrades BOOLEAN NOT NULL DEFAULT true,
    reported_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    upgrade_state    TEXT NOT NULL DEFAULT 'idle'
        CHECK (upgrade_state IN ('idle', 'requested', 'failed', 'exhausted')),
    target_digest    TEXT NOT NULL DEFAULT '',
    attempts         INTEGER NOT NULL DEFAULT 0,
    last_error       TEXT NOT NULL DEFAULT '',
    requested_at     TIMESTAMPTZ,
    next_attempt_at  TIMESTAMPTZ
);

-- The claim counts rows in 'requested' to bound how many nodes upgrade
-- at once, and the sweep lists them to time them out.
CREATE INDEX node_agents_requested_idx ON node_agents (upgrade_state) WHERE upgrade_state = 'requested';
