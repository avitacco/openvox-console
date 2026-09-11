// Command build-agent-packages builds the node-agent-client .deb and
// .rpm packages internal/agentdist embeds and serves, using nfpm - a
// pure-Go package builder requiring no dpkg-deb/rpmbuild/fpm on this
// build machine (see design.md in add-native-agent-packaging for why
// that mattered). Invoked by `make agent-packages`, after
// `make agent-binaries` has produced the linux/amd64 and linux/arm64
// node-agent-client binaries this tool packages - build-time only,
// never run by the console itself or on a managed node.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/goreleaser/nfpm/v2"
	"github.com/goreleaser/nfpm/v2/files"

	_ "github.com/goreleaser/nfpm/v2/deb"
	_ "github.com/goreleaser/nfpm/v2/rpm"
)

// manifestEntry describes one built package, so internal/agentdist can
// generate apt/yum repo metadata at runtime without parsing the binary
// .deb/.rpm formats itself - build-agent-packages already knows this
// information the moment it writes each file.
type manifestEntry struct {
	Format   string `json:"format"` // "deb" or "rpm"
	Arch     string `json:"arch"`   // package-format-native arch name (amd64/arm64 for deb, x86_64/aarch64 for rpm)
	Version  string `json:"version"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

const (
	binDir    = "internal/agentdist/bin"
	assetsDir = "internal/agentdist/pkgassets"
	outDir    = "internal/agentdist/packages"
)

// linuxArches maps this project's GOARCH names (used in the
// node-agent-client-linux-<goarch> binary filenames) to each package
// format's own architecture nomenclature.
var linuxArches = []struct {
	goarch  string
	debArch string
	rpmArch string
}{
	{goarch: "amd64", debArch: "amd64", rpmArch: "x86_64"},
	{goarch: "arm64", debArch: "arm64", rpmArch: "aarch64"},
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: build-agent-packages <version>")
		os.Exit(1)
	}
	debVersion, rpmVersion := sanitizeVersions(os.Args[1])

	// Cleared first, not just overwritten: package filenames embed the
	// version, so a stale build from a different version would
	// otherwise linger in outDir and get embedded into the console
	// binary alongside the current one for no reason.
	if err := os.RemoveAll(outDir); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fatal(err)
	}

	var manifest []manifestEntry
	for _, a := range linuxArches {
		binPath := filepath.Join(binDir, "node-agent-client-linux-"+a.goarch)
		if _, err := os.Stat(binPath); err != nil {
			fatal(fmt.Errorf("%s not found - run `make agent-binaries` first: %w", binPath, err))
		}

		entry, err := buildPackage("deb", a.debArch, debVersion, binPath)
		if err != nil {
			fatal(fmt.Errorf("building deb for %s: %w", a.goarch, err))
		}
		manifest = append(manifest, entry)

		entry, err = buildPackage("rpm", a.rpmArch, rpmVersion, binPath)
		if err != nil {
			fatal(fmt.Errorf("building rpm for %s: %w", a.goarch, err))
		}
		manifest = append(manifest, entry)
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "manifest.json"), manifestBytes, 0o644); err != nil {
		fatal(err)
	}
	fmt.Println("wrote", filepath.Join(outDir, "manifest.json"))
}

func buildPackage(format, arch, version, binPath string) (manifestEntry, error) {
	info := &nfpm.Info{
		Name:        "node-agent-client",
		Arch:        arch,
		Platform:    "linux",
		Version:     version,
		Maintainer:  "OpenVox Console",
		Description: "OpenVox Console node-agent-client - connects a node to its console over the NATS-based node transport.",
		Homepage:    "https://github.com/voxpupuli/enterprise-console",
		License:     "Apache-2.0",
		Overridables: nfpm.Overridables{
			Contents: files.Contents{
				{
					Source:      binPath,
					Destination: "/usr/bin/node-agent-client",
					FileInfo:    &files.ContentFileInfo{Mode: 0o755},
				},
				{
					Source:      filepath.Join(assetsDir, "node-agent-client.service"),
					Destination: "/usr/lib/systemd/system/node-agent-client.service",
					FileInfo:    &files.ContentFileInfo{Mode: 0o644},
				},
			},
			Scripts: nfpm.Scripts{
				PostInstall: filepath.Join(assetsDir, "postinst.sh"),
				PostRemove:  filepath.Join(assetsDir, "postrm.sh"),
			},
		},
	}

	packager, err := nfpm.Get(format)
	if err != nil {
		return manifestEntry{}, err
	}
	info = nfpm.WithDefaults(info)
	if err := info.Validate(); err != nil {
		return manifestEntry{}, err
	}

	filename := packager.ConventionalFileName(info)
	target := filepath.Join(outDir, filename)
	f, err := os.Create(target)
	if err != nil {
		return manifestEntry{}, err
	}
	defer f.Close()

	if err := packager.Package(info, f); err != nil {
		return manifestEntry{}, err
	}
	fmt.Println("wrote", target)

	sum, size, err := sha256File(target)
	if err != nil {
		return manifestEntry{}, err
	}
	return manifestEntry{
		Format:   format,
		Arch:     arch,
		Version:  version,
		Filename: filename,
		SHA256:   sum,
		Size:     size,
	}, nil
}

func sha256File(path string) (sum string, size int64, err error) {
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

// sanitizeVersions turns the project-wide VERSION string (from `git
// describe`, e.g. "v0.3.2-14-g1a2b3c4-dirty", or the "dev" fallback -
// see internal/runtime.Version) into a version safe for each package
// format. Debian tolerates hyphens in the upstream version portion but
// requires it start with a digit (confirmed live: dpkg rejected a
// literal "dev" build with "version number does not start with digit"
// - the "dev" fallback hits this whenever this repo has no tags yet);
// RPM's Version field rejects hyphens outright, so those become
// underscores there.
func sanitizeVersions(v string) (debVersion, rpmVersion string) {
	v = strings.TrimPrefix(v, "v")
	debVersion = v
	if len(debVersion) == 0 || debVersion[0] < '0' || debVersion[0] > '9' {
		debVersion = "0.0.0+" + debVersion
	}
	return debVersion, strings.ReplaceAll(v, "-", "_")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "build-agent-packages:", err)
	os.Exit(1)
}
