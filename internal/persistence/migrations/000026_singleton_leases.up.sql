-- Coordinates work that must run on exactly one console instance at a
-- time (see internal/leases). Keyed by a free-form name so any singleton
-- can take one, rather than by a provider id as the vulnerability-only
-- predecessor was.
CREATE TABLE singleton_leases (
    name       TEXT PRIMARY KEY,
    holder     TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- Expiry is judged by the database clock in every query, so instances
-- with skewed clocks still agree on whether a lease is live.
CREATE INDEX singleton_leases_expires_at_idx ON singleton_leases (expires_at);
