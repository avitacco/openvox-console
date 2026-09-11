## 1. openvoxdb-client: command submission and single-node lookup

- [x] 1.1 Add a command-submission method to `internal/openvoxdb.Client`
      (`POST /pdb/cmd/v1?command=...&version=...&certname=...`,
      authenticated the same way as `query`) and a `DeactivateNode(ctx,
      certname string) error` built on it, generating a current
      `producer_timestamp` at call time (never accepting one from the
      caller - see design.md's staleness risk) - done
      (`internal/openvoxdb/commands.go`); verified live via
      `TestDeactivateNode_Success` (a real, synthetic certname against
      the real dev openvoxdb, polling via `NodeByCertname` since
      commands are processed asynchronously)
- [x] 1.2 ~~Change `Nodes(ctx)` to `Nodes(ctx, includeInactive bool)`~~
      - superseded during implementation: live testing proved
      openvoxdb's `nodes` query already excludes deactivated/expired
      nodes unconditionally, with no override, so no parameter is
      needed or possible here (see design.md's corrected Context).
      `Nodes()` is unchanged. Added `NodeByCertname(ctx, certname
      string) (*Node, error)` instead, using openvoxdb's single-node
      lookup route (which behaves differently - still returns a
      deactivated node) and a new `ErrNodeNotFound` sentinel for a
      truly unknown certname - verified with `TestNodes_UsesUnfilteredQuery`
      (fake server, asserts `Nodes()` still sends plain `nodes {}`),
      `TestNodeByCertname_FindsAlreadyDeactivatedNode` and
      `TestNodeByCertname_UnknownCertnameReturnsErrNodeNotFound` (both
      live, against the real dev openvoxdb)
- [x] 1.3 ~~Update both existing call sites~~ - not needed:
      `Nodes()`'s signature never changed, so
      `internal/inventory/handlers.go` and
      `internal/groupnodes/handlers.go` needed no changes on this axis

## 2. internal/inventory: delete endpoint

- [x] 2.1 ~~Add a `showInactive` query parameter~~ - dropped along with
      the "show inactive" feature (see design.md) - not needed, since
      openvoxdb already excludes deactivated/expired nodes from
      `Nodes()` unconditionally
- [x] 2.2 Add `DELETE /api/v1/nodes/{name}` gated by the new
      `nodes:manage` permission, calling `DeactivateNode` - done; the
      existence check uses `NodeByCertname`, not `Nodes()` (see
      design.md for why `Nodes()` can't distinguish "unknown" from
      "already deactivated"); verified with
      `TestDeleteNode_ActiveNodeSucceeds`,
      `TestDeleteNode_AlreadyDeactivatedSucceeds`,
      `TestDeleteNode_UnknownCertnameReturns404`,
      `TestDeleteNode_DeactivateErrorPropagates`, and
      `TestRegister_DeleteRequiresNodesManage` (confirms the route is
      wired to `nodes:manage`, distinct from every read endpoint's
      `nodes:read`)
- [x] 2.3 Emit an audit event for a processed deletion
      (`internal/auditlog`, matching this capability's existing
      `recordAuditRead` pattern but for a write) - done; `NewHandlers`
      gained a `recordAudit` parameter (mirroring
      `internal/nodeconnectivity`'s existing recordAudit/recordAuditRead
      split), wired to `auditWrite(auditlog.CategoryNodes)` in
      `cmd/console/main.go`; verified via `TestDeleteNode_ActiveNodeSucceeds`
      asserting the `node.deleted` event
- [x] 2.4 (found live, not in the original plan) Also clean the node's
      CA certificate record as part of deletion - a real gap found by
      testing in a browser: the node list unions openvoxdb inventory
      with the separate connectivity/CA registry, so a deactivated node
      with a lingering cert kept reappearing. Added a `certCleaner`
      interface (`Clean(ctx, certname) error`), satisfied by the same
      `*certstatus.Client` already wired as `caClient` in
      `cmd/console/main.go` for `nodeconnectivity` - passed through to
      `inventory.NewHandlers` too. Tolerates `*certstatus.NotFoundError`
      (nothing to clean) and a nil `certCleaner` (no CA client
      configured); any other error propagates. Verified with
      `TestDeleteNode_NoCertClientConfiguredStillSucceeds`,
      `TestDeleteNode_NoCertRecordToCleanIsNotAnError`,
      `TestDeleteNode_CertCleanErrorPropagates`, and
      `TestDeleteNode_ActiveNodeSucceeds` now also asserting `Clean` was
      called

## 3. Frontend: delete action and confirmation

- [x] 3.1 Add a "Delete" action to the node list/detail view that opens
      a `confirmDialog` (matching the existing cert revoke/clean
      pattern in `frontend/src/nodes.js`) - the dialog text must make
      clear this is not easily reversible (no "show inactive" undo
      path exists - see design.md's Risks): state that the node will
      be deactivated and disappear from the list immediately, and that
      its historical data is removed only later, by openvoxdb's own
      background cleanup - verify by exercising it in a browser against
      the dev console - done (`certActionsCell` in
      `frontend/src/nodes.js`, gated on the new `nodes:manage`
      permission, shown regardless of cert status); dialog copy also
      states the certificate is cleaned and the action cannot be undone
      from the console; verified live in a real Chromium browser
      (playwright-core against the system browser)
- [x] 3.2 On confirm, call the new delete endpoint (`DELETE
      /api/v1/nodes/{name}`) and remove the node from the displayed
      list without a full page reload - verify visually - done
      (`runDeleteAction`, reloads the node list via the existing
      `load()`); along the way, found and fixed a real bug this
      exposed: `fetchJSON` had no cache-busting, so Chromium's default
      heuristic HTTP caching could serve a stale node list after a
      delete - added `cache: 'no-store'` to every request in
      `frontend/src/app.js`'s `fetchJSON` (affects all API calls, not
      just this one - none of this app's API responses should ever be
      browser-cached)

## 4. Live verification

- [x] 4.1 Live: against the real running console/openvoxdb, deactivated
      a real leftover test certname directly via the command API and
      confirmed via `nodes/<certname>` it now shows a real
      `deactivated` timestamp - done during design research, ahead of
      the handler existing yet
- [x] 4.2 Live: through the actual UI, delete a real test node end to
      end - confirmed it disappears from the node list (via a fresh
      page reload, not just the in-memory DOM, to rule out any
      render-timing artifact) and confirmed via both `/api/v1/nodes`
      and `/api/v1/node-connectivity` that it's gone from both the
      openvoxdb inventory and the CA/connectivity registry - i.e. the
      full combined delete+clean behavior from task 2.4, not just
      deactivation
- [x] 4.3 Live: confirm deleting a node that's already deactivated
      succeeds without error - covered by
      `TestDeleteNode_AlreadyDeactivatedSucceeds` against a fake
      client, and live during 4.2's own iteration (several of this
      session's test nodes were deleted more than once while narrowing
      down the cert-cleaning gap, each time succeeding cleanly)
- [x] 4.4 Live: confirm deleting an unknown certname returns a clear
      error, not a false success - covered by
      `TestNodeByCertname_UnknownCertnameReturnsErrNodeNotFound`
      (real openvoxdb) and `TestDeleteNode_UnknownCertnameReturns404`
      (handler logic)
- [x] 4.5 Run `go test -p 1 ./...`, `gofmt -l .`, `go vet ./...` - all
      clean

## 5. Documentation

- [x] 5.1 Update `operations.md`: document the delete action, that it
      deactivates and cleans the node's certificate rather than
      force-purging its data, that historical data lingers until
      openvoxdb's own background garbage collection clears it, and -
      importantly, since these were real findings, not assumptions -
      that openvoxdb provides no way to list deactivated nodes back out
      at all once deactivated (only a single-node lookup by exact
      certname still works, no "undo"/"recently deleted" view is
      planned), and that the node list is a union of openvoxdb
      inventory and the separate connectivity/CA registry (why deletion
      has to touch both to actually work) - done ("Node deletion:
      deactivate + clean cert, not a data purge")
- [x] 5.2 Update `internal/openvoxdb`'s package doc comment (done -
      already updated when `command()`/`NodeByCertname` were added in
      task 1.1/1.2) and `openvoxdb-client`'s main spec Purpose (still
      pending - happens directly on the main spec at archive time, per
      this project's normal process for a Purpose change, not during
      apply)
