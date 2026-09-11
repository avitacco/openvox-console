## 1. Validate the build-time tooling choice for real

- [x] 1.1 Add `github.com/goreleaser/nfpm/v2` as a build-time-only Go
      dependency and write a throwaway spike that builds a minimal
      `.deb` and `.rpm` from a trivial file set - verify both extract
      correctly (`dpkg-deb -x`/`rpm2cpio` in a scratch container) before
      writing any real packaging code, confirming design.md's "no
      system packaging toolchain needed" claim actually holds in this
      project's own build environment, not just in general - confirmed:
      a pure-Go spike (no cgo) built both a `.deb` and `.rpm` from a
      trivial file, and `dpkg-deb -I`/`-c` and `rpm -qip`/`-qlp` in
      real `debian:12-slim`/`rockylinux/rockylinux:9` containers both
      parsed them as fully valid packages
- [x] 1.2 Confirm no `.openspec.yaml`/design assumption breaks: check
      whether `nfpm` needs anything beyond a Go module fetch (e.g. cgo)
      that would complicate the existing `make build`/cross-compile
      flow in the Makefile - confirmed: `CGO_ENABLED=0 go build` on the
      spike succeeds (fits the existing `enc-bridge` CGO_ENABLED=0
      pattern), and building a package tagged `Arch: arm64` from the
      native amd64 build host worked with no cross-compilation of the
      builder itself needed - packaging is metadata + file-copy,
      architecture-agnostic to the host

## 2. Build the Linux packages

- [x] 2.1 Write an `nfpm` package spec (as Go-generated config or a
      template, matching this project's existing template-driven style
      in `internal/agentdist`) describing the node-agent-client `.deb`/
      `.rpm`: binary at a package-owned path, systemd unit at
      `/usr/lib/systemd/system/node-agent-client.service` (not `/etc/`
      - see design.md's Migration Plan), and the package-inventory
      facts.d script - verify by building both packages and confirming
      `dpkg -c`/`rpm -qlp` lists exactly the expected files - done as
      `cmd/build-agent-packages` (build-time only) plus static assets
      under `internal/agentdist/pkgassets/`; verified live with
      `dpkg-deb -I/-c` in a real `debian:12-slim` container and
      `rpm -qip/-qlp` in a real `rockylinux/rockylinux:9` container -
      both list exactly `/usr/bin/node-agent-client`,
      `/usr/lib/systemd/system/node-agent-client.service`, and
      `/opt/puppetlabs/facter/facts.d/package_inventory.sh`
- [x] 2.2 Add postinst/postrm maintainer scripts that enable+restart the
      systemd service on install/upgrade and clean up on removal -
      verify with a unit test asserting the generated maintainer script
      content, plus live verification in section 6 - done
      (`pkgassets/postinst.sh`, `pkgassets/postrm.sh`); postinst also
      resolves the design gap found during implementation (console
      config isn't known at package-build time - see design.md's new
      Decision) by re-deriving cert paths and reading the transport
      address from `/etc/node-agent-client/console.env` on every
      install/upgrade; content confirmed live via `dpkg-deb -e`/
      `rpm -q --scripts`; live functional verification in section 5
- [x] 2.3 Add a new Makefile target (alongside `agent-binaries`) that
      builds both packages for linux/amd64 and linux/arm64 at console
      build time - verify `make <new-target>` produces four package
      files (deb+rpm x amd64+arm64) - done (`make agent-packages`,
      depends on `agent-binaries`); ran it live, produced all four
      expected package files

## 3. Serve a minimal self-hosted apt/yum repository

- [x] 3.1 Implement Go-native apt repo metadata generation (a
      `Packages` file with name/version/architecture/filename/SHA256
      stanzas, gzip-compressed) for the built `.deb`s - verify with a
      unit test parsing the generated `Packages` file back and checking
      it matches the built package's real metadata and checksum - done
      in `internal/agentdist/repo.go`; live-verified: a real
      `debian:12-slim` container's `apt-get update` fetched `Release`
      and `Packages` from the running console and parsed them
      correctly. Caught and fixed a real bug in the process: dpkg
      rejected the literal `"dev"` fallback version
      ("version number does not start with digit") - `sanitizeVersions`
      now normalizes to `0.0.0+dev`
- [x] 3.2 Implement Go-native yum repo metadata generation
      (`repomd.xml`/`primary.xml`) for the built `.rpm`s - verify
      similarly with a unit test - done in `repo.go`, hand-formatted
      (not `encoding/xml`) to match real `createrepo` output shape
      exactly, including the `rpm:`-namespaced `<format>` fields;
      content verified by decompressing and reading real output from
      the running console
- [x] 3.3 Add routes in `internal/agentdist/handlers.go` serving the
      repo (packages + metadata) and a small repo-config snippet route
      (an apt `.list`/`.sources` entry marked `trusted=yes`, and a yum
      `.repo` file with `gpgcheck=0`) - verify with `handlers_test.go`
      cases asserting content-type, the unsigned/trusted markers per
      design.md's signing decision, and that requesting the repo
      returns metadata matching the packages actually being served -
      routes added; hit a real Go stdlib constraint along the way
      (`http.ServeMux` wildcards must occupy a whole path segment -
      `binary-{arch}` isn't valid, fixed by capturing the whole
      `{binarydir}` segment and trimming the `binary-` prefix in the
      handler); unit tests added (`TestAptSourceList_IsTrusted`,
      `TestYumRepoFile_IsUnsigned`, `TestAptPackages_MatchesManifest`,
      `TestAptPackages_UnknownArch404s`,
      `TestAptPackageFile_ServesRealDebBytes` - checks real ar-archive
      magic bytes, `TestYumPackageFile_ServesRealRpmBytes` - checks real
      rpm lead magic bytes, `TestYumRepomdAndPrimary_ReferenceEachOther`,
      `TestPackageRoutes_404WhenNotBuilt`)

## 4. Update the Linux install script

- [x] 4.1 Replace `install_script.go`'s raw `curl -o`/temp-rename
      node-agent-client download step with: add the console's repo
      config (from section 3's route) and run `apt-get install`/
      `yum install` for node-agent-client - verify with a
      `TestInstallScript_...` case asserting the script adds the repo
      and calls the package manager rather than downloading a raw
      binary - done; also writes `/etc/node-agent-client/console.env`
      (the design.md fix for console-specific config not being known
      at package-build time); `TestInstallScript_InstallsNodeAgentClientViaPackageManager`
      added; shellcheck on the live-rendered script is clean (only the
      two pre-existing informational SC1091 notices)
- [x] 4.2 Add the one-time migration step from design.md's Migration
      Plan: detect and remove a pre-existing hand-written
      `/etc/systemd/system/node-agent-client.service` and the old
      unmanaged binary at `/opt/openvox-console/bin/node-agent-client`
      before installing the package, so a node upgrading from the
      previous script-based install doesn't hit a dpkg/rpm file-conflict
      error - verify with a unit test asserting this cleanup step is
      present and only runs when the legacy path actually exists - done;
      `TestInstallScript_MigratesOldScriptManagedInstall` added; full
      live verification (an actual node making this transition) in 5.4
- [x] 4.3 Confirm `go build ./...` succeeds and existing
      `internal/agentdist` tests still pass (adjusted for the new
      script shape) - confirmed, all pass

## 5. Live verification: Linux end-to-end, including the original bug

- [x] 5.1 Live: fresh Debian-family container, run the updated
      install.sh through to a real `apt-get install node-agent-client`
      against the console's own served repo, confirm the service starts
      and connects (same pattern as `add-multi-platform-agent-install`'s
      live verification) - confirmed on a genuinely fresh, real-systemd
      Ubuntu 24.04 container: full install.sh run through openvoxagent
      install, enrollment, then a real `apt-get install node-agent-client`
      against the console's live-served apt repo; `systemctl status`
      showed it actively running under `/usr/bin/node-agent-client`;
      the console's own `/metrics` showed `console_node_agent_connected`
      increment, confirming a real transport connection, not just a
      started process
- [x] 5.2 Live: repeat against a fresh RedHat-family container via
      `yum install` - confirmed on a genuinely fresh, real-systemd
      `rockylinux/rockylinux:9-ubi-init` container: real `openvox-agent`
      install via the existing yum branch, enrollment, then a real
      `yum install node-agent-client` against the console's live-served
      yum repo; service ran correctly; package-inventory fact produced
      real rpm-provider rows
- [x] 5.3 Live: reproduce the exact scenario that caused the original
      bug - re-run the install script while node-agent-client is
      actively running as a systemd service (using a real
      systemd-enabled container, not a plain `docker exec` session,
      per the lesson learned diagnosing the original curl(23) issue) -
      confirm `apt-get install`/`yum install` upgrades the running
      service cleanly with no error, closing out the
      "Installing or upgrading node-agent-client tolerates a running
      instance" requirement from this change's delta spec - confirmed
      on both families: re-ran the true `curl | bash` pipe form while
      the service was actively running; apt cleanly upgraded it (PID
      changed 1117→1432, no curl(23), no manual temp-file/rename dance
      needed at all); yum reported "Nothing to do" cleanly on the
      already-current version - dpkg/rpm handle the exact scenario that
      broke the old script, by design, with zero workaround code
- [x] 5.4 Live: simulate a node that was enrolled under the *old*
      script-based install (hand-written systemd unit + raw binary
      already in place) and confirm the migration step in 4.2 lets the
      new package install cleanly take over, rather than erroring on a
      file conflict - confirmed: hand-built the exact old-style
      hand-written unit + raw binary, started it running for real, then
      ran the new install.sh - printed the migration message, removed
      the old unit and binary, installed the package with no dpkg
      conflict, and `dpkg -S /usr/bin/node-agent-client` confirmed the
      package now owns the path
- [x] 5.5 Run `go test ./internal/agentdist/...` and clean up all test
      containers - all 22 tests pass; all live-test containers removed

## 6. Documentation

- [x] 6.1 Update `operations.md`'s "Multi-platform agent install" and
      "Real-node install bugs found via an actual VM" sections to
      describe the packaged Linux install path and explicitly document
      the decision (from design.md) not to pursue native packaging for
      Windows/macOS, with the reasoning, so this doesn't read as an
      oversight later - done; also added a note to the "Real-node
      install bugs" section explaining the earlier curl(23)/ETXTBSY fix
      (temp-file+rename) is now fully superseded by native packaging,
      so a future reader doesn't think that workaround is still live
- [x] 6.2 Run the full test suite (`make test`), confirm it passes,
      `gofmt -l .` and `go vet ./...` clean - `gofmt -l .` and
      `go vet ./...` both clean; `go test -p 1 ./...` (same pattern the
      prior changes established, avoiding embedded-NATS port
      contention) passed fully across every package
