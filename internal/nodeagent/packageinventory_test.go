package nodeagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fileExistsAt(existing ...string) func(string) bool {
	set := make(map[string]bool, len(existing))
	for _, p := range existing {
		set[p] = true
	}
	return func(p string) bool { return set[p] }
}

func TestDetectPackageManagerFamily(t *testing.T) {
	cases := []struct {
		name       string
		fileExists func(string) bool
		want       packageManagerFamily
	}{
		{"apt present", fileExistsAt("/usr/bin/dpkg-query"), familyAPT},
		{"rpm present", fileExistsAt("/usr/bin/rpm"), familyRPM},
		{"both present prefers apt", fileExistsAt("/usr/bin/dpkg-query", "/usr/bin/rpm"), familyAPT},
		{"neither present", fileExistsAt(), familyUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectPackageManagerFamily(tc.fileExists); got != tc.want {
				t.Errorf("detectPackageManagerFamily() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandler_PackageInventoryStatus_Absent(t *testing.T) {
	factsDir := t.TempDir()
	handler := NewHandler(fakeRunner("", "", 0, nil), "puppet", factsDir, noFileExists)

	req := requestPayload(t, "puppet", actionPackageInventoryStatus, nil)
	wr := decodeWireResponse(t, handler(context.Background(), req))

	if wr.Error != "" {
		t.Fatalf("Error = %q, want empty", wr.Error)
	}
	if wr.Result == nil || wr.Result.PackageInventory == nil {
		t.Fatal("PackageInventory is nil, want a result")
	}
	if wr.Result.PackageInventory.Enabled {
		t.Error("Enabled = true, want false (fact file was never written)")
	}
}

func TestHandler_PackageInventoryStatus_Present(t *testing.T) {
	factsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(factsDir, packageInventoryFactFilename), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed fact file: %v", err)
	}
	handler := NewHandler(fakeRunner("", "", 0, nil), "puppet", factsDir, noFileExists)

	req := requestPayload(t, "puppet", actionPackageInventoryStatus, nil)
	wr := decodeWireResponse(t, handler(context.Background(), req))

	if wr.Result == nil || wr.Result.PackageInventory == nil {
		t.Fatal("PackageInventory is nil, want a result")
	}
	if !wr.Result.PackageInventory.Enabled {
		t.Error("Enabled = false, want true (fact file exists)")
	}
}

func TestHandler_PackageInventoryStatus_NoSideEffects(t *testing.T) {
	factsDir := t.TempDir()
	var ranPuppet bool
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		ranPuppet = true
		return "", "", 0, nil
	}
	handler := NewHandler(runner, "puppet", factsDir, noFileExists)

	req := requestPayload(t, "puppet", actionPackageInventoryStatus, nil)
	handler(context.Background(), req)

	if ranPuppet {
		t.Error("a status request ran puppet - it should have no side effects")
	}
	if _, err := os.Stat(filepath.Join(factsDir, packageInventoryFactFilename)); !os.IsNotExist(err) {
		t.Errorf("a status request created the fact file - it should have no side effects (stat err: %v)", err)
	}
}

func TestHandler_PackageInventorySet_EnableFromAbsent_Apt(t *testing.T) {
	factsDir := t.TempDir()
	var invoked []string
	handler := NewHandler(recordingRunner(&invoked, "changes applied", 2), "puppet", factsDir, fileExistsAt("/usr/bin/dpkg-query"))

	req := requestPayload(t, "puppet", actionPackageInventorySet, packageInventorySetParams{Enabled: true})
	wr := decodeWireResponse(t, handler(context.Background(), req))

	if wr.Error != "" {
		t.Fatalf("Error = %q, want empty", wr.Error)
	}
	if wr.Result == nil || wr.Result.PackageInventory == nil || !wr.Result.PackageInventory.Enabled {
		t.Fatalf("Result = %+v, want PackageInventory.Enabled = true", wr.Result)
	}
	if !wr.Result.PackageInventory.Ran {
		t.Error("Ran = false, want true - a real state change should trigger a Puppet run")
	}
	if len(invoked) == 0 {
		t.Fatal("puppet was not invoked, want a real run after enabling")
	}
	content, err := os.ReadFile(filepath.Join(factsDir, packageInventoryFactFilename))
	if err != nil {
		t.Fatalf("fact file was not written: %v", err)
	}
	if !strings.Contains(string(content), "dpkg-query") {
		t.Errorf("fact file content = %q, want the apt (dpkg-query) variant", content)
	}
}

func TestHandler_PackageInventorySet_EnableFromAbsent_Rpm(t *testing.T) {
	factsDir := t.TempDir()
	handler := NewHandler(fakeRunner("", "", 0, nil), "puppet", factsDir, fileExistsAt("/usr/bin/rpm"))

	req := requestPayload(t, "puppet", actionPackageInventorySet, packageInventorySetParams{Enabled: true})
	handler(context.Background(), req)

	content, err := os.ReadFile(filepath.Join(factsDir, packageInventoryFactFilename))
	if err != nil {
		t.Fatalf("fact file was not written: %v", err)
	}
	if !strings.Contains(string(content), "rpm -qa") {
		t.Errorf("fact file content = %q, want the rpm variant", content)
	}
}

func TestHandler_PackageInventorySet_EnableWithNeitherFamily(t *testing.T) {
	factsDir := t.TempDir()
	var ranPuppet bool
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		ranPuppet = true
		return "", "", 0, nil
	}
	handler := NewHandler(runner, "puppet", factsDir, noFileExists)

	req := requestPayload(t, "puppet", actionPackageInventorySet, packageInventorySetParams{Enabled: true})
	wr := decodeWireResponse(t, handler(context.Background(), req))

	if wr.Error == "" {
		t.Fatal("Error is empty, want a failure - no supported package manager found")
	}
	if ranPuppet {
		t.Error("puppet was invoked despite the fact write failing")
	}
	if _, err := os.Stat(filepath.Join(factsDir, packageInventoryFactFilename)); !os.IsNotExist(err) {
		t.Error("fact file was created despite no supported package manager - want no write")
	}
}

func TestHandler_PackageInventorySet_DisableFromPresent(t *testing.T) {
	factsDir := t.TempDir()
	factPath := filepath.Join(factsDir, packageInventoryFactFilename)
	if err := os.WriteFile(factPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("seed fact file: %v", err)
	}
	var invoked []string
	handler := NewHandler(recordingRunner(&invoked, "", 0), "puppet", factsDir, noFileExists)

	req := requestPayload(t, "puppet", actionPackageInventorySet, packageInventorySetParams{Enabled: false})
	wr := decodeWireResponse(t, handler(context.Background(), req))

	if wr.Error != "" {
		t.Fatalf("Error = %q, want empty", wr.Error)
	}
	if wr.Result == nil || wr.Result.PackageInventory == nil || wr.Result.PackageInventory.Enabled {
		t.Fatalf("Result = %+v, want PackageInventory.Enabled = false", wr.Result)
	}
	if !wr.Result.PackageInventory.Ran {
		t.Error("Ran = false, want true - a real state change should trigger a Puppet run")
	}
	if len(invoked) == 0 {
		t.Fatal("puppet was not invoked, want a real run after disabling")
	}
	if _, err := os.Stat(factPath); !os.IsNotExist(err) {
		t.Errorf("fact file still exists after disabling (stat err: %v)", err)
	}
}

func TestHandler_PackageInventorySet_AlreadyEnabledIsNoop(t *testing.T) {
	factsDir := t.TempDir()
	factPath := filepath.Join(factsDir, packageInventoryFactFilename)
	if err := os.WriteFile(factPath, []byte("original content\n"), 0o755); err != nil {
		t.Fatalf("seed fact file: %v", err)
	}
	var ranPuppet bool
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		ranPuppet = true
		return "", "", 0, nil
	}
	handler := NewHandler(runner, "puppet", factsDir, fileExistsAt("/usr/bin/dpkg-query"))

	req := requestPayload(t, "puppet", actionPackageInventorySet, packageInventorySetParams{Enabled: true})
	wr := decodeWireResponse(t, handler(context.Background(), req))

	if wr.Error != "" {
		t.Fatalf("Error = %q, want empty", wr.Error)
	}
	if !wr.Result.PackageInventory.Enabled {
		t.Error("Enabled = false, want true (already enabled)")
	}
	if wr.Result.PackageInventory.Ran {
		t.Error("Ran = true, want false - a no-op toggle should not report a run")
	}
	if ranPuppet {
		t.Error("puppet was invoked for a no-op toggle (already in the requested state)")
	}
	content, err := os.ReadFile(factPath)
	if err != nil || string(content) != "original content\n" {
		t.Errorf("fact file content = %q, err %v; want unchanged (\"original content\\n\")", content, err)
	}
}

func TestHandler_PackageInventorySet_AlreadyDisabledIsNoop(t *testing.T) {
	factsDir := t.TempDir()
	var ranPuppet bool
	runner := func(ctx context.Context, name string, args ...string) (string, string, int, error) {
		ranPuppet = true
		return "", "", 0, nil
	}
	handler := NewHandler(runner, "puppet", factsDir, noFileExists)

	req := requestPayload(t, "puppet", actionPackageInventorySet, packageInventorySetParams{Enabled: false})
	wr := decodeWireResponse(t, handler(context.Background(), req))

	if wr.Error != "" {
		t.Fatalf("Error = %q, want empty", wr.Error)
	}
	if wr.Result.PackageInventory.Enabled {
		t.Error("Enabled = true, want false (already disabled)")
	}
	if wr.Result.PackageInventory.Ran {
		t.Error("Ran = true, want false - a no-op toggle should not report a run")
	}
	if ranPuppet {
		t.Error("puppet was invoked for a no-op toggle (already in the requested state)")
	}
}
