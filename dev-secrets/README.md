# Development secrets

Throwaway values for the local dev stack only, referenced by
`docker-compose.override.yml`. They are committed deliberately: these are
the same non-secrets the compose file hardcoded before secrets existed
(`console`/`console`), and committing them keeps `make up` working on a
fresh clone with no setup step.

**Production uses a different directory** - `./secrets` by default (see
`CONSOLE_SECRETS_DIR` in `docker-compose.yml`), which is gitignored and
which you populate yourself. Nothing here is used by
`docker compose -f docker-compose.yml up`.

In dev the console runs on the host via `make run` and reads `.env`, so
`console_postgres_dsn`, `console_bootstrap_admin_password` and
`enc_bridge_token` here are only placeholders to satisfy the secret
mounts - the real dev values come from `.env`.
