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
	"time"

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
	Epoch    string `json:"epoch,omitempty"`   // rpm only
	Release  string `json:"release,omitempty"` // rpm only
	GoArch   string `json:"goarch"`            // the GOARCH of the client inside
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

// buildInfo is one platform's entry in bin/builds.json, written by
// cmd/build-agent-binaries.
type buildInfo struct {
	BuildTime string `json:"buildTime"`
}

// clientBuildTime reads the build time the client binaries were stamped
// with. Packages are versioned by it, so the package and the client
// inside it always agree on when they were built.
func clientBuildTime() (time.Time, error) {
	data, err := os.ReadFile(filepath.Join(binDir, "builds.json"))
	if err != nil {
		return time.Time{}, fmt.Errorf("%s/builds.json not found - run `make agent-binaries` first: %w", binDir, err)
	}
	var builds map[string]buildInfo
	if err := json.Unmarshal(data, &builds); err != nil {
		return time.Time{}, fmt.Errorf("parsing builds.json: %w", err)
	}
	return time.Parse(time.RFC3339, builds["linux/amd64"].BuildTime)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: build-agent-packages <version>")
		os.Exit(1)
	}
	buildTime, err := clientBuildTime()
	if err != nil {
		fatal(err)
	}
	debVersion, rpmVersion, rpmRelease := packageVersions(os.Args[1], buildTime)

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

		entry, err := buildPackage("deb", a.debArch, a.goarch, debVersion, "", "", binPath)
		if err != nil {
			fatal(fmt.Errorf("building deb for %s: %w", a.goarch, err))
		}
		manifest = append(manifest, entry)

		entry, err = buildPackage("rpm", a.rpmArch, a.goarch, rpmVersion, rpmEpoch, rpmRelease, binPath)
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

// packageScripts and rpmScripts decide when the install script
// (postinst.sh: write the environment file, enable and restart the
// service) runs.
//
// A deb runs it as postinst. An rpm runs it as %posttrans, not %post,
// because of the order rpm upgrades in: the new package's %post, then
// the OLD package's %postun. Packages shipped before postrm.sh learned
// to tell an upgrade from a removal stop the service, disable it and
// delete its environment file in that %postun - after a %post here
// would already have restarted it - so an upgrade from one of them left
// the node with no client running. %posttrans runs once at the very end
// of the transaction, after every %postun, so it repairs that and is
// also the only restart a clean upgrade needs.
func packageScripts(format string) nfpm.Scripts {
	scripts := nfpm.Scripts{PostRemove: filepath.Join(assetsDir, "postrm.sh")}
	if format != "rpm" {
		scripts.PostInstall = filepath.Join(assetsDir, "postinst.sh")
	}
	return scripts
}

func rpmScripts(format string) nfpm.RPMScripts {
	if format != "rpm" {
		return nfpm.RPMScripts{}
	}
	return nfpm.RPMScripts{PostTrans: filepath.Join(assetsDir, "postinst.sh")}
}

func buildPackage(format, arch, goarch, version, epoch, release, binPath string) (manifestEntry, error) {
	info := &nfpm.Info{
		Name:        "node-agent-client",
		Arch:        arch,
		Platform:    "linux",
		Version:     version,
		Epoch:       epoch,
		Release:     release,
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
			Scripts: packageScripts(format),
			// Pinned rather than left to nfpm's default: a running
			// client unpacks the package it is about to install to
			// check the binary inside (internal/nodeagent/pkgextract.go)
			// and reads gzip only.
			Deb: nfpm.Deb{Compression: "gzip"},
			RPM: nfpm.RPM{Compression: "gzip", Scripts: rpmScripts(format)},
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
		Epoch:    epoch,
		Release:  release,
		GoArch:   goarch,
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "build-agent-packages:", err)
	os.Exit(1)
}
