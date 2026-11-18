## 1. Shared pieces

- [x] 1.1 `nodescope.Scope.Outside(certnames)` with tests (fleet-wide returns none, order kept, duplicates reported once)
- [x] 1.2 One response helper for the refusal: 403 with `error` and `outOfScope`, used by every action handler
- [x] 1.3 Pass `nodeScope` to the orchestrator and agent-upgrade handler sets in `internal/app`

## 2. Actions

- [x] 2.1 Orchestrator: runs, tasks and plans refuse whole when any target is out of scope, before a job or plan is created
- [x] 2.2 Package reporting toggle (`PUT /nodes/{name}/packages/reporting`)
- [x] 2.3 Client upgrade (`POST /nodes/{name}/agent-upgrade`)
- [x] 2.4 Node removal (`DELETE /nodes/{name}`), checked before the existence lookup
- [x] 2.5 Certificate sign, revoke and clean, checked before any CA call
- [x] 2.6 Handler tests per action: in scope proceeds; out of scope is 403 naming the node with no side effect; unknown node and hidden node are refused identically for a scoped caller; fleet-wide caller unchanged; scope resolution error is 502
- [x] 2.7 Test: a certname pinned into a granted group directly under the root can be signed by a scoped caller before it has facts; an unpinned one cannot

## 3. Jobs

- [x] 3.1 `JobFilter.Certnames` in `ListJobs`: jobs with at least one matching target, correct total and paging, counts and previews over matching targets only
- [x] 3.2 `GetJob` and plan steps return only in-scope targets; a job with none is not found; steps with none are omitted
- [x] 3.3 Detail response sets `targetsHidden` when a scoped caller's view was trimmed
- [x] 3.4 Store and handler tests: job inside, outside and spanning scope; plan with mixed steps; list totals; fleet-wide unchanged

## 4. Route sweep

- [x] 4.1 Extend the route pattern to every method; every permission-gated route is swept or exempt with a reason
- [x] 4.2 Stand-ins record side effects (CA calls, node dispatches, openvoxdb commands); each action route as the scoped user against the hidden node gives 403 and none
- [x] 4.3 Move job routes from exempt to swept, with seeded jobs for the visible and hidden node
- [x] 4.4 Confirm the sweep fails when one handler's scope check is removed, then restore it

## 5. Frontend

- [x] 5.1 Shared helper that renders an out-of-scope refusal naming the nodes
- [x] 5.2 Use it on the nodes page (bulk run, bulk task, certificate actions), node page (run, task, upgrade, remove, package reporting), group page (run on group) and task dialog
- [x] 5.3 Job page: note when nodes are hidden; jobs list needs no change beyond showing the trimmed counts
- [x] 5.4 Extract and merge strings; translate German; report the status of the other locales

## 6. Docs

- [x] 6.1 Guide `use/access.md`: replace the "see, not do" warning with what is limited (reads, actions, jobs) and what is not (activity log, group definitions, service tokens); separate read and action scope example
- [x] 6.2 Guide `use/run-jobs.md`: a run naming a node outside your groups is refused whole; job history follows current group membership
- [x] 6.3 Guide `nodes/certificates.md`: signing as a scoped team - pin the certname into the team's group first; otherwise a fleet-wide role signs
- [x] 6.4 Guides `nodes/remove.md` and `nodes/agent-upgrades.md`: the action needs the node in your groups
- [x] 6.5 `architecture-summary.md` section 3 and `operations.md` (narrowed roles lose out-of-scope actions on upgrade; what remains unscoped)
- [x] 6.6 Rebuild the site (`make marketing-build`; `make marketing` if a screenshot changed)

## 7. Verification

- [x] 7.1 `make test`, `go vet ./...`, gofmt, `openspec validate add-group-scoped-node-actions --strict`
- [x] 7.2 Live: a user who reads All Nodes and runs on one group - run inside the group succeeds, a run outside and a mixed selection are refused naming the nodes, and no job appears
- [x] 7.3 Live: remove, upgrade and certificate actions on an out-of-scope node are refused; the node and its certificate are unchanged
- [x] 7.4 Live: the Jobs page as a scoped user shows only their nodes' jobs and results, checked in the browser
- [x] 7.5 Live: an existing fleet-wide user can still perform each action
