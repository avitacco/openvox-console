CREATE TABLE oidc_login_state (
    state         TEXT PRIMARY KEY,
    pkce_verifier TEXT NOT NULL,
    nonce         TEXT NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL
);
