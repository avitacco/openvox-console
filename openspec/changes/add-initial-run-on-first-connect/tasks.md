## 1. Claim record

- [x] 1.1 Add migration `000020_initial_run_claims` (up and down) creating a
  table keyed by certname with a claimed-at timestamp, and verify
  `persistence.Migrate` applies it against a real Postgres and the down
  migration reverses it
- [x] 1.2 Add a store method that claims a certname atomically - insert with
  `ON CONFLICT DO NOTHING`, returning whether this caller won the claim -
  and verify with a Postgres-backed test that two concurrent claims for the
  same certname yield exactly one winner

## 2. Connection notifications

- [x] 2.1 Add an observer hook to `internal/nodetransport` that reports a
  certname when a node connects, invoked from the existing
  `$SYS.ACCOUNT.*.CONNECT` subscription, and verify a test observer
  receives the certname when a node connects
- [x] 2.2 Make notification delivery non-blocking and panic-safe so a slow
  or failing observer cannot affect the transport, and verify with a test
  using an observer that blocks and one that panics - the node stays
  connected and dispatch to it still succeeds
- [x] 2.3 Rewrite the `Registry` doc comment so it distinguishes the
  connected-state map (still observability-only, dispatch still relying on
  the no-responders signal) from connect notifications (now load-bearing),
  and verify by reading it back against design.md's decision

## 3. Initial-run trigger

- [x] 3.1 Add the trigger component that, given a connected certname,
  checks inventory via `openvoxdb.NodeByCertname`, claims the certname, and
  dispatches a run through `Store.CreateJob` + `Dispatcher.DispatchRun`
  with `triggeredBy` set to `system:initial-run`; verify with tests that a
  node with no inventory gets exactly one job and an inventoried node gets
  none
- [x] 3.2 Verify by test that an openvoxdb error results in no claim and no
  dispatch, so the node remains eligible on its next connection
- [x] 3.3 Verify by test that a second notification for the same certname
  dispatches nothing, including when the first run has not yet reported
- [x] 3.4 Add the bounded worker pool draining a buffered notification
  channel, dropping with a warning log (including the certname) when full,
  and verify with a test that a burst of notifications dispatches every
  node's run without unbounded concurrency

## 4. Wiring

- [x] 4.1 Wire the transport observer to the trigger in
  `cmd/console/main.go`, keeping it inert when the node transport or
  openvoxdb client is not configured, and verify the console starts
  normally in both configured and unconfigured states
- [x] 4.2 Verify the dispatched job appears in the activity log and audit
  log attributed to `system:initial-run`, by test against the existing
  recorders

## 5. End-to-end verification

- [x] 5.1 Against a live dev stack, enrol a real node that has never run,
  and verify a run is dispatched automatically on connect, the job appears
  in the job list attributed to the system, and the node's facts appear in
  the console without any manual trigger
- [x] 5.2 Restart that node's agent several times and verify no further
  automatic job is created
- [x] 5.3 Run `gofmt -l .`, `go vet ./...` and `go test -p 1 ./...` and
  verify all pass
