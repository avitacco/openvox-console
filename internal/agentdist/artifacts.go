package agentdist

import (
	"encoding/json"
	"path"
	"sync"
	"time"
)

// Build identifies the node-agent-client this console embeds for one
// platform. BuildTime is zero for a client built without a stamp (a
// plain `go build` in development), which the upgrade coordinator reads
// as "upgrade nobody".
type Build struct {
	Digest    string
	BuildTime time.Time
	Version   string
}

// Artifact is what a running client downloads to upgrade itself: the
// package on Linux, the bare binary on Windows and macOS.
type Artifact struct {
	// Path is the artifact's location under the console's base URL.
	Path   string
	Size   int64
	SHA256 string
	// Format is "deb", "rpm" or "binary".
	Format string
}

// Install methods a client reports - mirrored from internal/nodeagent,
// which decides them.
const (
	FormatDeb    = "deb"
	FormatRPM    = "rpm"
	FormatBinary = "binary"
)

// buildsFile mirrors what cmd/build-agent-binaries writes.
type buildsFile map[string]struct {
	Digest    string `json:"digest"`
	BuildTime string `json:"buildTime"`
	Version   string `json:"version"`
}

// Catalog answers "which client build does this console serve for a
// platform, and what does a client download to get it". Built once from
// the embedded files.
type Catalog struct {
	builds    map[string]Build    // "goos/goarch"
	binaries  map[string]Artifact // "goos/goarch"
	packages  map[string]Artifact // "format/goarch"
	hasStamps bool
}

var (
	catalogOnce sync.Once
	catalog     *Catalog
)

// LoadCatalog returns the catalog for this console's embedded client
// builds. It never fails: a console built without stamped clients or
// without packages has a catalog that offers fewer upgrades, or none.
func LoadCatalog() *Catalog {
	catalogOnce.Do(func() {
		var manifest []manifestEntry
		if data, err := packagesFS.ReadFile("packages/manifest.json"); err == nil {
			_ = json.Unmarshal(data, &manifest)
		}
		buildsJSON, _ := binFS.ReadFile("bin/builds.json")
		catalog = newCatalog(buildsJSON, manifest, func(platform string) []byte {
			goos, goarch, _ := cutPlatform(platform)
			p, ok := binaryPath(goos, goarch)
			if !ok {
				return nil
			}
			data, _ := binFS.ReadFile(p)
			return data
		})
	})
	return catalog
}

func cutPlatform(platform string) (goos, goarch string, ok bool) {
	for i := range len(platform) {
		if platform[i] == '/' {
			return platform[:i], platform[i+1:], true
		}
	}
	return platform, "", false
}

// newCatalog builds a Catalog from builds.json's bytes (nil when the
// clients were built without it), the package manifest, and a reader
// for each platform's embedded binary.
func newCatalog(buildsJSON []byte, manifest []manifestEntry, binary func(platform string) []byte) *Catalog {
	c := &Catalog{builds: map[string]Build{}, binaries: map[string]Artifact{}, packages: map[string]Artifact{}}

	var stamps buildsFile
	if len(buildsJSON) > 0 {
		_ = json.Unmarshal(buildsJSON, &stamps)
	}

	for platform := range supportedPlatforms {
		data := binary(platform)
		if len(data) == 0 {
			continue
		}
		// The digest always comes from the bytes actually embedded, never
		// from builds.json: a binary rebuilt without rewriting that file
		// must not be advertised under a digest it does not have.
		digest := sha256hex(data)
		build := Build{Digest: digest}
		if stamp, ok := stamps[platform]; ok && stamp.Digest == digest {
			if t, err := time.Parse(time.RFC3339, stamp.BuildTime); err == nil {
				build.BuildTime = t
				build.Version = stamp.Version
				c.hasStamps = true
			}
		}
		c.builds[platform] = build

		goos, goarch, _ := cutPlatform(platform)
		c.binaries[platform] = Artifact{
			Path:   "/packages/node-agent-client?os=" + goos + "&arch=" + goarch,
			Size:   int64(len(data)),
			SHA256: digest,
			Format: FormatBinary,
		}
	}

	for _, e := range manifest {
		var p string
		switch e.Format {
		case FormatDeb:
			p = path.Join("/packages/apt", aptPoolPath(e.Filename))
		case FormatRPM:
			p = path.Join("/packages/yum", e.Filename)
		default:
			continue
		}
		c.packages[e.Format+"/"+e.GoArch] = Artifact{Path: p, Size: e.Size, SHA256: e.SHA256, Format: e.Format}
	}
	return c
}

// Build returns the client build served for goos/goarch.
func (c *Catalog) Build(goos, goarch string) (Build, bool) {
	b, ok := c.builds[goos+"/"+goarch]
	return b, ok
}

// Stamped reports whether this console's clients carry build times. An
// unstamped console cannot tell newer from older and upgrades nobody.
func (c *Catalog) Stamped() bool { return c.hasStamps }

// Artifact returns what a client on goos/goarch installed by
// installMethod downloads to upgrade. It reports false when there is
// none: an unsupported platform, an install method that cannot be
// upgraded in place, or Linux packages that were not built for this
// console.
func (c *Catalog) Artifact(goos, goarch, installMethod string) (Artifact, bool) {
	switch installMethod {
	case FormatDeb, FormatRPM:
		if goos != "linux" {
			return Artifact{}, false
		}
		a, ok := c.packages[installMethod+"/"+goarch]
		return a, ok
	case FormatBinary:
		// A bare binary is the supported install on Windows and macOS
		// only. On Linux it means a pre-packaging install, which the
		// install script migrates.
		if goos == "linux" {
			return Artifact{}, false
		}
		a, ok := c.binaries[goos+"/"+goarch]
		return a, ok
	}
	return Artifact{}, false
}
