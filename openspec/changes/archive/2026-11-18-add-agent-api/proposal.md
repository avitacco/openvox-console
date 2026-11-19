# Proposal

## Why

The console can already be driven by anything that holds a token, but an AI agent has no way to discover what the console offers, and nothing stops an agent that holds a task-running permission from acting on its own. Operators want agents to inspect the fleet freely and to *ask* for changes, with a person approving anything that alters a node.

## What Changes

- A built-in **MCP endpoint** (`/mcp`, streamable HTTP) that exposes console operations as tools to AI agents. Off unless `CONSOLE_MCP_ENABLED` is set. Tool definitions live in their own package.
- **Agent tokens**: a new token kind for agents, with an explicit permission list, node group scope, task allowlist and a **mandatory expiry**. Service tokens are unchanged.
- **Read tools** that run immediately: nodes, facts and reports, the task catalog, jobs and results, group and class information, patch inventory, target resolution and no-op or preview checks.
- **Write tools for all action kinds** (tasks, Puppet runs, plan runs including code plans, patch runs) that never start a job. They validate the request exactly as the console would, store it as a **pending approval request**, and return a request id.
- An **approval queue**: a new **Approvals** page and API where a person with the new permission approves or rejects a request. Parameters are frozen at request time and sensitive values are sealed and masked. Approval starts the job through the existing orchestrator. Requests expire.
- A **rate limit** and request size caps on `/mcp`, and an **OpenAPI document** of the console API for agents that do not speak MCP.
- Audit entries and job records mark work that came from an agent and who approved it.
- Console UI for agent tokens and approvals, 15-locale translations, guides, `operations.md`.

**Not changed:** service tokens, existing endpoints, and the way roles and node scope are evaluated for people.

## Capabilities

### New Capabilities
- `agent-access`: the MCP endpoint, agent tokens' use of it, the read tools, request-submitting tools, limits, and the OpenAPI document.
- `agent-approvals`: the pending-request lifecycle, who may approve, what an approver sees, freezing, expiry, and the Approvals page.

### Modified Capabilities
- `rbac`: agent tokens (permissions, node scope, task allowlist, mandatory expiry, management UI) and the new `agent-requests:read` / `agent-requests:approve` permissions.
- `orchestrator`: a job records the agent request it came from and the person who approved it.
- `audit-log-emission`: agent-originated calls and approval decisions are audited, with the agent and approver named.

## Impact

- New packages `internal/agentapi` (MCP, tools, limits, OpenAPI) and `internal/agentrequests` (store, service, handlers); wiring in `internal/app`.
- Migration 000041: `agent_tokens`, `agent_requests`, and a job origin column.
- `internal/rbac`: token issuing and a registry of agent tokens; `internal/orchestrator`: origin on jobs; frontend: Approvals page, agent-token section on the Admin page, a request badge on jobs.
- New config: `CONSOLE_MCP_ENABLED`, `CONSOLE_MCP_RATE_LIMIT`, `CONSOLE_AGENT_REQUEST_TTL`.
- Guides: new `use/agents.md`; updates to the roles guide.
