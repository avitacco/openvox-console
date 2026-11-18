# Live verification

Dev stack: console (mode `all`), openvoxserver, and `task-e2e-agent`, a
Ubuntu 26.04 container running a node agent built from this change.
Accounts below are throwaway users created for the run and deleted after it.

## What the node itself says

`apt list --upgradable` on the node lists 39 packages, and
`apt-get -s dist-upgrade` shows 39 `Inst` lines with an installed version, 33
of them with a `security` origin. `/var/run/reboot-required` does not exist.
The console's first scan of the same node reported 39 updates, 33 security,
no reboot: the counts agree.

## Run output

(`live.py`, scoped users `p10-webop` (a group pinning the node), `p10-dbop`
(a group pinning `openvoxserver`) and `p10-reader` (`nodes:read` only).)

```
== A. the real node, as the administrator
scan the e2e node                                                      200 {"scanned": 1, "failed": 0, "offline": 0, "results": [{"certname": "task-e2e-agent.example.com", "outcome": "scanned", "updates": 39, "security": 33}]}
its snapshot                                                           200 {"manager": "apt", "updates": 39, "security": 33, "rebootRequired": false}
search the fleet for pending openssl                                   200 {"items": [{"certname": "task-e2e-agent.example.com", "name": "openssl", "installed": "3.5.5-1ubuntu3.2", "available": "3.5.5-1ubuntu3.7", "security": true}, {"certname": "task-e2e-agent.example.com", "name": "openssl-provider-leg
== B. a node that is not connected
scan openvoxserver (no node agent)                                     200 {"scanned": 0, "failed": 0, "offline": 1}
its snapshot afterwards                                                200 scanned=False snapshot=None
== C. node scope
web operator: fleet view                                               200 nodes=['task-e2e-agent.example.com'] totals={"nodes": 1, "scanned": 1, "neverScanned": 0, "unsupported": 0, "withUpdates": 1, "withSecurity": 1, "rebootRequired": 0, "stale": 0}
db operator: fleet view                                                200 nodes=['openvoxserver'] totals={"nodes": 1, "scanned": 0, "neverScanned": 1, "unsupported": 0, "withUpdates": 0, "withSecurity": 0, "rebootRequired": 0, "stale": 0}
db operator: patches of the e2e node                                   404 {"error": "node not found"}
db operator: patches of a node that does not exist                     404 {"error": "node not found"}
db operator: search pending openssl                                    200 {"total": 0, "items": []}
web operator: search pending openssl                                   200 {"total": 2}
db operator: scan the e2e node                                         403 {"error": "this node is not in the node groups your role can act on: task-e2e-agent.example.com", "outOfScope": ["task-e2e-agent.example.com"]}
db operator: scan the web group                                        200 {"scanned": 0, "failed": 0, "offline": 0, "skipped": 1}
web operator: scan the web group                                       200 {"scanned": 1, "failed": 0, "offline": 0, "skipped": 0}
read-only user: scan                                                   403 {"error": "missing required permission: orchestrator:run"}
web operator: change the interval                                      403 {"error": "missing required permission: nodes:manage"}
== D. the interval
interval of 5 minutes                                                  400 {"error": "the scan interval must be between 15 minutes and 7 days", "field": "intervalMinutes"}
interval of 20000 minutes                                              400 {"error": "the scan interval must be between 15 minutes and 7 days", "field": "intervalMinutes"}
stored setting is unchanged                                            200 {"enabled": true, "intervalMinutes": 1440}
interval of 15 minutes                                                 200 {"enabled": true, "intervalMinutes": 15}
== E. the periodic pass
backdated attempt, interval 15 min                                     35s later: scannedAt 2026-11-17T17:15:19.189142-05:00 -> 2026-11-17T17:15:53.761624-05:00
patch.scan.requested audit entries                                     4 -> 4
switch it off                                                          200 {"enabled": false, "intervalMinutes": 15}
audit: scan requests=4, interval changes=2
cleaned up; interval back to {'enabled': True, 'intervalMinutes': 1440}
```

## Reading it

- **Real scan.** 39 pending updates, 33 flagged security, matching apt.
- **Offline.** `openvoxserver` has no node agent: the scan answers
  `offline` and records nothing. It stays "not scanned", not "clean".
- **Scope.** The web operator sees only the e2e node and its counts; the db
  operator sees only `openvoxserver` and gets the same `404` for the e2e node
  as for a node that does not exist, and an empty package search. Scanning
  the e2e node by name is a `403` naming it. Scanning the web group scans
  nothing and counts it as skipped.
- **Permissions.** A `nodes:read`-only user cannot scan; a user without
  `nodes:manage` cannot change the interval.
- **Interval.** 5 and 20000 minutes are refused with `400` and the field
  named, leaving the setting at 1440. 15 is accepted.
- **Periodic pass.** With the last attempt backdated two hours and a 15 minute
  interval, the node was scanned again by the next pass (35 s). The count of
  `patch.scan.requested` audit entries did not move: periodic scans write none.
  Switching the scan off was audited with the interval change.
- **Audit.** 4 scan requests (the administrator's two, and one group scan
  per operator) and the interval changes. Refused requests are not audited as
  scans.

## Screenshots

`screenshots/patching-fleet.png` (fleet view), `screenshots/node-patches.png`
(the node's Patches tab) and `screenshots/patching-interval-refused.png` (a
refused interval shown on its field).

The test node's agent was replaced with the new build for this run (the
previous binary is kept in the container as
`node-agent-client-before-patch`).
