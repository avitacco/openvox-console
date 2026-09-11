package agentdist

import (
	"fmt"
	"net/http"
	"strings"
	"text/template"
)

// Config configures the served install script's references back to
// this console.
type Config struct {
	// ConsoleBaseURL is this console's own externally-reachable base
	// URL (e.g. "https://console.example.com:8080"), used inside the
	// generated install script to fetch the node-agent-client binary
	// back from this same console.
	ConsoleBaseURL string
	// TransportAddr is the node transport's externally-reachable
	// "host:port" that the installed node-agent-client should connect
	// to.
	TransportAddr string
	// PuppetServerAddr is the Puppet server a node should enroll
	// against, written into the generated install script. Empty when
	// the console has none configured, in which case the script leaves
	// the node's own Puppet server configuration untouched rather than
	// guessing - see design.md in add-install-script-auto-enrollment.
	PuppetServerAddr string
}

// Handlers serves the agent-distribution HTTP routes.
type Handlers struct {
	cfg  Config
	repo *packageRepo // nil if `make agent-packages` was never run for this build
}

// NewHandlers builds Handlers backed by cfg. Panics if the embedded
// packages/manifest.json exists but is malformed - that's a build-time
// packaging bug, not a runtime condition to degrade gracefully around
// (unlike a missing manifest, which just means `make agent-packages`
// wasn't run and the apt/yum routes serve 404s - see buildPackageRepo).
func NewHandlers(cfg Config) *Handlers {
	repo, err := buildPackageRepo()
	if err != nil {
		panic(fmt.Sprintf("agentdist: embedded packages/manifest.json is malformed: %v", err))
	}
	return &Handlers{cfg: cfg, repo: repo}
}

// Register wires this package's routes onto mux. Deliberately
// unauthenticated - like codemanager's webhook endpoint, a node
// running `curl | bash` (or fetching its own platform's binary) has no
// console user token to present, so there's no permission wrapper to
// apply here.
func (h *Handlers) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /packages/node-agent-client", h.downloadBinary)
	mux.HandleFunc("GET /packages/install.sh", h.installScript)
	mux.HandleFunc("GET /packages/install.ps1", h.installScriptWindows)
	mux.HandleFunc("GET /packages/install-macos.sh", h.installScriptMacOS)

	// Self-hosted apt/yum repository for node-agent-client (Linux only
	// - see design.md in add-native-agent-packaging for why Windows and
	// macOS stay on the raw-binary route above). Unsigned/trusted, like
	// the rest of this unauthenticated package-serving surface - see
	// that design.md's signing decision.
	mux.HandleFunc("GET /packages/apt/node-agent-client.list", h.aptSourceList)
	mux.HandleFunc("GET /packages/apt/dists/stable/Release", h.aptRelease)
	mux.HandleFunc("GET /packages/apt/dists/stable/main/{binarydir}/Packages", h.aptPackages)
	mux.HandleFunc("GET /packages/apt/dists/stable/main/{binarydir}/Packages.gz", h.aptPackagesGz)
	mux.HandleFunc("GET /packages/apt/pool/main/n/node-agent-client/{file}", h.aptPackageFile)

	mux.HandleFunc("GET /packages/yum/node-agent-client.repo", h.yumRepoFile)
	mux.HandleFunc("GET /packages/yum/repodata/repomd.xml", h.yumRepomd)
	mux.HandleFunc("GET /packages/yum/repodata/primary.xml.gz", h.yumPrimaryGz)
	mux.HandleFunc("GET /packages/yum/{file}", h.yumPackageFile)
}

func (h *Handlers) aptSourceList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "deb [trusted=yes] %s/packages/apt stable main\n", h.cfg.ConsoleBaseURL)
}

func (h *Handlers) yumRepoFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "[openvox-console]\nname=OpenVox Console node-agent-client\nbaseurl=%s/packages/yum\nenabled=1\ngpgcheck=0\n", h.cfg.ConsoleBaseURL)
}

func (h *Handlers) repoNotBuilt(w http.ResponseWriter) bool {
	if h.repo == nil {
		http.Error(w, "no packages built for this console (make agent-packages was not run)", http.StatusNotFound)
		return true
	}
	return false
}

func (h *Handlers) aptRelease(w http.ResponseWriter, r *http.Request) {
	if h.repoNotBuilt(w) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(h.repo.aptRelease)
}

func (h *Handlers) aptPackages(w http.ResponseWriter, r *http.Request) {
	if h.repoNotBuilt(w) {
		return
	}
	data, ok := h.repo.aptPackages[strings.TrimPrefix(r.PathValue("binarydir"), "binary-")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}

func (h *Handlers) aptPackagesGz(w http.ResponseWriter, r *http.Request) {
	if h.repoNotBuilt(w) {
		return
	}
	data, ok := h.repo.aptPackagesGz[strings.TrimPrefix(r.PathValue("binarydir"), "binary-")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	_, _ = w.Write(data)
}

func (h *Handlers) aptPackageFile(w http.ResponseWriter, r *http.Request) {
	if h.repoNotBuilt(w) {
		return
	}
	data, ok := h.repo.debFiles[r.PathValue("file")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.debian.binary-package")
	_, _ = w.Write(data)
}

func (h *Handlers) yumRepomd(w http.ResponseWriter, r *http.Request) {
	if h.repoNotBuilt(w) {
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write(h.repo.yumRepomd)
}

func (h *Handlers) yumPrimaryGz(w http.ResponseWriter, r *http.Request) {
	if h.repoNotBuilt(w) {
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	_, _ = w.Write(h.repo.yumPrimaryGz)
}

func (h *Handlers) yumPackageFile(w http.ResponseWriter, r *http.Request) {
	if h.repoNotBuilt(w) {
		return
	}
	data, ok := h.repo.rpmFiles[r.PathValue("file")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/x-rpm")
	_, _ = w.Write(data)
}

func (h *Handlers) downloadBinary(w http.ResponseWriter, r *http.Request) {
	goos := r.URL.Query().Get("os")
	goarch := r.URL.Query().Get("arch")

	path, ok := binaryPath(goos, goarch)
	if !ok {
		http.Error(w, fmt.Sprintf("unsupported platform: os=%q arch=%q", goos, goarch), http.StatusBadRequest)
		return
	}

	data, err := binFS.ReadFile(path)
	if err != nil {
		http.Error(w, "node-agent-client binary not available for this platform", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="node-agent-client"`)
	_, _ = w.Write(data)
}

var installTmpl = template.Must(template.New("install.sh").Parse(installScriptTemplate))

func (h *Handlers) installScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_ = installTmpl.Execute(w, h.cfg)
}

func (h *Handlers) installScriptWindows(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_ = installTmplWindows.Execute(w, h.cfg)
}

func (h *Handlers) installScriptMacOS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_ = installTmplMacOS.Execute(w, h.cfg)
}
