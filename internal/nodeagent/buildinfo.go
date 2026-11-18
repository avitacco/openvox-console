package nodeagent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Version and BuildTime are stamped at build time with -ldflags -X (see
// cmd/build-agent-binaries). BuildTime is RFC 3339 in UTC, to the
// second. Both are empty in a plain `go build`, which the console reads
// as "unstamped": such a client is identified by its digest alone.
var (
	Version   = ""
	BuildTime = ""
)

// BuildInfo identifies one client build. Digest is the SHA-256 of the
// executable itself, so two clients report the same digest exactly when
// they are the same bytes - no version string to drift from the binary.
type BuildInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"buildTime"`
	Digest    string `json:"digest"`
}

// executablePath is this process's executable with symbolic links
// resolved, so it names the file a package manager owns.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

// fileSHA256 returns path's SHA-256 in hex and its size.
func fileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// CurrentBuildInfo reports the running client's build.
func CurrentBuildInfo() (BuildInfo, error) {
	exe, err := executablePath()
	if err != nil {
		return BuildInfo{}, fmt.Errorf("locate this executable: %w", err)
	}
	digest, _, err := fileSHA256(exe)
	if err != nil {
		return BuildInfo{}, fmt.Errorf("read this executable: %w", err)
	}
	return BuildInfo{Version: Version, BuildTime: BuildTime, Digest: digest}, nil
}
