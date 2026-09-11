CREATE TABLE service_tokens (
    id         BIGSERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    jti        TEXT NOT NULL UNIQUE,
    permissions TEXT[] NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
