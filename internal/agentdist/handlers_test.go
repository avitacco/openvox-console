package agentdist

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDownloadBinary_SupportedPlatform(t *testing.T) {
	h := NewHandlers(Config{})

	req := httptest.NewRequest(http.MethodGet, "/packages/node-agent-client?os=linux&arch=amd64", nil)
	rec := httptest.NewRecorder()
	h.downloadBinary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	if rec.Body.Len() == 0 {
		t.Error("response body is empty, want a binary")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream", ct)
	}
}

func TestDownloadBinary_UnsupportedPlatform(t *testing.T) {
	h := NewHandlers(Config{})

	// windows/arm64: OpenVox publishes no Windows ARM64 openvoxagent
	// package, so this project doesn't build a node-agent-client for it
	// either - see agentdist.go's supportedPlatforms doc comment.
	req := httptest.NewRequest(http.MethodGet, "/packages/node-agent-client?os=windows&arch=arm64", nil)
	rec := httptest.NewRecorder()
	h.downloadBinary(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestDownloadBinary_MissingParamsIsUnsupported(t *testing.T) {
	h := NewHandlers(Config{})

	req := httptest.NewRequest(http.MethodGet, "/packages/node-agent-client", nil)
	rec := httptest.NewRecorder()
	h.downloadBinary(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestInstallScript_ReturnsWellFormedScript(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080", TransportAddr: "transport.example.com:7422"})

	req := httptest.NewRequest(http.MethodGet, "/packages/install.sh", nil)
	rec := httptest.NewRecorder()
	h.installScript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "#!/usr/bin/env bash") {
		t.Error("script does not start with a shebang")
	}
	if !strings.Contains(body, "https://console.example.com:8080") {
		t.Error("script does not reference the configured console base URL")
	}
	if !strings.Contains(body, "transport.example.com:7422") {
		t.Error("script does not reference the configured broker address")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/x-shellscript") {
		t.Errorf("Content-Type = %q, want text/x-shellscript", ct)
	}
}

func TestInstallScript_CoversDebianAndRedHatFamilies(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080", TransportAddr: "transport.example.com:7422"})

	req := httptest.NewRequest(http.MethodGet, "/packages/install.sh", nil)
	rec := httptest.NewRecorder()
	h.installScript(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "apt-get") {
		t.Error("script does not contain a Debian/Ubuntu (apt-get) branch")
	}
	if !strings.Contains(body, "yum.voxpupuli.org") {
		t.Error("script does not contain a RedHat-family (yum/dnf) branch")
	}
	for _, family := range []string{"rhel", "fedora", "amzn"} {
		if !strings.Contains(body, family) {
			t.Errorf("script does not handle distro ID %q", family)
		}
	}
}

func TestInstallScriptWindows_ReturnsWellFormedScript(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080", TransportAddr: "transport.example.com:7422"})

	req := httptest.NewRequest(http.MethodGet, "/packages/install.ps1", nil)
	rec := httptest.NewRecorder()
	h.installScriptWindows(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "#Requires -RunAsAdministrator") {
		t.Error("script does not start with the admin-requirement directive")
	}
	if !strings.Contains(body, "'https://console.example.com:8080'") {
		t.Error("script does not reference the configured console base URL")
	}
	if !strings.Contains(body, "'transport.example.com:7422'") {
		t.Error("script does not reference the configured transport address")
	}
	if !strings.Contains(body, "New-Service") || !strings.Contains(body, "msiexec.exe") {
		t.Error("script does not install the MSI and register a Windows service")
	}
	if !strings.Contains(body, "os=windows&arch=amd64") {
		t.Error("script does not fetch the windows/amd64 node-agent-client build")
	}
}

func TestPSQuote_EscapesSingleQuotes(t *testing.T) {
	got := psQuote(`it's a "test"`)
	want := `'it''s a "test"'`
	if got != want {
		t.Errorf("psQuote() = %q, want %q", got, want)
	}
}

func TestInstallScriptMacOS_ReturnsWellFormedScript(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080", TransportAddr: "transport.example.com:7422"})

	req := httptest.NewRequest(http.MethodGet, "/packages/install-macos.sh", nil)
	rec := httptest.NewRecorder()
	h.installScriptMacOS(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "#!/usr/bin/env bash") {
		t.Error("script does not start with a shebang")
	}
	if !strings.Contains(body, "https://console.example.com:8080") {
		t.Error("script does not reference the configured console base URL")
	}
	if !strings.Contains(body, "transport.example.com:7422") {
		t.Error("script does not reference the configured transport address")
	}
	if !strings.Contains(body, "hdiutil -quiet attach") || !strings.Contains(body, "installer -pkg") {
		t.Error("script does not mount the dmg and run its installer")
	}
	if !strings.Contains(body, "launchctl bootstrap system") {
		t.Error("script does not register the launchd service")
	}
	if !strings.Contains(body, "os=darwin") {
		t.Error("script does not fetch a darwin node-agent-client build")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/x-shellscript") {
		t.Errorf("Content-Type = %q, want text/x-shellscript", ct)
	}
}

// Package-inventory reporting is now delivered by the node-agent-client
// package itself (its facts.d script is one of the files nfpm bundles -
// see cmd/build-agent-packages), not written by install.sh - see
// TestBuildPackageRepo_IncludesPackageInventoryFact and design.md in
// add-native-agent-packaging.

func TestInstallScript_InstallsNodeAgentClientViaPackageManager(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080", TransportAddr: "transport.example.com:7422"})

	req := httptest.NewRequest(http.MethodGet, "/packages/install.sh", nil)
	rec := httptest.NewRecorder()
	h.installScript(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "curl -fsSL -o /opt/openvox-console/bin/node-agent-client") {
		t.Error("script still downloads a raw node-agent-client binary - should install the package instead")
	}
	if !strings.Contains(body, "/packages/apt/node-agent-client.list") {
		t.Error("script does not fetch the apt repo config snippet")
	}
	if !strings.Contains(body, "/packages/yum/node-agent-client.repo") {
		t.Error("script does not fetch the yum repo config snippet")
	}
	if !strings.Contains(body, "apt-get install -y node-agent-client") {
		t.Error("script does not apt-get install the node-agent-client package")
	}
	if !strings.Contains(body, "dnf install -y node-agent-client") && !strings.Contains(body, "yum install -y node-agent-client") {
		t.Error("script does not dnf/yum install the node-agent-client package")
	}
	if !strings.Contains(body, "NODE_AGENT_TRANSPORT_ADDR=") || !strings.Contains(body, "/etc/node-agent-client/console.env") {
		t.Error("script does not write the console-specific transport address for the package's postinst to pick up")
	}
}

func TestInstallScript_MigratesOldScriptManagedInstall(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080", TransportAddr: "transport.example.com:7422"})

	req := httptest.NewRequest(http.MethodGet, "/packages/install.sh", nil)
	rec := httptest.NewRecorder()
	h.installScript(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "/etc/systemd/system/node-agent-client.service") {
		t.Error("script does not check for the old hand-written systemd unit")
	}
	if !strings.Contains(body, "/opt/openvox-console/bin/node-agent-client") {
		t.Error("script does not clean up the old unmanaged binary path")
	}
}

func TestInstallScriptWindows_SetsUpPackageInventoryReporting(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080", TransportAddr: "transport.example.com:7422"})

	req := httptest.NewRequest(http.MethodGet, "/packages/install.ps1", nil)
	rec := httptest.NewRecorder()
	h.installScriptWindows(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `PuppetLabs\facter\facts.d`) {
		t.Error("script does not set up the facts.d directory")
	}
	if !strings.Contains(body, "package_inventory.ps1") {
		t.Error("script does not write a package_inventory.ps1 external fact")
	}
	if !strings.Contains(body, "Get-Package") {
		t.Error("script's package-inventory fact does not use Get-Package")
	}
	if !strings.Contains(body, `"_puppet_inventory_1"`) {
		t.Error("script's package-inventory fact does not emit the _puppet_inventory_1 shape")
	}
}

func TestInstallScriptMacOS_SetsUpPackageInventoryReporting(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080", TransportAddr: "transport.example.com:7422"})

	req := httptest.NewRequest(http.MethodGet, "/packages/install-macos.sh", nil)
	rec := httptest.NewRecorder()
	h.installScriptMacOS(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "/opt/puppetlabs/facter/facts.d/package_inventory.sh") {
		t.Error("script does not write a package-inventory external fact")
	}
	if !strings.Contains(body, "pkgutil --pkgs") || !strings.Contains(body, "pkgutil --pkg-info") {
		t.Error("script's package-inventory fact does not enumerate packages via pkgutil")
	}
	if !strings.Contains(body, `"_puppet_inventory_1"`) {
		t.Error("script's package-inventory fact does not emit the _puppet_inventory_1 shape")
	}
}

func TestBinaryPath_KnownAndUnknownPlatforms(t *testing.T) {
	for _, p := range []struct{ goos, goarch string }{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"windows", "amd64"},
		{"darwin", "amd64"},
		{"darwin", "arm64"},
	} {
		if _, ok := binaryPath(p.goos, p.goarch); !ok {
			t.Errorf("binaryPath(%s, %s) = not ok, want ok", p.goos, p.goarch)
		}
	}

	// windows/arm64: no upstream openvoxagent package exists for it -
	// see agentdist.go's supportedPlatforms doc comment.
	if _, ok := binaryPath("windows", "arm64"); ok {
		t.Error("binaryPath(windows, arm64) = ok, want not ok (unsupported)")
	}
}

func TestAptSourceList_IsTrusted(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080"})

	req := httptest.NewRequest(http.MethodGet, "/packages/apt/node-agent-client.list", nil)
	rec := httptest.NewRecorder()
	h.aptSourceList(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "[trusted=yes]") {
		t.Errorf("apt source list = %q, want [trusted=yes] per design.md's signing decision", body)
	}
	if !strings.Contains(body, "https://console.example.com:8080/packages/apt") {
		t.Error("apt source list does not point at this console's own apt route")
	}
}

func TestYumRepoFile_IsUnsigned(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com:8080"})

	req := httptest.NewRequest(http.MethodGet, "/packages/yum/node-agent-client.repo", nil)
	rec := httptest.NewRecorder()
	h.yumRepoFile(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "gpgcheck=0") {
		t.Errorf("yum repo file = %q, want gpgcheck=0 per design.md's signing decision", body)
	}
	if !strings.Contains(body, "https://console.example.com:8080/packages/yum") {
		t.Error("yum repo file does not point at this console's own yum route")
	}
}

// requireRepo skips the test if this build has no embedded packages
// (i.e. `make agent-packages` wasn't run before `go test`) - the repo
// routes are still exercised for their "not built" 404 behavior by
// TestPackageRoutes_404WhenNotBuilt regardless.
func requireRepo(t *testing.T, h *Handlers) {
	t.Helper()
	if h.repo == nil {
		t.Skip("no packages embedded in this build (run `make agent-packages` first)")
	}
}

func TestAptPackages_MatchesManifest(t *testing.T) {
	h := NewHandlers(Config{})
	requireRepo(t, h)

	for arch, data := range h.repo.aptPackages {
		req := httptest.NewRequest(http.MethodGet, "/packages/apt/dists/stable/main/binary-"+arch+"/Packages", nil)
		req.SetPathValue("binarydir", "binary-"+arch)
		rec := httptest.NewRecorder()
		h.aptPackages(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("arch %s: status = %d, want 200", arch, rec.Code)
		}
		if rec.Body.String() != string(data) {
			t.Errorf("arch %s: served Packages content does not match generated content", arch)
		}
		if !strings.Contains(rec.Body.String(), "Package: node-agent-client") {
			t.Errorf("arch %s: Packages content missing the package stanza", arch)
		}
	}
}

func TestAptPackages_UnknownArch404s(t *testing.T) {
	h := NewHandlers(Config{})
	requireRepo(t, h)

	req := httptest.NewRequest(http.MethodGet, "/packages/apt/dists/stable/main/binary-mips/Packages", nil)
	req.SetPathValue("binarydir", "binary-mips")
	rec := httptest.NewRecorder()
	h.aptPackages(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for an unknown architecture", rec.Code)
	}
}

func TestAptPackageFile_ServesRealDebBytes(t *testing.T) {
	h := NewHandlers(Config{})
	requireRepo(t, h)

	for filename, data := range h.repo.debFiles {
		req := httptest.NewRequest(http.MethodGet, "/packages/apt/pool/main/n/node-agent-client/"+filename, nil)
		req.SetPathValue("file", filename)
		rec := httptest.NewRecorder()
		h.aptPackageFile(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", filename, rec.Code)
		}
		if rec.Body.Len() != len(data) {
			t.Errorf("%s: served %d bytes, want %d", filename, rec.Body.Len(), len(data))
		}
		// A real .deb is an ar archive - "!<arch>\n" magic.
		if !strings.HasPrefix(rec.Body.String(), "!<arch>\n") {
			t.Errorf("%s: served content is not a valid ar archive (.deb)", filename)
		}
	}
}

func TestYumPackageFile_ServesRealRpmBytes(t *testing.T) {
	h := NewHandlers(Config{})
	requireRepo(t, h)

	for filename, data := range h.repo.rpmFiles {
		req := httptest.NewRequest(http.MethodGet, "/packages/yum/"+filename, nil)
		req.SetPathValue("file", filename)
		rec := httptest.NewRecorder()
		h.yumPackageFile(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", filename, rec.Code)
		}
		if rec.Body.Len() != len(data) {
			t.Errorf("%s: served %d bytes, want %d", filename, rec.Body.Len(), len(data))
		}
		// A real .rpm starts with the lead magic 0xedabeedb.
		want := []byte{0xed, 0xab, 0xee, 0xdb}
		if !bytes.HasPrefix(rec.Body.Bytes(), want) {
			t.Errorf("%s: served content is not a valid rpm (bad magic)", filename)
		}
	}
}

func TestYumRepomdAndPrimary_ReferenceEachOther(t *testing.T) {
	h := NewHandlers(Config{})
	requireRepo(t, h)

	req := httptest.NewRequest(http.MethodGet, "/packages/yum/repodata/repomd.xml", nil)
	rec := httptest.NewRecorder()
	h.yumRepomd(rec, req)
	if !strings.Contains(rec.Body.String(), `location href="repodata/primary.xml.gz"`) {
		t.Error("repomd.xml does not reference primary.xml.gz")
	}

	req = httptest.NewRequest(http.MethodGet, "/packages/yum/repodata/primary.xml.gz", nil)
	rec = httptest.NewRecorder()
	h.yumPrimaryGz(rec, req)
	gz, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("primary.xml.gz is not valid gzip: %v", err)
	}
	primary, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("reading decompressed primary.xml: %v", err)
	}
	if !strings.Contains(string(primary), "<name>node-agent-client</name>") {
		t.Error("primary.xml does not describe the node-agent-client package")
	}
}

func TestPackageRoutes_404WhenNotBuilt(t *testing.T) {
	h := &Handlers{cfg: Config{}, repo: nil}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/packages/apt/dists/stable/Release", nil),
		httptest.NewRequest(http.MethodGet, "/packages/yum/repodata/repomd.xml", nil),
	} {
		rec := httptest.NewRecorder()
		if strings.Contains(req.URL.Path, "apt") {
			h.aptRelease(rec, req)
		} else {
			h.yumRepomd(rec, req)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 when no packages were built", req.URL.Path, rec.Code)
		}
	}
}

// renderScript runs one of the three install-script handlers with cfg
// and returns its body - the enrollment tests below all differ only in
// which platform's script they read, so the fetching is shared.
func renderScript(t *testing.T, cfg Config, platform string) string {
	t.Helper()
	h := NewHandlers(cfg)
	rec := httptest.NewRecorder()
	switch platform {
	case "linux":
		h.installScript(rec, httptest.NewRequest(http.MethodGet, "/packages/install.sh", nil))
	case "windows":
		h.installScriptWindows(rec, httptest.NewRequest(http.MethodGet, "/packages/install.ps1", nil))
	case "macos":
		h.installScriptMacOS(rec, httptest.NewRequest(http.MethodGet, "/packages/install-macos.sh", nil))
	default:
		t.Fatalf("unknown platform %q", platform)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status = %d, want 200", platform, rec.Code)
	}
	return rec.Body.String()
}

func enrollmentConfig(puppetServer string) Config {
	return Config{
		ConsoleBaseURL:   "https://console.example.com:8080",
		TransportAddr:    "transport.example.com:7422",
		PuppetServerAddr: puppetServer,
	}
}

// Every platform's script must enroll a node that has no certificate
// rather than telling the operator to go run puppet themselves - see
// specs/agent-distribution's "Enrolling a node that has no certificate
// yet" scenario.
func TestInstallScripts_EnrollWhenNoCertificate(t *testing.T) {
	for _, platform := range []string{"linux", "windows", "macos"} {
		t.Run(platform, func(t *testing.T) {
			body := renderScript(t, enrollmentConfig("puppet.example.com"), platform)

			if !strings.Contains(body, "ssl bootstrap") {
				t.Error("script does not enroll via 'puppet ssl bootstrap'")
			}
			if !strings.Contains(body, "waitforcert") {
				t.Error("script does not bound the wait for a signature")
			}
			if strings.Contains(body, "then re-run this script.") && strings.Contains(body, "agent -t' first to enroll") {
				t.Error("script still tells the operator to enroll the node themselves first")
			}
		})
	}
}

// The pending-signature branch has to name this console's own Nodes
// page (where certificates are actually signed), interpolated from the
// configured base URL rather than hardcoded.
func TestInstallScripts_PendingSignatureNamesConsoleNodesPage(t *testing.T) {
	for _, platform := range []string{"linux", "windows", "macos"} {
		t.Run(platform, func(t *testing.T) {
			body := renderScript(t, enrollmentConfig("puppet.example.com"), platform)

			if !strings.Contains(body, "has not been signed yet") {
				t.Error("script has no pending-signature branch")
			}
			if !strings.Contains(body, "/nodes.html") {
				t.Error("pending-signature branch does not point at the Nodes page")
			}
			if !strings.Contains(body, "https://console.example.com:8080") {
				t.Error("console URL is not interpolated from configuration")
			}
		})
	}
}

// A CA that can't be reached must be reported as that, not as a
// certificate waiting to be signed - the two are indistinguishable by
// exit code (see design.md in add-install-script-auto-enrollment).
func TestInstallScripts_DistinguishUnreachableCAFromPendingSignature(t *testing.T) {
	for _, platform := range []string{"linux", "windows", "macos"} {
		t.Run(platform, func(t *testing.T) {
			body := renderScript(t, enrollmentConfig("puppet.example.com"), platform)

			if !strings.Contains(body, "No more routes to ca") {
				t.Error("script does not detect a route-level CA failure")
			}
			if !strings.Contains(body, "not a certificate waiting to be signed") {
				t.Error("script does not distinguish an unreachable CA from a pending signature")
			}
		})
	}
}

// A configured node-facing address is written into the script; an
// unconfigured one leaves the node's own Puppet server setting alone
// rather than substituting a guess - see specs/agent-distribution's
// "Node-facing Puppet server address" requirement.
func TestInstallScripts_PuppetServerAddressSetAndUnset(t *testing.T) {
	for _, platform := range []string{"linux", "windows", "macos"} {
		t.Run(platform+"/set", func(t *testing.T) {
			body := renderScript(t, enrollmentConfig("puppet.example.com"), platform)
			if !strings.Contains(body, "puppet.example.com") {
				t.Error("script does not carry the configured node-facing Puppet server address")
			}
			if !strings.Contains(body, "config set server") {
				t.Error("script never points the node at the configured server")
			}
		})

		t.Run(platform+"/unset", func(t *testing.T) {
			body := renderScript(t, enrollmentConfig(""), platform)

			// The guard must still be present (so an unset address is a
			// runtime no-op), but no address may be baked in.
			if !strings.Contains(body, "config set server") {
				t.Error("script dropped the server-setting branch entirely when unset")
			}
			if strings.Contains(body, "puppet.example.com") {
				t.Error("script carries an address even though none is configured")
			}
		})
	}
}

// A node that already has a certificate must not be re-enrolled - the
// whole enrollment block stays behind the existing has-certificate
// check, which is what keeps re-running the script a no-op.
func TestInstallScripts_EnrollmentIsGuardedByExistingCertificate(t *testing.T) {
	for _, platform := range []string{"linux", "windows", "macos"} {
		t.Run(platform, func(t *testing.T) {
			body := renderScript(t, enrollmentConfig("puppet.example.com"), platform)

			certCheck := strings.Index(body, "CERT_FILE")
			if platform == "windows" {
				certCheck = strings.Index(body, "CertFile")
			}
			bootstrap := strings.Index(body, "ssl bootstrap")
			if certCheck < 0 || bootstrap < 0 {
				t.Fatal("script is missing the certificate check or the enrollment call")
			}
			if certCheck > bootstrap {
				t.Error("enrollment is not guarded by the existing-certificate check")
			}
		})
	}
}
