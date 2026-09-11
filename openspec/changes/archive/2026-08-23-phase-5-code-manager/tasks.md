## 1. Schema

- [x] 1.1 Add a migration for `deploys` (id, ref, status, started_at,
      finished_at, triggered_by, error_detail) and verify it applies
      cleanly and is a no-op on rerun

## 2. Dev: real g10k binary and control repo fixture

- [x] 2.1 Add a `make g10k-install` target installing a real
      `voxpupuli/g10k` binary into `bin/` (matching `make rbac-keys`'s
      one-time-setup pattern), and verify the installed binary actually
      runs (`bin/g10k -version` or equivalent) and resolves a real
      Puppetfile against a real Forge/git source
- [x] 2.2 Create a real local bare git control repo fixture (a real
      Puppetfile plus `site.pp`/manifests, committed - not a mock) for
      local dev/testing, document the one-time setup in README.md, and
      verify `git clone` against it (from a scratch directory) actually
      produces the expected working tree

## 3. codemanager: g10k subprocess wrapper

- [x] 3.1 Implement g10k config generation (temp YAML file: `sources`
      pointing at the configured control repo, `basedir` pointing at a
      fresh versioned deploy directory) and invocation via `os/exec`
      with a bounded `context.Context` timeout, capturing stdout/stderr,
      and verify against the real fixture repo (task 2.2) and real
      binary (task 2.1): a real deploy attempt resolves the fixture's
      Puppetfile and populates the versioned directory with the expected
      modules
- [x] 3.2 Verify a deploy against a control repo with an intentionally
      broken Puppetfile (e.g. an unresolvable module source) fails
      cleanly - non-zero exit captured, no partial state left in a
      location anything reads from yet (the versioned staging directory
      isn't activated until swap)

## 4. codemanager: atomic activation

- [x] 4.1 Implement the stage-then-swap activation described in
      design.md (`os.Symlink` to a temp name, `os.Rename` over
      `openvox-code/environments/<environment>`), and verify against a
      real filesystem: after activation, the environment path resolves
      to the new versioned directory's content
- [x] 4.2 Verify the atomicity claim for real, not just by code
      inspection: while repeatedly reading through the environment path
      in a tight loop from a separate goroutine, trigger an activation
      and confirm every read observes either the old or the new content
      in full, never a mix or a missing-file error
- [x] 4.3 Verify a failed deploy (task 3.2) never calls activation - the
      live environment path still resolves to the previously active
      version afterward

## 5. codemanager: deploy status store

- [x] 5.1 Implement `Store.CreateDeploy` (status `running`),
      `Store.CompleteDeploy` (status `succeeded`/`failed` + error
      detail + finished_at), and `Store.ListDeploys` (most recent
      first), and verify all three against a real Postgres instance

## 6. codemanager: deployment-ready event

- [x] 6.1 Implement publishing `codemanager.deployed` (environment, ref,
      deployedAt) on the NATS bus only after a successful activation,
      and verify with a real embedded NATS bus (mirroring
      `internal/activity`'s `Recorder` test pattern): a real successful
      deploy produces the event, a real failed deploy does not

## 7. codemanager: HTTP API

- [x] 7.1 Implement `POST /api/v1/code-deploys` (manual trigger, gated
      by `code:deploy`) running the full deploy pipeline (tasks 3-6) end
      to end, and verify via real HTTP request against a live console
      with the real fixture repo that triggering a deploy actually
      updates the live environment directory
- [x] 7.2 Implement `POST /api/v1/code-deploys/webhook` verifying the
      `X-Hub-Signature-256` HMAC header against
      `CONSOLE_CODE_WEBHOOK_SECRET` before running the same pipeline,
      and verify via a real HTTP request with a correctly computed HMAC
      signature that it triggers a deploy, and that an incorrect/missing
      signature is rejected without running one
- [x] 7.3 Implement `GET /api/v1/code-deploys` (history/status, gated by
      `code:read`), and verify via real HTTP request that it returns
      real recorded deploy attempts and rejects a request without
      `code:read`
- [x] 7.4 Add `code:deploy` and `code:read` to
      `cmd/console/main.go`'s `allPermissions`, and verify a freshly
      bootstrapped admin can trigger a deploy and view history

## 8. codemanager: activity integration

- [x] 8.1 Wire a `recordActivity` closure into the codemanager handlers
      (same injected-function pattern as `classifier`/`rbac`), recording
      an event on both successful and failed deploys, and verify via
      real HTTP requests against a live console that both outcomes
      produce a matching entry in `GET /api/v1/audit-log`

## 9. Frontend: deploy status page

- [x] 9.1 Add `deploys.html`/`deploys.js` listing deploy history
      (ref, status, triggered by, timestamps), a manual-trigger button
      gated by client-side `code:deploy`, and page access gated by
      client-side `code:read` (matching the existing admin/activity
      pages' pattern), and verify via browser it renders real deploy
      history from a live console and a manual trigger through the UI
      produces a new entry
- [x] 9.2 Add a "Deploys" nav link to every existing page's header (same
      pattern as the Activity link), and verify via browser it's
      reachable from every page

## 10. Final verification

- [x] 10.1 Verify, with a token lacking `code:deploy`/`code:read`, that
      both API endpoints reject the request and direct navigation to
      `/deploys.html` redirects away client-side
- [x] 10.2 Verify the full loop end to end against the real fixture
      repo: push a change to the control repo, send a real HMAC-signed
      webhook request, confirm the deploy appears in history, the live
      environment directory reflects the new code, the deployment-ready
      event fired, and an activity entry was recorded - all without a
      console restart
- [x] 10.3 `gofmt -l .`, `go vet ./...`, `make test` all pass with no
      regressions
