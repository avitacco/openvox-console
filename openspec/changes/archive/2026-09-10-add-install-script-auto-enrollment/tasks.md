## 1. Node-facing Puppet server address

- [x] 1.1 Add the node-facing Puppet server address to
      `internal/runtime/config.go` (env var alongside
      `CONSOLE_NODE_TRANSPORT_PUBLIC_ADDR`, documented with the same
      "this is what a *remote node* dials" framing as
      `NodeTransportPublicAddr`) - verify with a case in
      `internal/runtime/config_test.go` covering set and unset
- [x] 1.2 Carry it on `agentdist.Config` and pass it from
      `cmd/console/main.go` - verify `go build ./...` and that
      `handlers_test.go` still constructs `Config` correctly
- [x] 1.3 Document the new setting in `.env`'s commented block and in
      `operations.md`, including why it must be node-reachable and what
      unset means - verify by reading back the rendered script for both
      set and unset in task 3.4's tests

## 2. Linux script enrollment

- [x] 2.1 Confirm which enrollment command the agent this project
      installs actually supports (`puppet ssl bootstrap` vs
      `puppet agent -t --waitforcert`) by checking the installed
      openvox-agent version in a real container - record the finding in
      design.md rather than assuming, and pick the command/fallback
      accordingly
- [x] 2.2 In `internal/agentdist/install_script.go`, set the Puppet
      server from the configured node-facing address before enrolling,
      and skip that entirely when it's unset - verify by rendering the
      script both ways and asserting the server line is present/absent
- [x] 2.3 Replace the "no signed certificate, exit 1" block with
      enrollment: keep the existing has-certificate fast path, submit
      the CSR, wait with the bounded window from design.md, then
      re-check the certificate path to decide the outcome - verify the
      rendered script's structure in `handlers_test.go`
- [x] 2.4 Implement the three outcomes distinctly per design.md:
      certificate now present (continue), connection-level failure
      (report that error), still unsigned (report pending signature and
      name this console's Nodes page URL) - verify each branch's text
      appears in the rendered script and that the console URL is
      interpolated, not hardcoded

## 3. macOS and Windows scripts

- [x] 3.1 Apply the same enrollment flow to
      `internal/agentdist/install_script_macos.go`, in that script's
      existing bash idiom - verify by rendering and reading it; note
      explicitly that it is static-verified only
- [x] 3.2 Apply the same enrollment flow to
      `internal/agentdist/install_script_win.go` in PowerShell, keeping
      its existing `Write-Error`/exit conventions - verify by rendering
      and reading it; static-verified only
- [x] 3.3 Confirm the certificate/key/CA paths each script exports
      afterwards (`NODE_AGENT_CERT_FILE` and friends on Windows, the
      env file on Linux/macOS) are still correct when the certificate
      was created moments earlier by enrollment rather than pre-existing
- [x] 3.4 Extend `internal/agentdist/handlers_test.go` to cover all
      three platforms' rendered scripts for: server address set, server
      address unset, and the presence of the enrollment and
      pending-signature branches - verify `go test ./internal/agentdist/...`

## 4. Live verification (Linux)

- [x] 4.1 Live: on a fresh systemd container with no Puppet
      certificate and no openvoxagent, run the served install script
      end-to-end against the dev openvoxserver with autosign enabled -
      confirm it installs the agent, enrolls, and finishes the
      node-agent-client install in one run, with the node appearing
      connected in the console - confirmed live: fresh systemd container, autosign on - agent installed, enrolled, node-agent-client installed and connected in one 90s run (exit 0)
- [x] 4.2 Live: repeat with autosign disabled - confirm the script
      waits, then sign the request from the console's Nodes page while
      it waits, and confirm the same run continues to completion - confirmed live, and this is where the waitforcert/maxwaitforcert bug was caught (see design.md): with the fix, signing from the console's own API mid-wait was picked up in 7s and the same run completed, node connected
- [x] 4.3 Live: repeat with autosign disabled and no one signing -
      confirm the script exits at the timeout with the pending-signature
      message naming the console URL, and that re-running it after
      signing completes the install - confirmed live (total wait shortened to 45s for the test, same branch): exit 1 with the pending-signature message naming the real console URL, node-agent-client not installed; signing then re-running completed the install (exit 0)
- [x] 4.4 Live: point the node-facing address at an unreachable host -
      confirm the failure is reported as a connection failure, not as a
      signing timeout - confirmed live: an unresolvable server produced "Could not reach the Puppet CA ... not a certificate waiting to be signed" with the underlying getaddrinfo/No-more-routes error shown, exit 1. Note the agent retries for the whole window first, so this reports after the wait expires rather than immediately - documented in operations.md
- [x] 4.5 Live: re-run the script on the now-provisioned node from 4.1 -
      confirm it takes the fast path, performs no enrollment, and
      disturbs nothing - confirmed live: zero enrollment lines, service still active, certificate mtime untouched, exit 0
- [x] 4.6 Run `go test -p 1 ./...`, `gofmt -l .`, `go vet ./...`; remove
      every test container and scratch artifact

## 5. Documentation
 - all clean: gofmt and vet silent, 24 packages ok, every enroll-test container removed, their certs cleaned from the CA, no openvoxdb residue, scratch files deleted
- [x] 5.1 Update `operations.md`: the new setting, the enrollment flow,
      the wait window and what an operator sees when a request is
      pending, and that macOS/Windows enrollment is static-verified only
 - done, including the AUTOSIGN override needed to exercise the non-autosigned path at all, and the note that an unreachable CA reports only after the wait expires