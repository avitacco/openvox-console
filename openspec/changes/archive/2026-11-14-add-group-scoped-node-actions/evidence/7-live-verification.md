# Live verification (tasks 7.2 - 7.5)

Run on 2026-11-14 against the local dev stack, with the console built
from this change and certificate management configured.

Nodes used: `task-e2e-agent.example.com` (in **Debian Family**) and
`vuln-deb1.test` (a signed certificate with no facts, so in no group
but All Nodes). Neither was connected, so a run that is *accepted*
creates a job whose target then fails "agent not connected"; what is
being checked is whether the job is created at all.

Users, each with roles created for the test:

- `live-act`: `nodes:read`, `classifier:read`, `orchestrator:read` on
  **All Nodes**; `orchestrator:run`, `nodes:manage`,
  `nodes:certs:manage` on **Debian Family**. Reads everything, acts on
  one group.
- `live-narrow`: `nodes:read`, `orchestrator:read` on **Debian Family**.

## 7.2 Runs, as `live-act`

| Request | Result |
| --- | --- |
| run on `task-e2e-agent.example.com` | 200, job 1633 created |
| run on `vuln-deb1.test` | 403, `outOfScope: [vuln-deb1.test]` |
| run on `task-e2e-agent…`, `vuln-deb1.test`, `pkgfix-deb.test` | 403, `outOfScope: [vuln-deb1.test, pkgfix-deb.test]` |

Jobs naming `vuln-deb1.test`: 1 before, 1 after. Jobs naming
`task-e2e-agent.example.com`: 18 before, 19 after - only the accepted
run; the mixed request created nothing for the node that was in scope.

## 7.3 Other actions on the out-of-scope node, as `live-act`

All answered 403 naming `vuln-deb1.test`:

- `DELETE /nodes/vuln-deb1.test`
- `POST /nodes/vuln-deb1.test/agent-upgrade`
- `PUT /nodes/vuln-deb1.test/packages/reporting`
- `POST /nodes/vuln-deb1.test/cert/revoke`
- `DELETE /nodes/vuln-deb1.test/cert`

`POST /nodes/no-such-node.test/cert/revoke` answered the same 403,
differing only in the name. Afterwards the certificate of
`vuln-deb1.test` was still **signed** and the node still listed.

In the browser, **Run now** on the page of `vuln-deb1.test` showed:
"Nothing was done. Your role cannot act on this node, because it is not
in your node groups: vuln-deb1.test"
(`screenshots/node-page-run-refused.png`).

## 7.4 Jobs, as `live-narrow`

- Job list total: **21**, against 1635 for the administrator. Every
  listed job names only `task-e2e-agent.example.com`.
- Job 1634, which ran only on `vuln-deb1.test`: 404 "job not found",
  identical to the answer for a job id that does not exist.
- Job 1635, run by the administrator on both nodes: one target shown,
  `targetsHidden: true`. In the browser the job page lists the one node
  under the note "Only the nodes in your node groups are shown. This
  job also ran on other nodes, and its status covers all of them."
  (`screenshots/job-page-nodes-hidden.png`)

## 7.5 A fleet-wide user is unchanged

As the bootstrap administrator, using requests that change nothing:

| Request | Result |
| --- | --- |
| run on `vuln-deb1.test` | 200, job 1634 created |
| `POST /nodes/vuln-deb1.test/agent-upgrade` | 409 "the node is not connected" |
| `DELETE /nodes/no-such-node.test` | 404 "node not found" |
| `POST /nodes/no-such-node.test/cert/revoke` | 404 from the CA |

Each reached the layer behind the scope check - the dispatcher, the
upgrade coordinator, openvoxdb and the CA - which the scoped user's
requests did not.

## Set-up note

The dev stack's certificate-admin credential (`certs/ca-client-*.pem`)
had been issued by a CA that was since regenerated, so the CA answered
403 and certificate management was inactive. It was reissued by the
procedure in the containers install guide (openvoxserver stopped,
`puppetserver ca generate --ca-client`, restarted) and the CA now
answers 200. The previous files are kept in `certs/stale-2026-11-14/`.

## Clean-up

The two users and three roles were deleted. Jobs 1633-1635 remain in
the dev database as ordinary job history.
