package demodata_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestDemoDataIsNotShipped asserts that nothing a user installs can
// reach this package or the seeding tool that uses it.
//
// The requirement is in specs/demo-data-seeding: the seed is a
// development-time tool, not part of the product. The way that breaks is
// undramatic - somebody imports demodata from a production package for a
// convenient constant, and a fabricated fleet's names ship inside the
// console. Checking the real dependency graph is the only way to notice.
//
// The Dockerfile builds ./cmd/console, ./cmd/node-agent-client,
// ./cmd/build-agent-packages and vendored g10k; ./cmd/enc-bridge ships
// in the openvoxserver image. Those are the binaries a user runs.
func TestDemoDataIsNotShipped(t *testing.T) {
	const modulePath = "github.com/voxpupuli/enterprise-console"

	// Full import paths, not ./cmd/... - this test runs with its own
	// package directory as the working directory, where a relative
	// pattern resolves to nothing.
	shipped := []string{
		modulePath + "/cmd/console",
		modulePath + "/cmd/enc-bridge",
		modulePath + "/cmd/node-agent-client",
		modulePath + "/cmd/build-agent-packages",
	}

	forbidden := []string{
		modulePath + "/internal/demodata",
		modulePath + "/cmd/demo-seed",
	}

	for _, binary := range shipped {
		out, err := exec.Command("go", "list", "-deps", binary).Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", binary, err)
		}

		deps := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, dep := range deps {
			for _, bad := range forbidden {
				if dep == bad {
					t.Errorf("%s depends on %s - demo data must never reach a shipped binary", binary, bad)
				}
			}
		}
	}
}
