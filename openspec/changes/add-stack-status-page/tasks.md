## 1. Bus request/collect support

- [x] 1.1 Add a request/collect method to `messaging.Bus` that publishes on
  a subject with a per-request reply subject and gathers replies until a
  deadline, returning the replies and whether the deadline elapsed before
  all had arrived; verify a unit test covers one replier, several
  repliers, and no replier
- [x] 1.2 Verify by a two-instance test that replies are collected from a
  peer instance, not only the local one
- [x] 1.3 Verify by test that the call returns within its deadline when a
  subscriber never replies, and reports that the deadline elapsed
- [x] 1.4 Document the pattern in `internal/messaging`'s package
  documentation alongside the existing fan-out/queue rule; verify the
  documentation names when to use each of the three

## 2. Per-instance status

- [x] 2.1 Add `internal/stackstatus` with an `Instance` type carrying id,
  hostname, run mode, HTTP address, health, start time, version and
  worker names; verify a unit test asserts every field is populated from
  a constructed instance
- [x] 2.2 Build an instance's own status from the active surface and the
  runtime checkers, with a per-check timeout shorter than the collection
  window; verify a test asserts a slow dependency yields an unhealthy
  instance rather than no answer
- [x] 2.3 Add the responder that answers status requests on the bus;
  verify a test asserts it replies with this instance's status
- [x] 2.4 Register the responder as a worker present in every mode's
  surface; verify the mode-table test asserts all five modes run it

## 3. Dependency reporting

- [x] 3.1 Add `Checker` implementations for openvoxdb and the CA client
  over their existing clients; verify tests assert healthy, unreachable,
  and not-configured outcomes
- [x] 3.2 Report each dependency with its configured target and one of
  healthy / unreachable / not-configured; verify a test asserts an
  unconfigured optional dependency is not reported as unhealthy
- [x] 3.3 Verify by test that a dependency reported differently by two
  instances is surfaced as a disagreement rather than silently taking the
  serving instance's answer

## 4. Aggregation

- [x] 4.1 Add the aggregator that asks the cluster, collects replies, and
  assembles instances grouped by mode with a count per mode; verify a
  test asserts grouping and counts for a mixed set of replies
- [x] 4.2 Compute the expected instance count by combining every reply's
  cluster view - routed peers from the largest peer view, leaves by
  summing each routed instance's own leaf count (the sets do not
  overlap); verify unit tests cover a single instance, an all-routed
  cluster, and a cluster with leaves attached to more than one peer
- [x] 4.3 Mark the result incomplete when fewer instances replied than
  expected, when no usable cluster view was returned, or when the bus
  could not be reached; verify tests cover all three causes
- [x] 4.4 Verify by test that a leaf attached to a peer other than the
  serving instance is accounted for, and that its silence marks the
  result incomplete even though the serving instance cannot see it
  directly
- [x] 4.5 Verify by test that a complete result is not marked incomplete
- [x] 4.6 Verify by test that an unclustered single instance yields
  exactly itself and is not marked incomplete

## 5. Permission

- [x] 5.1 Add `status:read` to the bootstrap admin permission set; verify
  the existing `allPermissions` test asserts its presence
- [x] 5.2 Verify by test that a freshly bootstrapped administrator's token
  carries `status:read`
- [x] 5.3 Verify by test that `status:read` can be granted to an existing
  role through the roles API like any other permission

## 6. HTTP API

- [x] 6.1 Add `GET /api/v1/status` returning the aggregate as JSON, gated
  on `status:read`; verify a test asserts the shape includes instances,
  dependencies, and the incompleteness flag
- [x] 6.2 Verify by test that a caller without `status:read` is refused
- [x] 6.3 Add the route as a `status` group in the mode table, present
  wherever the REST API is served; verify the mode-table test asserts
  `web` and `all` serve it and `enc`, `orchestrator` and `worker` do not
- [x] 6.4 Verify by a multi-instance test that the endpoint reports every
  running instance of every mode

## 7. Console page

- [x] 7.1 Add `frontend/templates/pages/status.tmpl` and
  `frontend/src/status.js` rendering instances grouped by run mode with
  per-mode counts, using voxblocks components; verify the page renders
  against a seeded response
- [x] 7.2 Show each instance's mode, address, health, uptime, version and
  workers; verify by inspection against the spec's "An instance's detail
  is visible" scenario
- [x] 7.3 Render the dependency list with health and configured target,
  distinguishing not-configured from unreachable; verify the page shows
  all three states distinctly
- [x] 7.4 Surface an incomplete picture visibly on the page; verify a test
  or inspection asserts the notice appears when the flag is set
- [x] 7.5 Add the permission-gated nav link in `layout.html.tmpl`,
  following the existing hidden-by-default pattern; verify the link is
  shown only with `status:read`
- [x] 7.6 Add the page's strings to the i18n catalogue; verify no
  hard-coded user-facing English remains in the template or script

## 8. Documentation

- [x] 8.1 Document the status page in `operations.md`, including that
  `status:read` must be granted to existing roles after upgrade; verify
  an operator can follow it without reading the code
- [x] 8.2 Document what the page cannot show - an instance unreachable on
  the bus - and how incompleteness is signalled; verify it is stated
  plainly rather than implied

## 9. Integration verification

- [x] 9.1 Verify against a split topology (`web`, `enc`, `orchestrator`,
  `worker`, several of each) that every instance appears with its correct
  mode and workers
- [x] 9.2 Verify that stopping one instance removes it from the next
  status request, and that the remaining instances are still reported
- [x] 9.3 Verify the existing mode-equivalence baseline still passes with
  the new worker and route group added deliberately in both places
- [x] 9.4 Run `openspec validate add-stack-status-page --strict` and the
  full test suite repeatedly; verify both pass consistently, not once
