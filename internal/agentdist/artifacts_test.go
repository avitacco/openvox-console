package agentdist

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func testCatalog(buildsJSON string, manifest []manifestEntry) *Catalog {
	return newCatalog([]byte(buildsJSON), manifest, func(platform string) []byte {
		return []byte("client for " + platform)
	})
}

func TestCatalog_StampedBuild(t *testing.T) {
	digest := sha256hex([]byte("client for linux/amd64"))
	c := testCatalog(`{"linux/amd64":{"digest":"`+digest+`","buildTime":"2026-11-11T09:30:00Z","version":"v1"}}`, nil)

	b, ok := c.Build("linux", "amd64")
	if !ok || b.Digest != digest || b.Version != "v1" || !b.BuildTime.Equal(time.Date(2026, 11, 11, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("Build(linux, amd64) = %+v, %v", b, ok)
	}
	if !c.Stamped() {
		t.Error("Stamped() = false for a catalog with a stamped build")
	}
}

func TestCatalog_StaleBuildsFileIsNotTrusted(t *testing.T) {
	// builds.json describes a different binary than the one embedded:
	// the digest comes from the bytes, and the stamp is dropped.
	c := testCatalog(`{"linux/amd64":{"digest":"0000","buildTime":"2026-11-11T09:30:00Z","version":"v1"}}`, nil)
	b, _ := c.Build("linux", "amd64")
	if b.Digest != sha256hex([]byte("client for linux/amd64")) || !b.BuildTime.IsZero() {
		t.Errorf("Build = %+v, want the embedded digest with no build time", b)
	}
	if c.Stamped() {
		t.Error("Stamped() = true with no usable stamp")
	}
}

func TestCatalog_Artifacts(t *testing.T) {
	c := testCatalog("", []manifestEntry{
		{Format: "deb", Arch: "amd64", GoArch: "amd64", Filename: "c_1_amd64.deb", SHA256: "aa", Size: 10},
		{Format: "rpm", Arch: "aarch64", GoArch: "arm64", Filename: "c-1.aarch64.rpm", SHA256: "bb", Size: 20},
	})

	a, ok := c.Artifact("linux", "amd64", "deb")
	if !ok || a.Path != "/packages/apt/pool/main/n/node-agent-client/c_1_amd64.deb" || a.SHA256 != "aa" || a.Size != 10 {
		t.Errorf("deb artifact = %+v, %v", a, ok)
	}
	a, ok = c.Artifact("linux", "arm64", "rpm")
	if !ok || a.Path != "/packages/yum/c-1.aarch64.rpm" || a.SHA256 != "bb" {
		t.Errorf("rpm artifact = %+v, %v", a, ok)
	}
	a, ok = c.Artifact("windows", "amd64", "binary")
	if !ok || a.Path != "/packages/node-agent-client?os=windows&arch=amd64" || a.SHA256 != sha256hex([]byte("client for windows/amd64")) {
		t.Errorf("windows artifact = %+v, %v", a, ok)
	}

	for _, c2 := range []struct{ goos, goarch, method string }{
		{"linux", "arm64", "deb"},     // no such package built
		{"linux", "amd64", "binary"},  // script-managed Linux is not upgraded in place
		{"linux", "amd64", "unknown"}, // undetermined install method
		{"darwin", "arm64", "deb"},
		{"plan9", "amd64", "binary"},
	} {
		if _, ok := c.Artifact(c2.goos, c2.goarch, c2.method); ok {
			t.Errorf("Artifact(%s, %s, %s) offered an artifact", c2.goos, c2.goarch, c2.method)
		}
	}
}

func TestCatalog_PackagesNotBuilt(t *testing.T) {
	c := testCatalog("", nil)
	if _, ok := c.Artifact("linux", "amd64", "deb"); ok {
		t.Error("offered a deb artifact with no packages built")
	}
	if _, ok := c.Artifact("darwin", "arm64", "binary"); !ok {
		t.Error("a console without packages should still offer the macOS binary")
	}
}

// The embedded artifacts, fetched over the real routes, must be the
// bytes the catalog describes - that is what a client verifies.
func TestCatalog_ServedArtifactsMatch(t *testing.T) {
	mux := http.NewServeMux()
	NewHandlers(Config{}).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := LoadCatalog()
	check := func(goos, goarch, method string) {
		a, ok := c.Artifact(goos, goarch, method)
		if !ok {
			t.Logf("no %s artifact for %s/%s in this build; skipped", method, goos, goarch)
			return
		}
		resp, err := http.Get(srv.URL + a.Path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body := make([]byte, 0, a.Size)
		buf := make([]byte, 1<<16)
		for {
			n, err := resp.Body.Read(buf)
			body = append(body, buf[:n]...)
			if err != nil {
				break
			}
		}
		if resp.StatusCode != http.StatusOK || int64(len(body)) != a.Size || sha256hex(body) != a.SHA256 {
			t.Errorf("%s %s/%s: status %d, %d bytes, digest %s; catalog says %d bytes, %s",
				method, goos, goarch, resp.StatusCode, len(body), sha256hex(body), a.Size, a.SHA256)
		}
	}
	check("linux", "amd64", "deb")
	check("linux", "arm64", "rpm")
	check("windows", "amd64", "binary")
	check("darwin", "arm64", "binary")
}

// A client that upgrades itself exits and relies on its service manager
// to start the new binary. Each platform's installer has to arrange
// that.
func TestInstallers_RestartTheClientAfterItExits(t *testing.T) {
	h := NewHandlers(Config{ConsoleBaseURL: "https://console.example.com", TransportAddr: "t.example.com:7422"})
	render := func(path string, handler http.HandlerFunc) string {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Body.String()
	}

	win := render("/packages/install.ps1", h.installScriptWindows)
	failure := strings.Index(win, "sc.exe failure $ServiceName reset= 86400 actions= restart/")
	if failure < 0 {
		t.Error("the Windows script sets no restart-on-failure actions")
	}
	// Before the start, and outside the new-service branch, so an
	// already registered service gets them as well.
	if start := strings.LastIndex(win, "Start-Service -Name $ServiceName"); start < failure {
		t.Error("the Windows script starts the service before setting its restart actions")
	}
	if strings.Count(win, "Start-Service -Name $ServiceName") != 1 {
		t.Error("the Windows script should start the service in one place, after the restart actions")
	}

	mac := render("/packages/install-macos.sh", h.installScriptMacOS)
	if !strings.Contains(mac, "<key>KeepAlive</key>\n  <true/>") {
		t.Error("the macOS launchd job is not unconditionally kept alive, so a clean exit would not restart it")
	}

	unit, err := os.ReadFile("pkgassets/node-agent-client.service")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Restart=always", "EnvironmentFile=-/etc/node-agent-client/local.env"} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("the systemd unit lacks %q", want)
		}
	}
}

func TestYumMetadata_CarriesEpochAndRelease(t *testing.T) {
	r := &packageRepo{rpmFiles: map[string][]byte{}}
	err := r.buildYumMetadata([]manifestEntry{
		{Format: "rpm", Arch: "x86_64", Version: "0.1.0", Epoch: "1", Release: "20261111093000.abc", Filename: "new.rpm"},
		{Format: "rpm", Arch: "aarch64", Version: "bbba663", Filename: "old.rpm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(r.yumPrimaryGz))
	if err != nil {
		t.Fatal(err)
	}
	primary, _ := io.ReadAll(zr)
	for _, want := range []string{
		`<version epoch="1" ver="0.1.0" rel="20261111093000.abc"/>`,
		`node-agent-client-0.1.0-20261111093000.abc.src.rpm`,
		// A manifest from before these fields existed keeps its meaning.
		`<version epoch="0" ver="bbba663" rel="1"/>`,
	} {
		if !strings.Contains(string(primary), want) {
			t.Errorf("primary.xml lacks %s", want)
		}
	}
}
