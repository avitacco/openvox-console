package nodeagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const aptSimulation = `NOTE: This is only a simulation!
Reading package lists...
The following packages will be upgraded:
  libssl3 curl
Inst libssl3 [3.0.2-0ubuntu1.10] (3.0.2-0ubuntu1.12 Ubuntu:22.04/jammy-updates, Ubuntu:22.04/jammy-security [amd64])
Inst curl [7.81.0-1ubuntu1.14] (7.81.0-1ubuntu1.15 Ubuntu:22.04/jammy-updates [amd64])
Inst linux-image-6.8.0-50 (6.8.0-50.51 Ubuntu:24.04/noble-updates [amd64])
Conf libssl3 (3.0.2-0ubuntu1.12 Ubuntu:22.04/jammy-updates, Ubuntu:22.04/jammy-security [amd64])
`

func TestParseAptSimulation(t *testing.T) {
	got := parseAptSimulation(aptSimulation)
	want := []patchUpdate{
		{Name: "libssl3", Installed: "3.0.2-0ubuntu1.10", Available: "3.0.2-0ubuntu1.12", Security: true},
		{Name: "curl", Installed: "7.81.0-1ubuntu1.14", Available: "7.81.0-1ubuntu1.15"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("update %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseAptSimulation_NothingPending(t *testing.T) {
	got := parseAptSimulation("NOTE: This is only a simulation!\n0 upgraded, 0 newly installed\n")
	if got == nil || len(got) != 0 {
		t.Errorf("want an empty, non-nil list, got %#v", got)
	}
}

const dnfCheck = `
openssl.x86_64                 1:3.0.7-25.el9_3      baseos
kernel-core.x86_64             5.14.0-362.el9        baseos

Obsoleting Packages
foo.x86_64                     2.0-1                 appstream
`

func TestParseRPMCheckUpdate(t *testing.T) {
	got := parseRPMCheckUpdate(dnfCheck)
	if len(got) != 2 || got[0].Name != "openssl" || got[0].Available != "1:3.0.7-25.el9_3" || got[1].Name != "kernel-core" {
		t.Errorf("got %+v", got)
	}
}

func TestParseRPMSecurityNames(t *testing.T) {
	out := "ALSA-2024:1 Moderate/Sec.  openssl-1:3.0.7-25.el9_3.x86_64\nnot a row\n"
	names := parseRPMSecurityNames(out)
	if _, ok := names["openssl"]; !ok {
		t.Errorf("got %v", names)
	}
}

func scanResult(t *testing.T, raw []byte) *wireResponse {
	t.Helper()
	var w wireResponse
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	return &w
}

func has(paths ...string) func(string) bool {
	return func(p string) bool {
		for _, x := range paths {
			if x == p {
				return true
			}
		}
		return false
	}
}

func TestPatchScan_Apt(t *testing.T) {
	var ran []string
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		ran = append(ran, name+" "+strings.Join(args, " "))
		if len(args) > 0 && args[0] == "update" {
			return "", "", 0, nil
		}
		return aptSimulation, "", 0, nil
	}
	w := scanResult(t, handlePatchScan(context.Background(), runner, has(aptGet, "/var/run/reboot-required"), nil))
	if w.Error != "" || w.Result == nil || w.Result.PatchScan == nil {
		t.Fatalf("got %+v", w)
	}
	ps := w.Result.PatchScan
	if ps.Manager != "apt" || !ps.RebootRequired || len(ps.Updates) != 2 {
		t.Errorf("got %+v", ps)
	}
	if len(ran) != 2 || !strings.Contains(ran[1], "-s") {
		t.Errorf("the upgrade must only be simulated, ran %v", ran)
	}
}

func TestPatchScan_RefreshCanBeSkipped(t *testing.T) {
	var ran []string
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		ran = append(ran, strings.Join(args, " "))
		return "", "", 0, nil
	}
	f := false
	params, _ := json.Marshal(patchScanParams{Refresh: &f})
	handlePatchScan(context.Background(), runner, has(aptGet), params)
	if len(ran) != 1 || strings.HasPrefix(ran[0], "update") {
		t.Errorf("ran %v", ran)
	}
}

func TestPatchScan_AptUpdateFailure(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		return "", "E: Failed to fetch http://mirror\nmore", 100, nil
	}
	w := scanResult(t, handlePatchScan(context.Background(), runner, has(aptGet), nil))
	if !strings.Contains(w.Error, "apt-get update failed") || !strings.Contains(w.Error, "Failed to fetch") || w.Result != nil {
		t.Errorf("got %+v", w)
	}
}

func TestPatchScan_DNF(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		switch strings.Join(args, " ") {
		case "-q check-update --refresh":
			return dnfCheck, "", 100, nil
		case "-q updateinfo list security":
			return "ALSA-2024:1 Moderate/Sec.  openssl-1:3.0.7-25.el9_3.x86_64\n", "", 0, nil
		case "-r":
			return "", "", 1, nil
		}
		t.Fatalf("unexpected %s %v", name, args)
		return "", "", 0, nil
	}
	w := scanResult(t, handlePatchScan(context.Background(), runner, has(dnfBin, "/usr/bin/needs-restarting"), nil))
	ps := w.Result.PatchScan
	if ps.Manager != "dnf" || !ps.RebootRequired || len(ps.Updates) != 2 || !ps.Updates[0].Security || ps.Updates[1].Security {
		t.Errorf("got %+v", ps)
	}
}

func TestPatchScan_DNFNothingPending(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		return "", "", 0, nil
	}
	w := scanResult(t, handlePatchScan(context.Background(), runner, has(dnfBin), nil))
	if ps := w.Result.PatchScan; ps == nil || ps.Updates == nil || len(ps.Updates) != 0 {
		t.Errorf("got %+v", w)
	}
}

func TestPatchScan_DNFFailure(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		return "", "Cannot download repomd.xml", 1, nil
	}
	w := scanResult(t, handlePatchScan(context.Background(), runner, has(dnfBin), nil))
	if !strings.Contains(w.Error, "check-update failed") {
		t.Errorf("got %+v", w)
	}
}

func TestPatchScan_UnsupportedSystem(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		t.Fatal("nothing should run")
		return "", "", 0, nil
	}
	w := scanResult(t, handlePatchScan(context.Background(), runner, has(), nil))
	if w.Result == nil || !w.Result.PatchScan.Unsupported || len(w.Result.PatchScan.Updates) != 0 {
		t.Errorf("got %+v", w)
	}
}

func TestHandler_RoutesPatchScan(t *testing.T) {
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		return "", "", 0, nil
	}
	h := NewHandler(runner, "puppet", t.TempDir(), has(aptGet))
	raw := h(context.Background(), []byte(`{"module":"puppet","action":"patch_scan"}`))
	if w := scanResult(t, raw); w.Result == nil || w.Result.PatchScan == nil || w.Result.PatchScan.Manager != "apt" {
		t.Errorf("got %s", raw)
	}
}

// An agent built before patch_scan answers `unsupported action`, which is
// the exact text the console treats as "cannot report patches".
func TestHandler_UnknownActionWording(t *testing.T) {
	h := NewHandler(func(context.Context, string, ...string) (string, string, int, error) { return "", "", 0, nil }, "puppet", t.TempDir(), has())
	raw := h(context.Background(), []byte(`{"module":"puppet","action":"patch_scan_v2"}`))
	if w := scanResult(t, raw); w.Error != `unsupported action "patch_scan_v2"` {
		t.Errorf("got %s", raw)
	}
}
