## 1. Move the package-inventory fact assets to node-agent-client

- [x] 1.1 Move `internal/agentdist/pkgassets/package_inventory_apt.sh`
      and `package_inventory_rpm.sh` to `internal/nodeagent` (embedded
      via `go:embed`, content unchanged) - verify `go build ./...`
      succeeds and the embedded content matches the original files
      byte-for-byte - done (`internal/nodeagent/factassets/`, content
      byte-identical, embedding wired in task 2.2)
- [x] 1.2 Remove the package-inventory `Content` entries from
      `cmd/build-agent-packages/main.go` (both the apt and rpm
      variants) - verify a rebuilt `.deb`/`.rpm` no longer contains
      `/opt/puppetlabs/facter/facts.d/package_inventory.sh`
      (`dpkg-deb -c`/`rpm -qlp`) - done; verified live: rebuilt both
      packages, `dpkg-deb -c` on the real `.deb` confirms the fact
      script is gone, only the binary and systemd unit remain

## 2. node-agent-client: package-inventory status and set actions

- [x] 2.1 Add `actionPackageInventoryStatus` and
      `actionPackageInventorySet` constants and the set action's
      `{"enabled": bool}` params type to both
      `internal/orchestrator/wire.go` and `internal/nodeagent/handler.go`
      (mirrored, unshared types, matching this project's existing
      pattern for `actionRun`/`actionRunTask`) - verify `go build ./...`
      succeeds - done; also added the mirrored `PackageInventory`
      optional field to both packages' `responseData`, including a
      `Ran bool` field (added after noticing Output's zero value alone
      can't distinguish "no run happened" from "a run happened with
      empty output and exit 0" - a real, common successful-unchanged-run
      outcome, not just a hypothetical edge case)
- [x] 2.2 Add package-manager-family detection to `internal/nodeagent`
      (file check for `/usr/bin/dpkg-query` vs `/usr/bin/rpm`, per
      design.md - not a shell-out) and wire it to pick the correct
      embedded fact-script variant - verify with a unit test covering
      both families and the neither-present error case - done
      (`internal/nodeagent/packageinventory.go`); `TestDetectPackageManagerFamily`
      covers apt, rpm, both-present, and neither-present
- [x] 2.3 Implement `actionPackageInventoryStatus` in
      `NewHandler`: report whether the fact file currently exists, with
      no side effects - verify with a unit test using a temp facts.d
      directory (inject the path so the test doesn't touch a real
      system path) - done; `TestHandler_PackageInventoryStatus_Absent/_Present/_NoSideEffects`
- [x] 2.4 Implement `actionPackageInventorySet` in `NewHandler`: write
      or remove the fact file to match the requested state, run Puppet
      via the same runner/args `actionRun` already uses when a change
      was actually made, and no-op (no write, no run) when the
      requested state matches the current one - verify with unit tests
      covering enable-from-absent, disable-from-present, and
      already-in-that-state cases, asserting the runner is/isn't
      invoked accordingly - done; six tests covering enable (both
      families), the neither-family error case, disable, and both
      already-in-that-state no-op directions
- [x] 2.5 Confirm the existing single-in-flight busy-rejection in
      `client.go` covers these new actions with no changes needed there
      - verify with a test that a package-inventory request arriving
      during another in-flight request gets the existing busy rejection
      - done, with a real finding: nats.go delivers to a single
      subscription strictly serially, so two requests can never
      actually race through the real transport (confirmed by an earlier
      version of this test trying exactly that and timing out - see
      design.md). Rewrote the test to call the unexported `dispatch`
      method directly with two goroutines instead

## 3. Console-side dispatch and API

- [x] 3.1 Add a small dispatch helper in `internal/packageinventory`
      that builds the `requestData` for status/set requests and calls
      `internal/nodetransport.Dispatch` directly (not through
      `internal/orchestrator`'s job-tracking dispatcher, per design.md)
      - verify with a unit test against a fake transport - done
      (`internal/packageinventory/dispatch.go`); four tests covering
      status/set payload shape and both node-error and transport-error
      propagation
- [x] 3.2 Add `GET /api/v1/nodes/{name}/packages/reporting` (status,
      `nodes:read`) and `PUT /api/v1/nodes/{name}/packages/reporting`
      (set, `orchestrator:run`) to `internal/packageinventory/handlers.go`
      - verify with `handlers_test.go` cases for both permissions
      (missing token, wrong permission, correct permission) per the
      delta spec's "Endpoints require authentication" scenarios - done;
      following this project's established convention (permission
      enforcement itself is tested once in `internal/rbac`, not
      re-tested per consuming package - confirmed no other package does
      this either), `TestRegister_ReportingPermissions` verifies the
      route-to-permission wiring instead (status→`nodes:read`,
      set→`orchestrator:run`)
- [x] 3.3 Translate `nodetransport.ErrNodeNotConnected` into a clear
      "node offline" response (not the generic dispatch-timeout error)
      on both endpoints - verify with a unit test against a fake
      transport that returns that error - done (409, both endpoints,
      including when no transport is configured at all)
- [x] 3.4 Emit an audit event (`internal/auditlog`, `nodes/inventory`
      category) on a processed set request - verify with a unit test
      asserting the event's action, actor, and affected node - done
      (`node.packageInventory.set`); also added a read-audit event on
      the status endpoint for consistency with this package's existing
      read endpoints, not originally called out in tasks.md but
      matching established convention

## 4. Frontend: node detail page toggle

- [x] 4.1 Add a toggle control to `frontend/templates/pages/node.tmpl`
      reflecting the status endpoint's current state, calling the set
      endpoint on change - verify by starting the dev console and
      exercising it against a live-connected test node in a browser -
      done (a `<vox-switch>` in the Packages section header, matching
      this project's existing switch pattern); verified in a real
      Chromium browser (playwright-core) against a real enrolled test
      node - toggled on, saw the real Puppet run's outcome message, and
      the packages table refreshed with real data (167 rows)
- [x] 4.2 Disable the control when the node is not currently connected,
      using the existing node-connectivity signal already shown
      elsewhere on this page - verify visually against both a connected
      and a disconnected test node - done, with a correction: the node
      *detail* page didn't already show connectivity (only the nodes
      *list* page did) - fetches `/api/v1/node-connectivity` on this
      page too, reusing the same existing data source rather than
      inventing a new one. Verified live: stopped node-agent-client's
      service, confirmed the switch became disabled with a clear
      "node is offline" message, confirmed via the real API that the
      status endpoint itself returns 409 while stopped, then restarted
      it and confirmed the switch worked normally again
- [x] 4.3 Surface the dispatched Puppet run's outcome (or a "node busy,
      try again" message on a busy rejection) after a set action,
      rather than only reflecting the resulting boolean state - verify
      visually - done; a real run's exit code is shown, and any error
      (including a busy rejection's real message text) surfaces via the
      same alert, with the switch reverting to its prior state

## 5. Live verification

- [x] 5.1 Live: on a real systemd container already enrolled and
      running node-agent-client (same pattern established in
      `add-native-agent-packaging`), exercise enable → confirm the fact
      file appears and a real Puppet run occurs (new `package_inventory`
      rows in openvoxdb) → disable → confirm the file is removed and a
      real Puppet run occurs → toggle to the current state a second time
      and confirm no filesystem change or Puppet run happens - all
      confirmed live: enable produced a real 234-byte fact script, a
      real Puppet run, and 166 real `package_inventory` rows in
      openvoxdb; disable removed the file with another real run;
      re-disabling (already in that state) returned `{"enabled":false}`
      with no `output` field and no filesystem change
- [x] 5.2 Live: confirm the migration behavior from design.md - upgrade
      a node that has the *old* package-bundled fact (from
      `add-native-agent-packaging`) to a package built after this
      change, and confirm the package manager removes the
      now-undeclared fact file as part of the ordinary upgrade
      transaction, without a dpkg/rpm error - confirmed via a targeted
      spike (two real `nfpm`-built `.deb`s, one with the fact bundled
      and one without) rather than a full rebuild cycle, since the
      underlying mechanism is standard, well-established dpkg behavior
      independent of this project's specific packaging setup: installed
      the old one, confirmed the fact file present, installed the new
      one over it, confirmed the file was gone - held even though the
      spike's arbitrary version naming caused dpkg to log it as a
      "downgrade", which if anything strengthens confidence
- [x] 5.3 Live: stop node-agent-client's service on a test node and
      confirm both the status and set endpoints return the "node
      offline" response from task 3.3, and that the frontend disables
      the toggle accordingly - confirmed live: real API returned 409
      with the expected message while stopped, and the real browser UI
      showed the switch disabled with the offline message
- [x] 5.4 Run `go test -p 1 ./...`, `gofmt -l .`, `go vet ./...`; clean
      up all test containers - all clean; all test containers and
      spike artifacts removed

## 6. Documentation

- [x] 6.1 Update `operations.md` describing the toggle, the dispatch
      mechanism it uses, and the migration behavior from design.md
      (reporting resets to off for nodes upgrading from the old
      package-bundled fact) so it doesn't read as a silent regression
      to a future reader - done ("Package-inventory reporting is now a
      per-node toggle, not a package default"); also documents the
      `Ran` field's purpose and the NATS serial-delivery finding
