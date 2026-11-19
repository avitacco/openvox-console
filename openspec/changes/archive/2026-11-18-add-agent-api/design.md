# Design

## Context

See proposal.md for motivation. What exists today, found by reading the code:

- **Tokens.** `rbac.Issuer` signs ES256 JWTs carrying `Permissions`, `NodeScopes` and `TaskGrants`. A service token is issued with **no `NodeScopes`**, which `NodeGroupsFor` reads as fleet-wide, and with a 100-year expiry; it is revoked explicitly. `IssueUserTokenWithTasks` already supports per-permission node scopes. Revocation by jti is persisted and propagated over NATS.
- **Starting work.** `orchestrator.Starter` has `CheckRun/CheckTask/CheckPlan/CheckPatch` (validate without starting) and `StartRun/StartTask/StartPlan/StartPatch`, driven by a `StartRequest{Target, Scope, Tasks, Actor, Schedule}`. `CheckTask` returns `StoredParams` (sensitive values redacted) and `Sensitive` (the real values).
- **Stored requests.** Scheduled jobs already store a request and start it later: sensitive values are sealed with `internal/sealer`, and an `Authority` func re-derives the owner's scope at firing time. Approvals follow the same pattern.
- **Routing.** `internal/app/app.go` builds route groups per run mode; handlers take an `authorize(permission, next)` wrapper. There is no rate limiting anywhere today.
- **Job origin.** `JobOrigin{Source, Schedule}` is recorded per job.

## Goals / Non-Goals

**Goals:**
- Agents can read broadly and can change nothing without a person's approval.
- The approver sees exactly what will run, and that is what runs.
- Reuse the Starter, scope, task allowlist, sealer and audit paths; add no second implementation of any rule.

**Non-Goals:**
- A stdio MCP binary, auto-approve policies, MCP features other than tools (no resources, prompts or sampling), OAuth for MCP clients.
- Changing service tokens or any existing endpoint.

## Decisions

**1. Agent tokens are a new kind, not service tokens.** Service tokens are fleet-wide and never expire, which is wrong for agents. Agent tokens carry `Type: "agent"`, an explicit permission list, `NodeScopes` for those permissions, task grants, and an `exp`. The agent endpoint accepts only this type; every other endpoint refuses it, so a leaked agent token cannot be replayed against the REST API. A table `agent_tokens` records name, jti, owner (the issuing user), scope and expiry for listing and the self-approval rule. *Alternative: add scope and expiry to service tokens* - rejected: it changes what enc-bridge and other holders can do and leaves the "type" guard impossible.

**2. Expiry is mandatory, with a configured maximum** (default 90 days). A fixed bound makes revocation a backstop and not the only control. The admin UI proposes 30 days.

**3. Change tools store a request, never a job.** Each write tool builds the same `StartRequest` the REST handler would, runs the matching `Check*` (scope, allowlist, parameters, environment, targets), and stores the result in `agent_requests`. The stored fields are the frozen request: kind, JSON of task/options/plan steps, redacted params, resolved targets (or query and group, and the targets resolved at that moment), environment, and the token. Sensitive values are sealed (`agent_request:<id>:params`). *Alternative: create the job in a "pending approval" status* - rejected: the orchestrator, reaper and dispatcher would all need to know a new state, and nodes could be claimed by a job that may never run.

**4. Authority at approval time = the token, re-read; plus the approver.** On approval the service loads the agent token row (not revoked, not expired) and rebuilds its scope and task allowlist from it, then calls the matching `Check*` again and `Start*` with `Actor` = the agent token's name and a `JobOrigin` carrying the request and approver. If the token is gone or the check fails, the request becomes `failed` with the reason. The **approver must also hold `agent-requests:approve` and `orchestrator:run` over every target** (via the existing node scope helper), so approval cannot be used to run on nodes the approver could not touch. **Decision point (default chosen):** the token's issuing user may not approve its requests; this blocks a person approving their own agent's work. If an organization has one admin, they would need a second account; a config switch to allow it can be added later.

**5. Requests resolve targets once.** A query or group is resolved at submission and the node list frozen, so the approver sees the exact nodes. For patch runs and plans, the frozen options and steps are stored the same way. *Trade-off:* a node that joins between submission and approval is not included; the agent can resubmit.

**6. Lifecycle.** `pending → approved | rejected | expired | failed`. A decision is one `UPDATE ... WHERE status='pending' AND expires_at > now()`; the row count says whether this caller won, which is what makes two simultaneous approvals start one job. `approved` records `job_id`. A reaper-style sweep (in the existing reaper worker) marks past-due requests `expired` and audits it. Default TTL 1 hour; `CONSOLE_AGENT_REQUEST_TTL`.

**7. Package layout.** `internal/agentrequests` owns the table, the service (submit, decide, expire) and the REST handlers for the Approvals page. `internal/agentapi` owns the MCP transport, the tool definitions, rate limiting, OpenAPI serving and the agent-token management handlers. Tools are a table of `{name, description, input schema, permission, handler}`, so a later stdio binary can reuse them. `agentapi` depends on `agentrequests` and the orchestrator, not the other way round.

**8. MCP transport.** Streamable HTTP, JSON-RPC over POST at `/mcp`, stateless (no server-side sessions): `initialize`, `tools/list`, `tools/call`, `ping`, and `notifications/*` accepted and ignored. Stateless keeps the endpoint working behind several console instances with no sticky routing. Implemented directly (about 300 lines) rather than adding a dependency. **Checked at implementation:** the official Go SDK (`github.com/modelcontextprotocol/go-sdk`, v1.x) exists and is maintained. It was not adopted: the endpoint needs only `initialize`, `ping`, `tools/list` and `tools/call`; its tool list and every call depend on the calling token, which a hand-written stateless handler expresses directly; and a new dependency on the path that authenticates automated callers is a cost for little gain. Revisit if the protocol surface (resources, prompts, streaming) grows. The SDK was judged from its description and version history, not by trying it. *Alternative: SSE streaming* - rejected: nothing here streams.

**9. Tool results are text plus structured JSON, masked.** Results are built from the same JSON the REST API returns, passed through the existing redaction. Sensitive task output markers and params follow the existing rules. Tool errors use MCP's `isError` with the console's refusal message, so an agent can correct itself.

**10. Rate limiting and bounds.** A per-token token bucket in memory (default 60 calls/min, `CONSOLE_MCP_RATE_LIMIT`); per instance, which is an accepted limit for a cluster (N instances allow up to N times the rate). Body cap 256 KiB. Pending requests per token capped at 20. Result sizes are capped with a truncation notice.

**11. Audit.** Agent tool calls reuse the audit categories of the matching REST call, with `actor` set to the token name and an `agent` marker. Request submit and every decision are audited unconditionally.

**12. OpenAPI.** The document describes the REST API for integrations that use a user or service token; agent tokens cannot call that API (decision 1), so an agent that does not speak MCP has no approval-gated path and should be given one only through MCP. The guide says so. Generated from a hand-maintained route table in `agentapi` (method, path, permission, summary, request and response shape) for the endpoints an agent could reasonably use, served at `GET /api/v1/openapi.json`. A test fails if a registered `/api/v1` route is missing from the table or marked excluded, so it cannot drift silently. *Alternative: annotate every handler* - rejected as too invasive.

## Risks / Trade-offs

- **Approval fatigue:** a flood of requests trains people to click approve → per-token pending cap, request TTL, and the page shows the full detail (tasks and node counts) plus filtering by token.
- **A prompt-injected agent crafts a harmful but valid request** → the human sees the exact task, parameters and nodes; roles, node scope and task allowlist still bound what can even be requested.
- **Sealed secrets without a secrets key** → refused, as for schedules.
- **Per-instance rate limits** in a cluster → documented; revisit if needed.
- **Hand-written MCP layer** may lag protocol changes → small and isolated in its package; protocol version negotiated in `initialize`.
- **Frozen target list goes stale** → expiry is short; documented.

## Migration Plan

Migration 000041 adds `agent_tokens`, `agent_requests` and `jobs.agent_request_id` (nullable); the down migration drops them. Nothing changes for existing tokens, roles or jobs. The endpoint is off until `CONSOLE_MCP_ENABLED` is set, so deploying is inert; rollback is unsetting the flag or reverting.

## Open Questions

- Whether the approvals page should also notify (email/webhook) when a request arrives. Not needed for correctness; the badge and page ship first.
