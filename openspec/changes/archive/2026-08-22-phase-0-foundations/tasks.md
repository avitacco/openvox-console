## 1. Project skeleton

- [x] 1.1 Initialize the Go module and directory layout (cmd/console,
      internal/runtime, internal/messaging, internal/persistence,
      internal/web) and verify `go build ./...` succeeds
- [x] 1.2 Add a Dockerfile that builds and packages the single binary and
      verify `docker build` produces a runnable image

## 2. Service runtime (config, logging, health check)

- [x] 2.1 Implement configuration loading (file and/or env vars) and verify
      startup fails with a clear error when a required value is missing
- [x] 2.2 Wire structured logging and verify a startup log entry is emitted
      on successful boot
- [x] 2.3 Implement the health check HTTP endpoint and verify it responds
      success when dependencies are healthy
- [x] 2.4 Wire health check dependency reporting for Postgres and NATS and
      verify the endpoint reports a specific unhealthy dependency when one
      is unreachable

## 3. Persistence (Postgres + migrations)

- [x] 3.1 Add the Postgres connection pool, configured from the loaded
      config, and verify the binary connects successfully against a local
      Postgres instance
- [x] 3.2 Verify the binary logs the failure and reports Postgres unhealthy
      (without crashing) when Postgres is unreachable at startup
- [x] 3.3 Add `golang-migrate/migrate` and an initial (empty/bootstrap)
      migration, and verify running the migration tool against a fresh
      database applies it and records it as applied
- [x] 3.4 Verify re-running the migration tool against an up-to-date
      database makes no changes and reports the schema as current

## 4. Messaging (embedded NATS)

- [x] 4.1 Start an embedded, unclustered NATS server as part of binary
      startup, before the HTTP server begins accepting requests, and
      verify the health check reports NATS healthy
- [x] 4.2 Add an internal publish/subscribe helper package and verify a
      unit test in which one internal subscriber receives a message
      published by another component in-process

## 5. Web shell (embedded frontend)

- [x] 5.1 Wire an `embed.FS`-based static asset pipeline for the frontend
      and verify the binary serves assets with no external files present
      on disk
- [x] 5.2 Add `@openvoxproject/voxblocks` as a frontend dependency and
      verify it builds into the embedded asset bundle
- [x] 5.3 Build the placeholder UI shell page using voxblocks components
      and verify requesting the root path returns a shell composed of
      voxblocks elements rather than bespoke markup

## 6. Integration verification

- [x] 6.1 Run the binary end to end against a local Postgres instance and
      verify: it starts, connects to Postgres, serves the health check as
      healthy, and NATS is running in-process (this phase's exit criteria)
