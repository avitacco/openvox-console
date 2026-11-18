# Live verification

Dev stack: console (mode `all`) built from this change, `openvoxserver`
(no node agent), and `task-e2e-agent`, an Ubuntu 26.04 container running a
node agent built from this change. Users, roles and the group below were
throwaway and deleted afterwards. Screenshots are in `screenshots/`.

## An agent that predates patching

With the previous agent still on the node, a security dry run answered:

```
failed [('task-e2e-agent.example.com', 1, 'failed', "this node's agent client is too old to patch; upgrade it and run again. Nothing was installed.")]
```

## Dry run (new agent)

```
pending on the node before: 39
start 200 6548 {'scope': 'security', 'dryRun': True, 'batchSize': 10, 'stopAfterFailures': 3}
succeeded [('task-e2e-agent.example.com', 1, 'succeeded', '')]
manager apt dryRun True packages 33 {'would_upgrade'}
sample [{'to': '2.43-2ubuntu2.7', 'from': '2.43-2ubuntu2', 'name': 'libc-gconv-modules-extra', 'outcome': 'would_upgrade', 'security': True}, {'to': '2.43-2ubuntu2.7', 'from': '2.43-2ubuntu2', 'name': 'libc6', 'outcome': 'would_upgrade', 'security': True}]
pending on the node after: 39
snapshot {'certname': 'task-e2e-agent.example.com', 'manager': 'apt', 'rebootRequired': False, 'updates': 39, 'security': 33, 'scannedAt': '2026-11-17T19:54:36.381874-05:00', 'attemptedAt': '2026-11-17T19:54:36.381874-05:00'}
```

## A real run of named packages

`vim` has nothing pending; the others are real security updates.

```
installed before:
ca-certificates=20260601~26.04.1
diffutils=1:3.12-1
tar=1.35+dfsg-4ubuntu0.2
pending before: 39
start 200 6549 {'scope': 'packages', 'packages': ['ca-certificates', 'diffutils', 'tar', 'vim'], 'batchSize': 5, 'stopAfterFailures': 3}
heartbeats seen while running: [(0, 'running', '2026-11-17T19:54:51.306574-05:00'), (4, 'running', '2026-11-17T19:54:51.306574-05:00'), (8, 'running', '2026-11-17T19:54:51.306574-05:00'), (12, 'running', '2026-11-17T19:54:51.306574-05:00'), (16, 'succeeded', '2026-11-17T19:54:51.306574-05:00')]
job succeeded finished 2026-11-17T19:55:06.333922-05:00 [('task-e2e-agent.example.com', 1, 'succeeded', '')]
exit code 0 output tail: etc/ssl/certs... | rehash: warning: skipping ca-certificates.crt, it does not contain exactly one certificate or CRL | 4 added, 5 removed; done. | Running hooks in /etc/ca-certificates/update.d... | done.
   {'name': 'vim', 'outcome': 'nothing_to_apply'}
   {'to': '20260816ubuntu0.26.04.2', 'from': '20260601~26.04.1', 'name': 'ca-certificates', 'outcome': 'upgraded', 'security': True}
   {'to': '1:3.12-1ubuntu0.1', 'from': '1:3.12-1', 'name': 'diffutils', 'outcome': 'upgraded', 'security': True}
   {'to': '1.35+dfsg-4ubuntu0.4', 'from': '1.35+dfsg-4ubuntu0.2', 'name': 'tar', 'outcome': 'upgraded', 'security': True}
installed after:
ca-certificates=20260816ubuntu0.26.04.2
diffutils=1:3.12-1ubuntu0.1
tar=1.35+dfsg-4ubuntu0.4
pending after: 36
snapshot {'updates': 36, 'security': 30, 'rebootRequired': False} names left include diffutils: False
```

## A real security-only run, followed by heartbeat

Each distinct heartbeat seen while the run was going (the console asks every 15 seconds):

```
pending before: 36 security: 30
start 200 6550
distinct heartbeats while running:
   (0, 'pending', None, '2026-11-17T19:55:19.500638-05:00')
   (3, 'running', '2026-11-17T19:55:19.518501-05:00', '2026-11-17T19:55:19.519934-05:00')
   (15, 'running', '2026-11-17T19:55:34.522215-05:00', '2026-11-17T19:55:34.537647-05:00')
   (30, 'succeeded', '2026-11-17T19:55:34.522215-05:00', '2026-11-17T19:55:49.5427-05:00')
job succeeded [('task-e2e-agent.example.com', 1, 'succeeded', '')]
outcomes Counter({'upgraded': 30}) rebootRequired False exit 0
pending after: 6
snapshot {'updates': 6, 'security': 0, 'rebootRequired': False} remaining: ['base-files', 'libaudit-common', 'libaudit1', 'libsystemd0', 'libudev1', 'openvox-agent']
```

## Stop threshold, one run per node, refusals, scope, audit

```
== A. the stop threshold (batch 1, stop after 1 failed node)
failed [('openvoxserver', 1, 'failed', 'agent not connected'), ('task-e2e-agent.example.com', 2, 'failed', 'not started: the run stopped after 1 failed node(s)')]
== B. the same with a threshold of 0 (never stops)
failed [('openvoxserver', 1, 'failed', 'agent not connected'), ('task-e2e-agent.example.com', 2, 'succeeded', '')]
== C. one run per node
first run on the e2e node                                        200 {"id": 6557, "status": "running"}
second run on the same node while the first is open              409 {"error": "already being patched by another run: task-e2e-agent.example.com", "nodes": ["task-e2e-agent.example.com"]}
== D. refused requests
batch size 0                                                     400 {"error": "the batch size must be between 1 and 100", "field": "batchSize"}
unknown scope                                                    400 {"error": "scope must be \"security\", \"all\" or \"packages\"", "field": "scope"}
packages without the packages scope                              400 {"error": "packages are only used when the scope is packages", "field": "packages"}
an option-like package name                                      400 {"error": "\"--allow-downgrades\" is not a valid package name", "field": "packages"}
dry run with reboot                                              400 {"error": "a dry run never reboots", "field": "reboot"}
an unknown option                                                400 {"error": "invalid patch options: json: unknown field \"force\""}
== E. node scope and permissions
web operator: patch the db node                                  403 {"error": "this node is not in the node groups your role can act on: openvoxserver", "outOfScope": ["openvoxserver"]}
web operator: patch both nodes (refused whole)                   403 {"error": "this node is not in the node groups your role can act on: openvoxserver", "outOfScope": ["openvoxserver"]}
read-only user: patch                                            403 {"error": "missing required permission: orchestrator:run"}
web operator: patch their own node                               200 {"id": 6558, "status": "running", "triggeredBy": "p11-webop"}
== F. reading the run, scoped
web operator reads the 2-node run: targets=['task-e2e-agent.example.com'] targetsHidden=True
read-only user (no node groups) reads it: 404
job list kind=patch: [(6558, 'patch', 'succeeded'), (6557, 'patch', 'succeeded'), (6556, 'patch', 'failed')]
== G. audit
  patch.triggered admin 6555 {"batchSize": 1, "dryRun": true, "nodes": 2, "packages": null, "reboot": false, "scope": "all", "source": null, "stopAfterFailures": 1}
  patch.failed admin 6555 null
  patch.triggered admin 6556 {"batchSize": 1, "dryRun": true, "nodes": 2, "packages": null, "reboot": false, "scope": "all", "source": null, "stopAfterFailures": 0}
  patch.failed admin 6556 null
  patch.triggered admin 6557 {"batchSize": 10, "dryRun": true, "nodes": 1, "packages": null, "reboot": false, "scope": "all", "source": null, "stopAfterFailures": 3}
  patch.succeeded admin 6557 null
  patch.triggered p11-webop 6558 {"batchSize": 10, "dryRun": true, "nodes": 1, "packages": null, "reboot": false, "scope": "security", "source": null, "stopAfterFailures": 3}
  patch.succeeded p11-webop 6558 null
IDS {"gw": 826, "rw": 845, "rr": 846, "p11-webop": 1183, "p11-reader": 1184, "stop": 6555}
```

## The reaper

Four rows were inserted by hand to show the two rules (these are not real runs): a patch job started 5 hours ago that reported a minute ago, a patch job silent for 11 minutes, a task started 121 minutes ago whose heartbeat column is fresh, and a Puppet run 30 minutes old. After one reaper tick:

```
--- before
  id  | kind  | status  |   certname    | age_min | silent_min 
------+-------+---------+---------------+---------+------------
 6551 | patch | running | reaper-demo-a |     300 |          1
 6552 | patch | running | reaper-demo-b |      30 |         11
 6553 | task  | running | reaper-demo-c |     121 |          0
 6554 | run   | running | reaper-demo-d |      30 |           
(4 rows)

--- after
  id  | kind  | status  | target  |                                   reason                                    
------+-------+---------+---------+-----------------------------------------------------------------------------
 6551 | patch | running | running | 
 6552 | patch | failed  | failed  | progress stopped being reported: the console instance following this patch 
 6553 | task  | failed  | failed  | could not be tracked to completion: the console instance that dispatched th
 6554 | run   | running | running | 
(4 rows)

```

The long-running patch job survives; the silent one is failed for silence;
the task is failed by its age whatever its heartbeat says; the young run is
left alone.

## Not done live

A reboot was not exercised on the node: the container has no init system.
The rule (only when asked for and required, never for a dry run or a failed
node, delivered before it happens) and the rescan after a reboot are covered
by tests (`TestPatchApply_Reboot`, `TestPatchRun_AfterARebootTheNodeIsScannedAgain`,
`TestPatchRun_ANodeThatDoesNotReturnHasItsScanFail`).
