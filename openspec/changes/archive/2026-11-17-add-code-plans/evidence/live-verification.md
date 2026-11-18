# Live verification

Dev stack: console (mode `all`) built from this change, `openvoxserver`,
`task-e2e-agent` (a node with an agent), and `agentless-sshd` (an Alpine
container, registered as the agentless target `plan-ssh`). The plans are real
YAML files placed in the deployed environment's `task_fixture` module
(`site/task_fixture/plans/`), read by the console through the
`environments/production` symlink the code manager leaves. `sshd` was set up
to record every command it is asked to run. Users, roles, groups, the target and
the container were throwaway and removed afterwards. Screenshots are in
`screenshots/`.

Script output (`live.py`):

```
== A. a plan from the deployed code, on an agent node and an agentless target
register the agentless target 201
check: True ops
start 200 {'id': 7508, 'kind': 'codeplan', 'planName': 'task_fixture::demo', 'environment': 'production', 'status': 'running'}
job succeeded | error: -
   1     succeeded task_fixture::echo         job #7509 
   2     succeeded task_fixture::echo         job #7510 
   3     succeeded message                    echoed on ["task-e2e-agent.example.com","plan-ssh"]; token is 
returned: {"ok": true, "nodes": ["task-e2e-agent.example.com", "plan-ssh"], "first_said": "hi there"}
step 2's job: parent=7508 stepOrder=2 targets=['task-e2e-agent.example.com', 'plan-ssh']
job list shows the plan, not its steps: [(7508, 'codeplan'), (6809, 'plan'), (6808, 'task')]
== B. parameters are checked before anything starts
missing required parameter                                     400 {"error": "parameter targets is required", "field": "targets"}
wrong type                                                     400 {"error": "parameter repeat is not a Integer[1, 10]: expected Integer[1, 10]", "field": "repeat"}
out of range                                                   400 {"error": "parameter repeat is not a Integer[1, 10]: expected Integer[1, 10], got 99", "field": "repeat"}
an unknown parameter                                           400 {"error": "parameter nope is not a parameter of this plan", "field": "nope"}
an unusable plan (command step)                                400 {"error": "plan task_fixture::bad cannot be run: step 1 (shell): \"command\": commands are not supported: run a task instead"}
a plan that does not exist                                     400 {"error": "plan task_fixture::nothere was not found in environment production"}
== C. a failing step ends the plan; later steps are not run
job failed | step 2 (boom) failed on task-e2e-agent.example.com
   1     succeeded task_fixture::echo         job #7512 
   2     failed    task_fixture::fail         job #7513 failed on task-e2e-agent.example.com
   3     not_run   message                    
== D. a plan calling a plan
job succeeded
   1     succeeded task_fixture::demo         
   1.1     succeeded task_fixture::echo         job #7515 
   1.2     succeeded task_fixture::echo         job #7516 
   1.3     succeeded message                    echoed on ["task-e2e-agent.example.com"]; token is 
   2     succeeded message                    the inner plan returned true
returned: {"ok": true, "nodes": ["task-e2e-agent.example.com"], "first_said": "from the outer plan"}
== E. a sensitive parameter
job succeeded | -
   1     succeeded task_fixture::secret       job #7518 
   2     succeeded message                    used [redacted]
returned: {"ok": true, "token": "[redacted]"} | stored parameters: {"sha256": "da9dd7258c9a6c1f084de040f7bb298d169d5002e3b61c61218aef6868526a1d", "parameters": {"token": "[redacted]", "targets": ["plan-ssh"]}}
sshd was asked to run 16 commands in this session; the secret is in any of them: False
== F. a node group as the target, resolved once
job succeeded | nodes: ['task-e2e-agent.example.com']
== G. node scope and permissions, per step
scoped user: name a node outside their groups                  403 {"error": "this node is not in the node groups your role can act on: openvoxserver", "outOfScope": ["openvoxserver"]}
scoped user: a plan whose task they may not run                403 {"error": "step 2 (task_fixture::fail): you are not allowed to run the task \"task_fixture::fail\"", "step": 2, "task": "task_fixture::fail"}
scoped user: step 2 names a node they may not act on -> failed | step 2 (other): this node is not in the node groups the caller can act on: openvoxserver
   1     succeeded task_fixture::echo         job #7523 
   2     failed    task_fixture::echo         this node is not in the node groups the caller can act on: openvoxserver
scoped user: their own group: succeeded
scoped user reads the demo run (it also ran on a node outside their group... both nodes are in their group): 200 traceHidden=None
cleanup: [204, 204]
== H. a long run is kept alive past the old reaper window
started plan job 7527, then backdated its start by 5 hours (the old rule fails a job two hours after it started)
after 75s: status=succeeded, distinct heartbeats seen=16, started_at shows 2026-11-17T16:09:44
== I. nothing secret in the records
  the console log                                sentinel found: False
  job rows (params, trace, result, error, output) sentinel found: False
  API responses seen here                        sentinel found: False
  audit entries for code plans: 16, for example:
    codeplan.triggered admin {"environment": "production", "parameters": ["greeting", "targets"], "plan": "task_fixture::demo", "sha256": "b20f5c28833e2114441969355c974dabbb1b191d0daa6cb26d6ce18b3784
    codeplan.succeeded admin null
    codeplan.triggered admin {"environment": "production", "parameters": ["targets"], "plan": "task_fixture::failing", "sha256": "25daef7da4e90fcaf1fefdf85882f42c1fabc7ff5999d29fad93caacf7d38a4c"}
```

## A scoped reader of a plan that also ran on nodes they cannot see

The demo run of section A ran on the e2e node and on `plan-ssh`. A reader
whose role covers only the e2e node:

```
administrator reads plan job 7508: message='echoed on ["task-e2e-agent.example.com","plan-ssh"]; token is ' result=yes params=yes
reader limited to the e2e node:  status 200 traceHidden=True targetsHidden=True message=None result=no params=no steps=[('1', 'succeeded'), ('2', 'succeeded'), ('3', 'succeeded')]
cleanup: [204, 204, 204]
```

## A long run, past the old reaper window

A plan that sleeps 420 seconds was started and its `started_at` was then moved
back five hours (the previous rule fails a job two hours after it started). The
reaper ticks every five minutes and ran at least once while it was going.

```
plan job 7529 started; its start was then backdated by 5 hours (the old rule fails a job two hours after it started). now=21:11:09
ended after 425s at 21:18:15: status=succeeded, distinct heartbeats=86, started_at shown=2026-11-17T16:11:09
reaper ticks while it ran (console log):  jobs failed by the reaper since start
```

Section H of the first script did the same with a 70 second run (it ended
before a reaper tick, so it only shows the heartbeat advancing).

## Not done live

A second console instance without the code directory (the "not available"
answer) is covered by tests (`TestAPI_NoCodeOnThisInstanceIsNotNoPlans`,
`TestFiles_Unavailable`). Scheduling a code plan is not part of this change.
