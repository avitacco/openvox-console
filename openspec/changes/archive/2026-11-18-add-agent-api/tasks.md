# Tasks

## 1. Data model

- [x] 1.1 Migration 000041: `agent_tokens`, `agent_requests`, nullable `jobs.agent_request_id` and approver columns, with down migration. Verify: migrate up/down/up on the test database.
- [x] 1.2 `internal/agentrequests` store: create, get, list (by token, by status), atomic decide, expire past-due, link job; sealed params. Verify: store tests, including two concurrent decisions yielding one winner.

## 2. Agent tokens

- [x] 2.1 Agent token type in `rbac`: issue with mandatory bounded expiry, permissions (refusing `agent-requests:*`), node scopes, task grants; registry rows; revoke through the existing revoker. Verify: tests for issuing, expiry bounds, refusal of approve permission, revocation.
- [x] 2.2 Type guard: agent tokens accepted only at `/mcp` and the agent request endpoints; every other endpoint refuses them, and `/mcp` refuses user and service tokens. Verify: test each combination.
- [x] 2.3 Permissions `agent-requests:read` and `agent-requests:approve`; REST endpoints to create, list, revoke agent tokens (`rbac:admin`). Verify: handler tests; permission lists updated in the role editor sweep tests.

## 3. Requests and approvals

- [x] 3.1 Service: `Submit` for task, run, plan (including code plan) and patch, calling the matching `Check*` with the token's scope and allowlist, freezing resolved targets and sealing sensitive values. Verify: tests per kind, plus refusals (scope, allowlist, bad params, no secrets key).
- [x] 3.2 Service: `Approve`/`Reject` with the approver rules (permission, `orchestrator:run` over all targets, not the token owner), token re-validation, re-check, start through `Starter` with `JobOrigin` carrying the request and approver. Verify: tests for each refusal and for token revoked while waiting.
- [x] 3.3 Expiry sweep in the reaper worker and TTL config. Verify: test that past-due requests expire and cannot be approved.
- [x] 3.4 REST handlers for the Approvals page: list, get, approve, reject; sensitive values masked. Verify: handler tests, including a secret never appearing in any response.
- [x] 3.5 Orchestrator: job origin carries the agent request, token and approver; shown in job details and filterable. Verify: orchestrator tests and a node-scope sweep update.

## 4. MCP endpoint

- [x] 4.1 `internal/agentapi` transport: JSON-RPC over POST, `initialize`, `ping`, `tools/list`, `tools/call`, notifications; config flag, off by default, not routed when off. Check for a small compatible Go MCP SDK first and record the choice in design.md. Verify: protocol tests and the disabled-path test.
- [x] 4.2 Read tools: nodes, node detail (facts, reports), groups, task catalog and task detail, jobs and job detail, patch inventory, resolve targets, own requests. Verify: tests that each is scoped and masks sensitive values.
- [x] 4.3 Change tools: run task, run Puppet, run plan, run patch, each submitting a request; tools listed only when the token allows. Verify: tests that no job is ever created by a call.
- [x] 4.4 Per-token rate limit, body and result caps, pending-request cap. Verify: tests for each limit and that tokens are independent.
- [x] 4.5 Audit: agent calls and request lifecycle events, with no secrets. Verify: audit tests, including a sensitive parameter scan.
- [x] 4.6 OpenAPI document from the route table at `/api/v1/openapi.json`, with a drift test against registered routes. Verify: document validates and the drift test fails when a route is added without an entry.

## 5. Console UI

- [x] 5.1 Approvals page: pending and recent requests, detail view, approve/reject with note, pending badge in the navigation, hidden actions without the approve permission. Verify: live screenshots, and a node-scope check.
- [x] 5.2 Agent token section on the Admin page: create (with expiry and node groups), list, revoke, shown-once token, explanation of approval. Verify: live screenshots.
- [x] 5.3 Job page and list show the agent request and approver. Verify: live screenshot of an approved job.
- [x] 5.4 Translations in all 15 locales. Verify: `go run ./i18n status` shows full coverage.

## 6. Documentation and verification

- [x] 6.1 Guide `marketing/guides/use/agents.md` (set up a token and role, connect a client, approve requests, what agents can and cannot do), update the roles guide and rebuild the marketing site. Verify: `make marketing-build`.
- [x] 6.2 `operations.md`: config variables, token lifecycle, rate limit behavior in a cluster, request expiry, audit entries.
- [x] 6.3 Live check on the dev stack: connect an MCP client (Claude Code) to `/mcp`, read nodes, submit a task, see it pending, approve it in the UI, read the result; also reject, expire, and revoke-while-pending. Record evidence under the change.
- [x] 6.4 `openspec validate add-agent-api --strict`, full test suite, `go vet`, `make frontend`.
