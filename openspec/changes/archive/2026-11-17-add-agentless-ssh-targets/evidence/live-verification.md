# Live verification

Dev stack: console (mode `all`) built from this change with a secrets key,
`openvoxserver` serving the `task_fixture` module, `task-e2e-agent` (a node
with an agent), and `agentless-sshd`, an Alpine container running OpenSSH
(`ops` with a password and an authorized key, `deploy` with a password,
`ops` allowed `sudo` without a password). Its `sshd` was configured with a
`ForceCommand` that records every command it is asked to run, to prove what
crossed the wire. Accounts, targets and the container were throwaway and were
removed afterwards. Screenshots are in `screenshots/`.

The bootstrap admin role predates `agentless:manage`, so the permission was
granted to it on the Roles page first, as the access guide says to do after an
upgrade.

Script output (`live.py`):

```
== A. name clash and registration
a name that is already a node (openvoxserver)                  409 {"error": "a node with that name already exists", "field": "name"}
an unreadable private key                                      400 {"error": "the private key could not be read (is it a PEM or OpenSSH private key, and is the passphrase right?)", "field": "privateKey"}
shared credential ops-shared                                   201 {"id": 5, "name": "ops-shared", "username": "deploy", "hasPassword": true, "hasKey": false}
target agentless-pw (own password)                             201 {"id": 10, "name": "agentless-pw", "address": "127.0.0.1", "port": 2222, "username": "ops", "hasPassword": true, "hasKey": false, "hostKey": {}}
target agentless-key (private key, runs as root)               201 {"id": 11, "name": "agentless-key", "username": "ops", "runAs": "root", "hasKey": true, "hasPassword": false, "keyFingerprint": "SHA256:pv1YIdML6J5SxBV7IqNgi8vsIpA4ZZWNZdXtz5J7HgQ"}
target agentless-shared (shared credential)                    201 {"id": 12, "name": "agentless-shared", "credentialName": "ops-shared", "username": "deploy", "hasPassword": true}
list shows no secrets                                          200 {"total": 3}
== B. a task before the host key is trusted
job failed | host key: the host key of this target has not been trusted: check it and accept its fingerprint first
login attempts seen by sshd before/after: 47 47
== C. check: the key is shown, not trusted, no login sent
check 200 {'ok': False, 'stage': 'host key', 'reason': 'host key: the host key has not been trusted yet', 'presented': 'SHA256:2QyecialTmwLQTqtIjo/synb8POustm8QVyOsq5nksQ', 'newKey': True}
login attempts after the check: 47
the host's own fingerprints:
256 SHA256:2QyecialTmwLQTqtIjo/synb8POustm8QVyOsq5nksQ root@e06cbeab2507 (ECDSA)
256 SHA256:wPq0EYbGn+ICFhEEJI6hub5Vh+xG0NCgV3/9dnPvmhY root@e06cbeab2507 (ED25519)
3072 SHA256:0vCGk7iEMfqyzUKIZQV+WG+W97RcqUd0exlbLqWKP3U root@e06cbeab2507 (RSA)
the console's presented fingerprint is one of them: True
accept a fingerprint that was not shown                        409 {"error": "the key presented by this target is not the one you were shown", "field": "fingerprint"}
accept the fingerprint shown                                   200 {"name": "agentless-pw", "hostKey": {"pinned": "SHA256:2QyecialTmwLQTqtIjo/synb8POustm8QVyOsq5nksQ"}}
check agentless-pw       ok=True loggedInAs=ops runAs=None 
check agentless-key      ok=True loggedInAs=ops runAs=root 
check agentless-shared   ok=True loggedInAs=deploy runAs=None 
== D. tasks
echo job succeeded
   agentless-pw succeeded exit 0 | {"received":{"_task":"task_fixture::echo","count":2,"message":"hi from the console","mode":"plain"},"task":"un | result keys: ['received', 'task']
   agentless-key succeeded exit 0 | {"received":{"_task":"task_fixture::echo","count":2,"message":"hi from the console","mode":"plain"},"task":"un | result keys: ['received', 'task']
   agentless-shared succeeded exit 0 | {"received":{"_task":"task_fixture::echo","count":2,"message":"hi from the console","mode":"plain"},"task":"un | result keys: ['received', 'task']
multi (a helper file shipped with the module) succeeded | {"greeting":"hello Ada"} 
fail task failed exit 1 | {"_error":{"kind":"task_fixture/failed","msg":"the fixture failed on purpose","details":{}
a ruby task whose interpreter is not on the host (the fixture declares no requirement): failed | task exited with code 127
/tmp/ovc-task.XXXXoOpdAf/task_fixture/tasks/ruby.rb: exec: line 0: /tmp/ovc-task.XXXXoOpdAf/task_fixture/tasks/ruby.rb: not 
run-as root: id seen by the task's wrapper is not printed; sudo proves it: succeeded
== E. sensitive parameter: not on any command line, not stored
secret task succeeded | {"length":18} 
the poller read 26342 process command lines; it saw the running task's own command line 429 times; it saw the secret on any: False
sshd was asked to run 51 commands over the whole session (recorded by ForceCommand); the secret appears in any of them: False
  e.g. cd '/tmp/ovc-task.XXXXbnJeld' && sh -c 'eval "$(dd bs=1 count=108 2>/dev/null)" && exec "$0"' '/tmp/ovc-task.XXXXbnJeld/task_fixture/tasks/multi.sh'
  e.g. mktemp -d "${TMPDIR:-/tmp}/ovc-task.XXXXXXXXXX"
stored job params contain the secret: False
== F. timeout and cleanup
sleep 40 with a 3 s timeout: failed | task timed out after 3s and was stopped
task directories left on the host: 0
== G. one task at a time per target
second while the first runs: failed | this target is already running a task
the first finished unharmed: succeeded
== H. a changed host key
check: {'ok': False, 'stage': 'host key', 'reason': 'host key: the host key is not the pinned one; the target was not logged in to', 'presented': 'SHA256:SGHlyjaCrDL3cNvAYKr31qZ1KTjzG8W8SoCFaYGIu/g'}
task: failed | host key: the host key is not the pinned one; the target was not logged in to
login attempts at sshd during both: 0
target hostKey: {'pinned': 'SHA256:2QyecialTmwLQTqtIjo/synb8POustm8QVyOsq5nksQ', 'proposed': 'SHA256:SGHlyjaCrDL3cNvAYKr31qZ1KTjzG8W8SoCFaYGIu/g', 'changed': True, 'presented': 'SHA256:SGHlyjaCrDL3cNvAYKr31qZ1KTjzG8W8SoCFaYGIu/g'}
accept (must be explicit replace)                              400 {"error": "this target already has a pinned host key; replace it explicitly", "field": "fingerprint"}
replace with the fingerprint shown                             200 {"name": "agentless-pw", "hostKey": {"pinned": "SHA256:SGHlyjaCrDL3cNvAYKr31qZ1KTjzG8W8SoCFaYGIu/g"}}
check after replacing: True ops
== I. a wrong password
change the password to a wrong one                             200 {"name": "agentless-pw", "hasPassword": true}
check: {'ok': False, 'stage': 'authenticate', 'reason': 'authentication failed: the target refused the login'} | password in the answer: False
restored: True
== J. a mixed job with a node that has an agent
job succeeded
   agentless-pw succeeded | {"received":{"_task":"task_fixture::echo","count":1,"message":"mixed","mode":"pl
   task-e2e-agent.example.com succeeded | {"received":{"_task":"task_fixture::echo","count":1,"message":"mixed","mode":"pl
== K. what needs the agent is refused
Puppet run on an agentless target                              400 {"agentless": ["agentless-pw"], "error": "agentless target can only run tasks, not a Puppet run: agentless-pw"}
patch run                                                      400 {"agentless": ["agentless-pw"], "error": "agentless target can only run tasks, not a patch run: agentless-pw"}
a plan with a run step                                         400 {"agentless": ["agentless-pw"], "error": "step 2 (run): agentless target can only run tasks, not a run step: agentless-pw", "step": 2}
a plan of tasks on an agentless target: succeeded
== L. node scope
scoped user lists: ['agentless-pw']
scoped user reads another target                               404 {"error": "not found"}
scoped user reads one that does not exist                      404 {"error": "not found"}
scoped user: task on another target                            403 {"error": "this node is not in the node groups your role can act on: agentless-key", "outOfScope": ["agentless-key"]}
scoped user: add a target outside their groups                 403 {"error": "this node is not in the node groups your role can act on: sneaky", "outOfScope": ["sneaky"]}
scoped user: shared credentials                                403 {"error": "shared credentials can only be managed or used by a caller whose agentless:manage covers every node"}
scoped user: task on their own target: succeeded
cleanup: [204, 204, 204]
== M. secrets: where they are not
  the console log                                secrets found: none
  job rows (params, output, errors, results)     secrets found: none
  every API response seen here                   secrets found: none
  audit entries for agentless actions: 48, e.g.
    {"time":"2026-11-17T20:36:57.442779726-05:00","level":"INFO","msg":"audit","action":"agentless.credential.created","actor":"admin","category":"nodes","resourceType":"agentless-credential","resourceId":"ops-shar
    {"time":"2026-11-17T20:36:57.463832109-05:00","level":"INFO","msg":"audit","action":"agentless.target.created","actor":"admin","category":"nodes","resourceType":"agentless-target","resourceId":"agentless-pw","a
    {"time":"2026-11-17T20:36:57.484986952-05:00","level":"INFO","msg":"audit","action":"agentless.target.created","actor":"admin","category":"nodes","resourceType":"agentless-target","resourceId":"agentless-key","
sealed columns (name | bytes | first bytes | key set):
agentless-key|||t
agentless-pw|41|017619d7|f
agentless-shared|||f

```

## Two bugs this run found, now fixed and tested

- **The name-clash check treated "openvoxdb has never heard of that name"
  as a failure** (a 502 on every new name), because the real client answers
  `ErrNodeNotFound` and the check expected `nil`. The check is now
  `agentless.NodeKnown`, tested against a fake that answers as the real one does.
- **Updating a target without a port reset it to 22 and cleared its host key
  pin**, which looked like "connection refused" after a password change. A port
  left out now keeps its value.
- **A task taking its input on stdin, run under run-as, failed with
  `sudo: exec: command not found`** (sudo runs programs, not the shell builtin
  `exec`). The unit tests' fake `sudo` was more forgiving than the real one;
  it now refuses builtins the same way, and a test covers the case.

## Not done live

Windows, jump hosts and certificates are out of scope. A password-protected
private key was exercised only in unit tests (the live key had no passphrase).
