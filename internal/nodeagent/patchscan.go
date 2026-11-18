package nodeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// actionPatchScan asks the node which updates its package manager has
// pending. It is read-only: nothing is installed, upgraded or removed.
const actionPatchScan = "patch_scan"

// patchScanParams is requestData.Params' shape for actionPatchScan.
// Refresh (default true) refreshes the package lists first, because
// otherwise the answer is only as fresh as the last refresh.
type patchScanParams struct {
	Refresh *bool `json:"refresh,omitempty"`
}

// patchUpdate is one pending update. Installed is empty when the package
// manager's output does not say.
type patchUpdate struct {
	Name      string `json:"name"`
	Installed string `json:"installed,omitempty"`
	Available string `json:"available"`
	Security  bool   `json:"security"`
}

// patchScanResponse mirrors the console's copy. Unsupported is set when
// the node has no package manager the scan knows; that is a different
// answer from "nothing is pending".
type patchScanResponse struct {
	Manager        string        `json:"manager,omitempty"`
	Unsupported    bool          `json:"unsupported,omitempty"`
	RebootRequired bool          `json:"reboot_required"`
	Updates        []patchUpdate `json:"updates"`
}

const (
	aptGet = "/usr/bin/apt-get"
	dnfBin = "/usr/bin/dnf"
	yumBin = "/usr/bin/yum"
)

func handlePatchScan(ctx context.Context, runner CommandRunner, fileExists func(string) bool, params json.RawMessage) []byte {
	var p patchScanParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return errorResponse(fmt.Sprintf("invalid patch scan params: %v", err))
		}
	}
	refresh := p.Refresh == nil || *p.Refresh

	var (
		res *patchScanResponse
		err error
	)
	switch {
	case fileExists(aptGet):
		res, err = scanApt(ctx, runner, fileExists, refresh)
	case fileExists(dnfBin):
		res, err = scanRPM(ctx, runner, fileExists, dnfBin, "dnf", refresh)
	case fileExists(yumBin):
		res, err = scanRPM(ctx, runner, fileExists, yumBin, "yum", refresh)
	default:
		res = &patchScanResponse{Unsupported: true, Updates: []patchUpdate{}}
	}
	if err != nil {
		return errorResponse(err.Error())
	}
	return encodeResult(&responseData{PatchScan: res})
}

func scanApt(ctx context.Context, runner CommandRunner, fileExists func(string) bool, refresh bool) (*patchScanResponse, error) {
	if refresh {
		if _, stderr, code, err := runner(ctx, aptGet, "update", "-q"); err != nil {
			return nil, fmt.Errorf("apt-get update could not run: %v", err)
		} else if code != 0 {
			return nil, fmt.Errorf("apt-get update failed (exit %d): %s", code, firstLine(stderr))
		}
	}
	// A simulation: -s never changes the system.
	stdout, stderr, code, err := runner(ctx, aptGet, "-s", "-q", "dist-upgrade")
	if err != nil {
		return nil, fmt.Errorf("apt-get could not run: %v", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("apt-get simulation failed (exit %d): %s", code, firstLine(stderr))
	}
	return &patchScanResponse{
		Manager:        "apt",
		RebootRequired: fileExists("/var/run/reboot-required"),
		Updates:        parseAptSimulation(stdout),
	}, nil
}

// Inst libssl3 [3.0.2-0ubuntu1] (3.0.2-0ubuntu1.1 Ubuntu:22.04/jammy-updates, Ubuntu:22.04/jammy-security [amd64])
var aptInst = regexp.MustCompile(`^Inst (\S+) \[([^\]]+)\] \((\S+) ([^\[)]*)`)

// parseAptSimulation reads the upgrades out of `apt-get -s` output.
// Lines for packages that would be newly installed have no installed
// version and are not updates, so they are skipped.
func parseAptSimulation(out string) []patchUpdate {
	updates := []patchUpdate{}
	for _, line := range strings.Split(out, "\n") {
		m := aptInst.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		updates = append(updates, patchUpdate{
			Name:      m[1],
			Installed: m[2],
			Available: m[3],
			Security:  strings.Contains(strings.ToLower(m[4]), "security"),
		})
	}
	return updates
}

func scanRPM(ctx context.Context, runner CommandRunner, fileExists func(string) bool, bin, name string, refresh bool) (*patchScanResponse, error) {
	args := []string{"-q", "check-update"}
	if refresh {
		args = append(args, "--refresh")
	}
	stdout, stderr, code, err := runner(ctx, bin, args...)
	if err != nil {
		return nil, fmt.Errorf("%s could not run: %v", name, err)
	}
	// 0 means nothing pending, 100 means updates are available.
	if code != 0 && code != 100 {
		return nil, fmt.Errorf("%s check-update failed (exit %d): %s", name, code, firstLine(stderr))
	}
	updates := parseRPMCheckUpdate(stdout)

	// Security metadata is best effort: a repository without it leaves
	// every update unflagged, and the scan still succeeds.
	if secOut, _, secCode, secErr := runner(ctx, bin, "-q", "updateinfo", "list", "security"); secErr == nil && secCode == 0 {
		flagRPMSecurity(updates, parseRPMSecurityNames(secOut))
	}

	reboot := false
	if fileExists("/usr/bin/needs-restarting") {
		// Exit 1 means a reboot is required.
		if _, _, c, e := runner(ctx, "/usr/bin/needs-restarting", "-r"); e == nil && c == 1 {
			reboot = true
		}
	}
	return &patchScanResponse{Manager: name, RebootRequired: reboot, Updates: updates}, nil
}

// parseRPMCheckUpdate reads `check-update` rows (name.arch version repo),
// stopping at the "Obsoleting Packages" section.
func parseRPMCheckUpdate(out string) []patchUpdate {
	updates := []patchUpdate{}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Obsoleting") {
			break
		}
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		dot := strings.LastIndex(f[0], ".")
		if dot <= 0 || strings.ContainsAny(f[1], "/") {
			continue
		}
		updates = append(updates, patchUpdate{Name: f[0][:dot], Available: f[1]})
	}
	return updates
}

// parseRPMSecurityNames returns the package names `updateinfo list
// security` mentions, with the available version, as name-version.
// A row is `ALSA-2024:1 Moderate/Sec. openssl-3.0.7-25.el9.x86_64`.
func parseRPMSecurityNames(out string) map[string]string {
	names := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		nevra := f[2]
		if dot := strings.LastIndex(nevra, "."); dot > 0 {
			nevra = nevra[:dot] // drop the architecture
		}
		rel := strings.LastIndex(nevra, "-")
		if rel <= 0 {
			continue
		}
		ver := strings.LastIndex(nevra[:rel], "-")
		if ver <= 0 {
			continue
		}
		names[nevra[:ver]] = nevra[ver+1:]
	}
	return names
}

func flagRPMSecurity(updates []patchUpdate, security map[string]string) {
	for i := range updates {
		if _, ok := security[updates[i].Name]; ok {
			updates[i].Security = true
		}
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
