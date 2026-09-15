-- One row per certname the console has dispatched an automatic initial
-- Puppet run for. The primary key is what makes the claim atomic: an
-- INSERT ... ON CONFLICT DO NOTHING either inserts (this caller won and
-- must dispatch) or does not (someone already claimed it), with no
-- separate read to race against. That is what keeps two console
-- instances observing the same connect event from both dispatching.
--
-- Rows are never deleted by normal operation - see design.md in
-- add-initial-run-on-first-connect for why a purged or deleted node is
-- deliberately not re-armed.
CREATE TABLE initial_run_claims (
    certname   TEXT PRIMARY KEY,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
