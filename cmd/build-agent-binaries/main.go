// Command build-agent-binaries cross-compiles node-agent-client for
// every platform internal/agentdist embeds, stamping each with the
// version and build time, and writes builds.json beside them so the
// console knows which client build it serves (see design.md in
// add-agent-auto-upgrade). Invoked by `make agent-binaries` and the
// Dockerfile - build-time only.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	outDir  = "internal/agentdist/bin"
	pkgPath = "github.com/voxpupuli/enterprise-console/internal/nodeagent"
)

// platforms must match internal/agentdist's supportedPlatforms.
var platforms = []string{"linux/amd64", "linux/arm64", "windows/amd64", "darwin/amd64", "darwin/arm64"}

// build is one platform's entry in builds.json - mirrored by
// internal/agentdist, which reads it.
type build struct {
	Digest    string `json:"digest"`
	BuildTime string `json:"buildTime"`
	Version   string `json:"version"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: build-agent-binaries <version>")
		os.Exit(1)
	}
	version := os.Args[1]

	// One build time for every platform, so "which is newer" has the
	// same answer whichever platform a node runs. AGENT_BUILD_TIME
	// overrides it for reproducible builds and for tests that need two
	// builds a known distance apart.
	buildTime := time.Now().UTC().Format(time.RFC3339)
	if override := os.Getenv("AGENT_BUILD_TIME"); override != "" {
		t, err := time.Parse(time.RFC3339, override)
		if err != nil {
			fatal(fmt.Errorf("AGENT_BUILD_TIME must be RFC 3339: %w", err))
		}
		buildTime = t.UTC().Format(time.RFC3339)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}

	builds := map[string]build{}
	for _, platform := range platforms {
		goos, goarch, _ := strings.Cut(platform, "/")
		target := filepath.Join(outDir, "node-agent-client-"+goos+"-"+goarch)
		ldflags := fmt.Sprintf("-X %s.Version=%s -X %s.BuildTime=%s", pkgPath, version, pkgPath, buildTime)
		cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", target, "./cmd/node-agent-client")
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fatal(fmt.Errorf("building %s: %w", platform, err))
		}
		data, err := os.ReadFile(target)
		if err != nil {
			fatal(err)
		}
		sum := sha256.Sum256(data)
		builds[platform] = build{Digest: hex.EncodeToString(sum[:]), BuildTime: buildTime, Version: version}
		fmt.Println("wrote", target)
	}

	out, err := json.MarshalIndent(builds, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "builds.json"), out, 0o644); err != nil {
		fatal(err)
	}
	fmt.Println("wrote", filepath.Join(outDir, "builds.json"))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "build-agent-binaries:", err)
	os.Exit(1)
}
