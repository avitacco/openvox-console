# Live check, on the dev stack

Console started with `CONSOLE_MCP_ENABLED=true` against the dev Postgres and the
`task-e2e-agent` node. Clients: a small JSON-RPC client written for the check
(protocol, tools, edge cases) and **Claude Code** (`claude -p` with an isolated
`--mcp-config` using the agent token, so no user settings were touched).

| Step | Result |
| --- | --- |
| `initialize`, `tools/list` with a token holding nodes/classifier/orchestrator read + run | 22 tools; `request_*` tools absent for a read-only token (unit-tested) |
| Same agent token on a REST endpoint | `401`: agent tokens work only at `/mcp` |
| Claude Code: "call `list_nodes`" | returned `openvoxserver, task-e2e-agent.example.com` |
| Claude Code: `request_puppet_run` (no-op) then `get_request` | request 111 **pending**, "nothing has run yet"; no job existed |
| `request_task` `task_fixture::echo` | parameter errors returned to the agent with the field names; corrected call stored as request 78 |
| `request_task` `task_fixture::secret` with `password` | stored as request 79; listing, detail and console log held no trace of the value |
| Reviewer approves 78 | job 7769 ran as `agent:ops-assistant`, output echoed the exact frozen parameters; job shows "Requested by AI agent … Approved by reviewer" |
| Reviewer approves a secret request (a second one, after the node was free) | job 7771 reported `{"length":17}`: the sealed value reached the task; `hunter2` absent from job JSON and console log |
| Two approvals back to back on one node | second job failed with the node agent's existing "busy" refusal, not a console fault |
| Reviewer rejects 77 | agent's `get_request` shows rejected with the note |
| Token's issuer (admin) approves | refused (`403`); a reviewer with no `orchestrator:run` over the node is refused |
| Token revoked with a request pending | `401` at `/mcp` at once; request is kept and **failed** ("the agent token was revoked"); approving it answers `409` |
| Request past its time (`expires_at` moved back) | agent sees `expired`; approving answers `409` |
| Claude Code approves-then-reads: request 111 approved by the reviewer | `get_request` and `get_job` through Claude Code: "approved by reviewer, job 7773 succeeded" |

Screenshots: `screenshots/approvals-review.png`, `agent-tokens.png`,
`job-from-agent.png`.

Found and fixed during the check: revoking a token **deleted** its requests (the
first schema cascaded). Requests are now kept as records, with their token
cleared, and the ones still pending are failed.
