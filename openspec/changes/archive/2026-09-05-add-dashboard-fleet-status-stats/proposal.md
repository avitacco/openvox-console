## Why

The Dashboard's "Fleet status" summary currently renders one card per
raw openvoxdb report status (failed/noop/changed/unchanged/unreported,
whatever appears), plus a "Total nodes" count - a shape that grows or
shrinks with whatever statuses happen to exist, and lumps every
"changed" node together whether Puppet corrected unexpected drift or
simply applied an intentional code change. The user wants a fixed,
scannable row of four stats - failed, corrected, nodes with intentional
changes, unchanged - laid out like a stat-card row (see the reference
screenshot discussed in conversation), splitting "changed" into the two
categories that actually matter operationally: unwanted drift Puppet
had to fix versus expected changes from a deploy.

## What Changes

- Extend `internal/openvoxdb.Node` to parse `latest_report_corrective_change`
  (a boolean/null field already present on every real
  `nodes {}` query response - confirmed live against this project's
  actual openvoxdb; no PQL query change needed, only new struct
  parsing).
- Rework `GET /api/v1/nodes/summary`'s classification: each node maps
  to exactly one of `failed`, `corrected`, `intentional`, `unchanged`,
  or is excluded from these four buckets entirely (a `noop` node, or one
  with no report yet) - see design.md for the exact per-node rule.
  `total` remains the full node count, unaffected by this reclassification.
- Replace the Dashboard's dynamic per-status stat list with exactly
  four fixed `vox-stat` cards, in a horizontal row: Failed, Corrected,
  Intentional changes, Unchanged. The "Total nodes" card is removed.
- **Data availability note** (confirmed live against this project's real
  openvoxdb before writing this proposal): `latest_report_corrective_change`
  is currently `null` on every real report in this environment, because
  Puppet's corrective-change tracking (`corrective_change = true` in
  `puppet.conf`, or `--corrective_change` per run) is off by default and
  not enabled on any node here. Per the user's explicit choice, this
  change also enables that tracking on the project's real test agent
  (`openvox-testing-agent`) and live-verifies a real corrective change
  end to end, not just the plumbing - see tasks.md.

## Capabilities

### Modified Capabilities
- `inventory`: adds a "Fleet status summary" requirement - this is new
  ground (no existing requirement covers `/api/v1/nodes/summary` or the
  Dashboard's stat row today; confirmed by reading the current
  `inventory` spec before writing this delta), so it's written as an
  ADDED requirement under this existing capability rather than a
  MODIFIED one.

## Impact

- **Code**: `internal/openvoxdb/queries.go` (new `Node` field),
  `internal/inventory/handlers.go` (`nodesSummary` classification
  logic), `frontend/src/index.js` (fixed 4-stat rendering, replacing
  `orderedStatuses`/dynamic rendering), `frontend/templates/pages/
  index.tmpl` (no structural change expected - `#status-summary` stays
  the mount point).
- **APIs**: `GET /api/v1/nodes/summary`'s `byStatus` keys change meaning
  (from raw openvoxdb status strings to this app's four fixed
  categories) - this endpoint has exactly one consumer
  (`frontend/src/index.js`), confirmed by search, so this is not a
  breaking change for anything else in this codebase.
- **Infrastructure**: `openvox-testing-agent`'s Puppet configuration
  gains `corrective_change = true`, and a real managed resource on that
  container is deliberately drifted and corrected to produce genuine
  `corrected` data for live verification - not a permanent behavior
  change to any other node.
