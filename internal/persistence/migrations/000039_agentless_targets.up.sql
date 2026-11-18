-- Agentless SSH targets (add-agentless-ssh-targets).
--
-- A target is a host the console reaches over SSH rather than through a
-- node agent. Its login is either its own or a shared credential's. Every
-- secret column holds a sealed envelope (internal/sealer) bound to its row
-- and field; nothing here is ever readable as plaintext, and the API never
-- returns one. key_fingerprint is the public key's fingerprint, which is
-- not secret and lets an operator recognise a key.
CREATE TABLE agentless_credentials (
    id                 BIGSERIAL PRIMARY KEY,
    name               TEXT NOT NULL UNIQUE,
    username           TEXT NOT NULL,
    run_as             TEXT NOT NULL DEFAULT '',
    sealed_private_key BYTEA,
    sealed_passphrase  BYTEA,
    sealed_password    BYTEA,
    key_fingerprint    TEXT NOT NULL DEFAULT '',
    created_by         TEXT NOT NULL,
    updated_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT agentless_credentials_secret CHECK (sealed_private_key IS NOT NULL OR sealed_password IS NOT NULL)
);

-- The host key pin: pinned_* is the trusted key; proposed_* is what the
-- last check saw, waiting for an operator to accept; key_changed is set
-- when a pinned target presented another key (presented_fingerprint).
CREATE TABLE agentless_targets (
    id                    BIGSERIAL PRIMARY KEY,
    name                  TEXT NOT NULL UNIQUE,
    address               TEXT NOT NULL,
    port                  INT NOT NULL DEFAULT 22 CHECK (port BETWEEN 1 AND 65535),
    credential_id         BIGINT REFERENCES agentless_credentials (id) ON DELETE RESTRICT,
    username              TEXT NOT NULL DEFAULT '',
    run_as                TEXT NOT NULL DEFAULT '',
    sealed_private_key    BYTEA,
    sealed_passphrase     BYTEA,
    sealed_password       BYTEA,
    key_fingerprint       TEXT NOT NULL DEFAULT '',
    pinned_key            BYTEA,
    pinned_fingerprint    TEXT NOT NULL DEFAULT '',
    proposed_key          BYTEA,
    proposed_fingerprint  TEXT NOT NULL DEFAULT '',
    key_changed           BOOLEAN NOT NULL DEFAULT FALSE,
    presented_fingerprint TEXT NOT NULL DEFAULT '',
    last_check_at         TIMESTAMPTZ,
    last_check_ok         BOOLEAN,
    last_check_stage      TEXT NOT NULL DEFAULT '',
    last_check_error      TEXT NOT NULL DEFAULT '',
    created_by            TEXT NOT NULL,
    updated_by            TEXT NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- A target has a login of its own or a credential's, never neither.
    CONSTRAINT agentless_targets_login CHECK (
        credential_id IS NOT NULL
        OR (username <> '' AND (sealed_private_key IS NOT NULL OR sealed_password IS NOT NULL))
    )
);
CREATE INDEX agentless_targets_credential_idx ON agentless_targets (credential_id);
