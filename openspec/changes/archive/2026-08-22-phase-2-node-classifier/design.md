## Context

See `proposal.md` for motivation and the `classifier`/`enc-api` specs for
requirements. The ENC output format below was verified against Puppet's
own external node classifier documentation
(https://www.puppet.com/docs/puppet/7/nodes_external.html), not assumed:

```yaml
classes:
  common:
  ntp:
    ntpserver: 0.pool.ntp.org
parameters:
  mail_server: mail.example.com
environment: production
```

Classes must be hash-form (not a plain array) to support per-class
parameters; `node_terminus = exec` + `external_nodes = <path>` in
`puppet.conf` is how openvox-server is told to use an ENC at all; the
executable receives the certname as its only argument and must write this
YAML to stdout, exiting 0 on success.

## Goals / Non-Goals

**Goals:**
- Correct, verified compatibility with openvox-server's exec-based ENC
  mechanism - the one thing in this phase that must be externally right.
- A classification model that's simpler to reason about than Puppet
  Enterprise's group-inheritance-plus-rules-plus-pinning tree: flat groups,
  explicit priority, no implicit hierarchy.

**Non-Goals:**
- No group inheritance or nesting. A flat list with explicit priority
  replaces it entirely - see Decisions.
- No permission gating on group management yet. Phase 3 (RBAC) applies
  auth middleware retroactively to this phase's endpoints, same as it will
  for Phase 1's.
- No caching of classification results - always computed fresh from
  current group state, so an edited group takes effect on the next
  classification with no invalidation logic needed.
- No general rule/expression language. See the rule-grammar decision
  below.
- No OR logic within a single group's rule. Expressing "match A or B"
  means creating two groups; see Risks for why that's an acceptable
  trade-off, not a one-way door.

## Decisions

**Flat groups with explicit numeric priority, not hierarchy.** Puppet
Enterprise's parent/child group tree, with precedence implied by tree
depth, is exactly the model architecture-summary.md calls out as a known
source of confusion. Replacing it: every group is independent; a group's
`priority` integer alone decides which value wins when two matching groups
conflict. Alternative considered: keep hierarchy but simplify it -
rejected, because any hierarchy still requires understanding a tree to
predict an outcome, while "highest priority wins" requires understanding
one number.

**Group priorities must be unique.** Removes any tie-breaking ambiguity
(no "first created wins" or similar implicit rule to remember). Enforced
as a validation error at group create/update time. Alternative considered:
allow ties and break them by creation order - rejected as one more implicit
rule when the explicit-priority model's whole point is to avoid implicit
rules.

**Rule grammar: AND-only fact comparisons, hand-parsed - not a general
expression language.** A rule is zero or more conditions
(`fact.path <op> value`, `op` one of `=`, `!=`, `~` for regex, `>`/`<`/
`>=`/`<=` for numeric comparison), all of which must match; no OR, no
nesting. Matches the project's established "thin, not a general engine"
posture (see Phase 1's PQL client design) - most real classification needs
("all Debian nodes," "all nodes with role X") are single-condition or
small-AND. Multi-group OR-equivalent behavior (two groups both applying to
overlapping node sets) is a documented consequence, not a limitation
nobody can work around.

**A separate `enc-bridge` binary translates JSON to ENC YAML, using an
established YAML library.** `node_terminus = exec` requires a local
executable, not an HTTP client - openvox-server can't call our HTTP
endpoint directly. `cmd/enc-bridge` is a small second binary in this same
Go module: given a certname argument, it calls `GET /api/v1/enc/{certname}`
and marshals the JSON response to YAML with `gopkg.in/yaml.v3`, writing to
stdout. Alternative considered: a bash/curl script - rejected because
hand-formatting arbitrary parameter values (strings, numbers, nested
structures) as valid YAML without a real library is exactly the class of
"easy to get subtly wrong" problem this project already avoids elsewhere
(JWT parsing, Postgres migrations). A small compiled binary reusing the
existing build/Dockerfile tooling is cheap by comparison.

**Environment resolution: highest-priority matching group's environment
wins; omit the key if none set one.** Consistent with the class/parameter
merge rule - one explicit precedence rule for everything a group can
contribute. Puppet treats a missing `environment` key as "use the default,"
so omitting it is valid, not an error case to handle specially.

## Risks / Trade-offs

- **[Risk]** Requiring globally unique priorities could be inconvenient at
  larger scale (many groups, priority-number bookkeeping). →
  **Mitigation**: validation gives a clear error naming the conflicting
  group; relaxing this later (e.g. allowing ties with a documented
  tie-break) doesn't change the external ENC contract, only internal
  merge logic - safe to defer.
- **[Risk]** No OR within a rule means some classification intents need
  multiple groups instead of one. → **Mitigation**: this is a documented,
  explicit trade-off for a hand-parsed grammar over a general engine; the
  merge-by-priority model already handles multi-group overlap correctly,
  so there's no correctness gap, only an ergonomics one.
- **[Risk]** A second binary (`enc-bridge`) is a new deployment artifact
  the openvox-server container needs mounted in. → **Mitigation**: same
  module, same `go build`/Dockerfile family as `cmd/console`; documented
  alongside the `docker-compose.yml` `openvox` profile the same way the
  console client cert step already is.

## Migration Plan

New Postgres migration adding node group tables (groups, their classes/
parameters, their rule conditions, pinned nodes) to the console's existing
shared schema - no data migration, fresh tables. Rollout: merge, apply
migrations, build `enc-bridge` alongside `console`, mount it into the
`openvoxserver` container and set `node_terminus = exec` /
`external_nodes` in its `puppet.conf`, then verify with `make openvox-test`
that a node classified with a real group actually gets that class in its
compiled catalog.

## Open Questions

- Whether unique priority should eventually become "unique, but relaxable
  via an explicit tie-break setting" - deferred; doesn't change the specs
  or task breakdown, only a future validation-rule change.
- Exact fact-path syntax for nested/structured facts (e.g. reaching
  `os.family` the way openvoxdb's own structured facts are shaped, per
  Phase 1's `openvoxdb-client` findings) - an implementation parsing
  detail, not a behavior the specs constrain.
