// Package agentdist serves this console's own node-agent-client build to
// nodes: a platform-matched binary download and an install script that
// gets both openvoxagent and node-agent-client onto a node - see
// openspec/specs/agent-distribution for why this exists (neither OpenVox's
// packaging nor any external channel ships a working orchestration
// client) and why binaries are embedded at console build time rather
// than built on demand.
package agentdist

import (
	"embed"
	"fmt"
)

//go:embed all:bin
var binFS embed.FS

// packagesFS holds the .deb/.rpm packages `make agent-packages` builds
// (linux only - see design.md in add-native-agent-packaging for why
// Windows/macOS still use the raw-binary route above) and the
// manifest.json describing them, read by repo.go to serve a minimal
// self-hosted apt/yum repository.
//
//go:embed all:packages
var packagesFS embed.FS

// supportedPlatforms lists the GOOS/GOARCH combinations `make
// agent-binaries` cross-compiles cmd/node-agent-client for. Windows is
// amd64-only: OpenVox publishes no Windows ARM64 openvoxagent package
// for a node-agent build to pair with (confirmed live against
// downloads.voxpupuli.org/windows/openvox8/ - see design.md in
// add-multi-platform-agent-install).
var supportedPlatforms = map[string]bool{
	"linux/amd64":   true,
	"linux/arm64":   true,
	"windows/amd64": true,
	"darwin/amd64":  true,
	"darwin/arm64":  true,
}

// binaryPath returns the embedded path for goos/goarch, or false if
// that platform isn't supported.
func binaryPath(goos, goarch string) (string, bool) {
	if !supportedPlatforms[goos+"/"+goarch] {
		return "", false
	}
	return fmt.Sprintf("bin/node-agent-client-%s-%s", goos, goarch), true
}
