package agentdist

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"time"
)

// packageName is node-agent-client's package name in both the apt and
// yum repos this file generates.
const packageName = "node-agent-client"

// manifestEntry mirrors cmd/build-agent-packages's own type - kept as a
// separate definition (not a shared import) since that command is
// build-time-only and never linked into the console binary.
type manifestEntry struct {
	Format   string `json:"format"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// packageRepo holds the pre-rendered apt and yum repository metadata
// this console serves, computed once from the embedded packages built
// by `make agent-packages` (see design.md in add-native-agent-packaging
// for why this is hand-rolled rather than shelled out to
// dpkg-scanpackages/createrepo: a single-package repo's metadata is
// simple enough to generate directly, keeping the build machine free of
// any apt/yum tooling dependency).
type packageRepo struct {
	// aptPoolPath(entry) -> raw .deb bytes.
	debFiles map[string][]byte
	// arch -> Packages file content (uncompressed).
	aptPackages map[string][]byte
	// arch -> Packages.gz content.
	aptPackagesGz map[string][]byte
	aptRelease    []byte

	// filename -> raw .rpm bytes, served flat at the yum repo root.
	rpmFiles     map[string][]byte
	yumRepomd    []byte
	yumPrimaryGz []byte
}

// aptPoolPath returns the pool path apt repo convention uses: the
// package's first letter, then the package name, then the file -
// e.g. "pool/main/n/node-agent-client/node-agent-client_1.0.0_amd64.deb".
func aptPoolPath(filename string) string {
	return path.Join("pool", "main", packageName[:1], packageName, filename)
}

// buildPackageRepo reads packagesFS's manifest.json and package files
// and renders the apt/yum repository metadata once. Returns
// (nil, nil) if no manifest is present (a build that only ran
// `make agent-binaries`, not `make agent-packages` - the packages/
// route just serves nothing rather than the console failing to start;
// no Linux node has ever depended on this path if the operator never
// built it).
func buildPackageRepo() (*packageRepo, error) {
	manifestBytes, err := packagesFS.ReadFile("packages/manifest.json")
	if err != nil {
		return nil, nil //nolint:nilnil // see doc comment: absent manifest is a valid "not built" state, not an error
	}

	var manifest []manifestEntry
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return nil, fmt.Errorf("parsing packages/manifest.json: %w", err)
	}

	repo := &packageRepo{
		debFiles:      map[string][]byte{},
		aptPackages:   map[string][]byte{},
		aptPackagesGz: map[string][]byte{},
		rpmFiles:      map[string][]byte{},
	}

	var debEntries, rpmEntries []manifestEntry
	for _, e := range manifest {
		data, err := packagesFS.ReadFile(path.Join("packages", e.Filename))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Filename, err)
		}
		switch e.Format {
		case "deb":
			repo.debFiles[e.Filename] = data
			debEntries = append(debEntries, e)
		case "rpm":
			repo.rpmFiles[e.Filename] = data
			rpmEntries = append(rpmEntries, e)
		default:
			return nil, fmt.Errorf("manifest entry %s has unknown format %q", e.Filename, e.Format)
		}
	}

	if err := repo.buildAptMetadata(debEntries); err != nil {
		return nil, err
	}
	if err := repo.buildYumMetadata(rpmEntries); err != nil {
		return nil, err
	}
	return repo, nil
}

func (r *packageRepo) buildAptMetadata(entries []manifestEntry) error {
	byArch := map[string][]manifestEntry{}
	var arches []string
	for _, e := range entries {
		if _, ok := byArch[e.Arch]; !ok {
			arches = append(arches, e.Arch)
		}
		byArch[e.Arch] = append(byArch[e.Arch], e)
	}
	sort.Strings(arches)

	type releaseChecksum struct {
		sha256 string
		size   int
		path   string
	}
	var checksums []releaseChecksum

	for _, arch := range arches {
		var buf bytes.Buffer
		for _, e := range byArch[arch] {
			fmt.Fprintf(&buf, "Package: %s\n", packageName)
			fmt.Fprintf(&buf, "Version: %s\n", e.Version)
			fmt.Fprintf(&buf, "Architecture: %s\n", e.Arch)
			fmt.Fprintf(&buf, "Maintainer: OpenVox Console\n")
			fmt.Fprintf(&buf, "Filename: %s\n", aptPoolPath(e.Filename))
			fmt.Fprintf(&buf, "Size: %d\n", e.Size)
			fmt.Fprintf(&buf, "SHA256: %s\n", e.SHA256)
			fmt.Fprintf(&buf, "Description: OpenVox Console node-agent-client - connects a node to its console over the NATS-based node transport.\n")
			fmt.Fprintf(&buf, "\n")
		}
		plain := buf.Bytes()
		gz, err := gzipBytes(plain)
		if err != nil {
			return err
		}
		r.aptPackages[arch] = plain
		r.aptPackagesGz[arch] = gz

		checksums = append(checksums,
			releaseChecksum{sha256hex(plain), len(plain), fmt.Sprintf("main/binary-%s/Packages", arch)},
			releaseChecksum{sha256hex(gz), len(gz), fmt.Sprintf("main/binary-%s/Packages.gz", arch)},
		)
	}

	var rel bytes.Buffer
	fmt.Fprintf(&rel, "Origin: OpenVox Console\n")
	fmt.Fprintf(&rel, "Label: OpenVox Console\n")
	fmt.Fprintf(&rel, "Suite: stable\n")
	fmt.Fprintf(&rel, "Codename: stable\n")
	fmt.Fprintf(&rel, "Architectures: %s\n", joinSpace(arches))
	fmt.Fprintf(&rel, "Components: main\n")
	fmt.Fprintf(&rel, "Date: %s\n", time.Now().UTC().Format(time.RFC1123))
	fmt.Fprintf(&rel, "SHA256:\n")
	for _, c := range checksums {
		fmt.Fprintf(&rel, " %s %d %s\n", c.sha256, c.size, c.path)
	}
	r.aptRelease = rel.Bytes()
	return nil
}

// buildYumMetadata hand-formats primary.xml rather than using
// encoding/xml's struct-tag namespace support: dnf/yum's metadata
// parser is strict about matching real createrepo output (the
// "rpm:"-prefixed elements under xmlns:rpm specifically), which is
// easier to get right by matching that shape textually than by relying
// on encoding/xml to infer the same prefix from a bare namespace URI.
func (r *packageRepo) buildYumMetadata(entries []manifestEntry) error {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Arch < entries[j].Arch })

	var pkgs bytes.Buffer
	now := time.Now().Unix()
	for _, e := range entries {
		fmt.Fprintf(&pkgs, "<package type=\"rpm\">\n")
		fmt.Fprintf(&pkgs, "  <name>%s</name>\n", packageName)
		fmt.Fprintf(&pkgs, "  <arch>%s</arch>\n", e.Arch)
		fmt.Fprintf(&pkgs, "  <version epoch=\"0\" ver=\"%s\" rel=\"1\"/>\n", e.Version)
		fmt.Fprintf(&pkgs, "  <checksum type=\"sha256\" pkgid=\"YES\">%s</checksum>\n", e.SHA256)
		fmt.Fprintf(&pkgs, "  <summary>OpenVox Console node-agent-client</summary>\n")
		fmt.Fprintf(&pkgs, "  <description>OpenVox Console node-agent-client - connects a node to its console over the NATS-based node transport.</description>\n")
		fmt.Fprintf(&pkgs, "  <packager>OpenVox Console</packager>\n")
		fmt.Fprintf(&pkgs, "  <url>https://github.com/voxpupuli/enterprise-console</url>\n")
		fmt.Fprintf(&pkgs, "  <time file=\"%d\" build=\"%d\"/>\n", now, now)
		fmt.Fprintf(&pkgs, "  <size package=\"%d\" installed=\"0\" archive=\"0\"/>\n", e.Size)
		fmt.Fprintf(&pkgs, "  <location href=\"%s\"/>\n", e.Filename)
		fmt.Fprintf(&pkgs, "  <format>\n")
		fmt.Fprintf(&pkgs, "    <rpm:license>Apache-2.0</rpm:license>\n")
		fmt.Fprintf(&pkgs, "    <rpm:group>Unspecified</rpm:group>\n")
		fmt.Fprintf(&pkgs, "    <rpm:sourcerpm>%s-%s-1.src.rpm</rpm:sourcerpm>\n", packageName, e.Version)
		fmt.Fprintf(&pkgs, "    <rpm:header-range start=\"0\" end=\"0\"/>\n")
		fmt.Fprintf(&pkgs, "  </format>\n")
		fmt.Fprintf(&pkgs, "</package>\n")
	}

	primary := []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<metadata xmlns="http://linux.duke.edu/metadata/common" xmlns:rpm="http://linux.duke.edu/metadata/rpm" packages="%d">
%s</metadata>
`, len(entries), pkgs.String()))

	primaryGz, err := gzipBytes(primary)
	if err != nil {
		return err
	}
	r.yumPrimaryGz = primaryGz

	repomd := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo">
  <revision>1</revision>
  <data type="primary">
    <checksum type="sha256">%s</checksum>
    <open-checksum type="sha256">%s</open-checksum>
    <location href="repodata/primary.xml.gz"/>
    <timestamp>%d</timestamp>
    <size>%d</size>
    <open-size>%d</open-size>
  </data>
</repomd>
`, sha256hex(primaryGz), sha256hex(primary), time.Now().Unix(), len(primaryGz), len(primary))
	r.yumRepomd = []byte(repomd)
	return nil
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(data); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sha256hex(data []byte) string {
	h := sha256.New()
	_, _ = io.Copy(h, bytes.NewReader(data))
	return hex.EncodeToString(h.Sum(nil))
}

func joinSpace(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += " "
		}
		out += s
	}
	return out
}
