# Live verification on the dev stack (2026-11-14)

Console built from this change, run on the host against the compose
stack (`CONSOLE_PUPPET_SERVER_URL=https://localhost:8140`), with the
`task-e2e-agent.example.com` container as the node. Pages were driven in
headless Chromium; screenshots are in `screenshots/`.

## Server access (1.1, 1.2)

| Request | Certificate | Before the rule | After the rule |
|---|---|---|---|
| `GET /puppet/v3/environment_classes?environment=production` | console | 403 | 200 |
| same | console-ca-client | - | 403 |
| `...?environment=nosuch` | console | - | 404 `Could not find environment 'nosuch'` |

The rule is added by `openvoxserver-init/20-allow-class-discovery.sh`
on container start. The recorded 200 response is
`internal/classcatalog/testdata/api/environment_classes_production.json`.
The server sent no `ETag` (its class cache is off).

Found on the way: the image sets `environment_timeout = unlimited`, so a
module copied into the environment did not appear until
`DELETE /puppet-admin-api/v1/environment-cache`.

## Class discovery (8.2)

- `GET /api/v1/classes/environments` → `["production"]`;
  `GET /api/v1/classes?environment=production` → 7 classes, sorted,
  `class_fixture` with its 13 parameters, and `broken.pp` listed under
  `errors` by its path inside the environment. `?environment=nosuch` → 404.
- With `broken.pp` present no group was marked (parse errors suppress
  "not defined"). With it removed, the seeded groups naming
  `profile::*` classes, which the dev code does not define, were each
  marked `class-missing`, and the groups list showed **Class mismatch**
  on them (`groups-list.png`).
- Group page, in the browser (`classes-editor.png`): added
  `class_fixture` from the list; its 13 parameters were offered with
  their types; added seven. Controls by type: `owner` text, `port`
  number (default 8080 shown), `manage_service` choice true/false,
  `ensure` choice absent/present, `ratio` number, `servers` JSON,
  undeclared `colour` JSON and marked "Not a parameter of this class".
  Added `gone` (marked "Not defined in production") and
  `class_fixture::worker`. A duplicate class was refused. JSON view and
  back kept all three classes.
- Saved. Stored: `port` 8443 as a number, `manage_service` false as a
  Boolean, `servers` as a list. The save succeeded with the marks, and
  stayed on the page showing them.
- ENC answer for the pinned node included `class_fixture` with those
  values, `gone` and `class_fixture::worker`: marked classes are still
  sent.
- Removed `worker.pp` from the environment and flushed the server's
  cache. Before **Refresh**: no new mark (the console's five-minute
  copy). After `POST /api/v1/classes/refresh`: `class_fixture::worker`
  marked `class-missing` (`classes-flagged.png`); the group's stored
  classes were byte-identical to before; the ENC answer still included
  the class.
- Unavailable, **refused**: console started with a certificate the rule
  does not allow. `GET /api/v1/classes` → 503 `reason: forbidden`; group
  → `classesChecked: false`, no marks; the page showed the
  auth.conf notice, took a typed class name, and saved it
  (`classes-unavailable-forbidden.png`).
- Unavailable, **not configured**: console started without
  `CONSOLE_PUPPET_SERVER_URL`. 503 `reason: not_configured`; notice
  naming the setting (`classes-unavailable-not-configured.png`); the ENC
  answer was unchanged.

Not done as the task worded it:

- "Remove the `auth.conf` rule" was done by presenting a certificate the
  rule does not name, which gets the same 403 from the same server. The
  rule's absence giving 403 is the "before" column above.
- "Deploy" was a manifest removed by hand plus a cache flush: this dev
  setup has no code source configured, so the console cannot deploy. A
  deploy expiring the cache is covered by
  `TestDeployExpiresClassCatalog`, over a real NATS bus.

## Run options (8.3)

| Job | Client | Options | Result |
|---|---|---|---|
| 1957 | old (Oct 6 build) | no-op | target failed: "this node's agent client is too old for run options ... Puppet was not run." |
| 1958 | old | none | succeeded, exit 0, no output - unchanged behaviour |
| 1959 | new | no-op | succeeded; output lists `Notify[class_fixture for ops] ... (noop)` and "Would have triggered 'refresh'" |
| 1960 | new | debug, environment `feature_x` | succeeded; 114 KB output, 941 `Debug:` lines, no colour codes; report environment `production` → marked "Ran in production, not feature_x" |
| 1961 | new | environment `feature_x`, node's group set to `feature_x` | report environment `feature_x` → no mark |
| 1963 | new | no-op, environment `feature_x`, node's groups setting **no** environment | output "Using environment 'feature_x'"; report environment `feature_x` → no mark |
| 1962 | new | no-op, environment `anything_goes`, console with no server connection | accepted: the name is not checked when the list cannot be read |

- The old client was sent only `run_with_options` for job 1957 and
  answered `unsupported action`. The node's last-run file was next
  written by job 1958.
- Refused with 400 and no job: environment `nosuch` ("not found on the
  Puppet server"), `bad-name` (invalid name), option `trace` (unknown).
- Job 1959 used the class picked in the browser above, so the chain
  group page → ENC → catalog → no-op output was exercised end to end.
- Jobs list shows the options per job; the job page shows them, the
  output under each node (capped at 32rem with a scrollbar after the
  first screenshot showed a 23,000 px page), and the mark
  (`job-noop.png`, `job-debug-environment-mismatch.png`).
- Run dialog (`run-dialog.png`): names the node, offers both
  environments, carries the environment note.
- For jobs 1963 the environment was cleared on the three seeded groups
  the node matches and restored afterwards; each was checked to have
  the same classes, rule and pins before and after.

## Left in the dev stack

- `openvox-code/environments/production/site/class_fixture` (two
  classes, both parse), beside `task_fixture`.
- Jobs 1957-1963 as ordinary job history.
- The `auth.conf` rule on the dev openvoxserver.

Removed: the test group, the `feature_x` environment, the temporary
`profile` module, and the client binary copied into the node container,
which is stopped as it was found.
