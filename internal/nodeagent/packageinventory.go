package nodeagent

import (
	"embed"
	"os"
)

// factassets holds the two package-inventory external fact script
// variants, moved here from internal/agentdist/pkgassets (see design.md
// in add-package-inventory-toggle) - node-agent-client itself now
// writes and removes this file, not the .deb/.rpm package or the
// install script. Content unchanged from before the move.
//
//go:embed factassets/package_inventory_apt.sh factassets/package_inventory_rpm.sh
var factassets embed.FS

// packageManagerFamily identifies which package-inventory fact script
// variant a node needs.
type packageManagerFamily int

const (
	familyUnknown packageManagerFamily = iota
	familyAPT
	familyRPM
)

// detectPackageManagerFamily checks well-known binary paths via
// fileExists, not a $PATH-dependent lookup (a systemd service's own
// environment may not have one) and not a shell-out (see design.md).
func detectPackageManagerFamily(fileExists func(string) bool) packageManagerFamily {
	switch {
	case fileExists("/usr/bin/dpkg-query"):
		return familyAPT
	case fileExists("/usr/bin/rpm"):
		return familyRPM
	default:
		return familyUnknown
	}
}

// FileExists is the production fileExists implementation NewHandler
// expects - os.Stat against a real path. Exported since
// cmd/node-agent-client (the real production caller) lives outside this
// package; tests within this package inject their own instead, to
// exercise every family without depending on what's actually installed
// on the machine running them.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// packageInventoryFactScript returns the embedded fact script content
// for family, or false if family is familyUnknown.
func packageInventoryFactScript(family packageManagerFamily) ([]byte, bool) {
	var name string
	switch family {
	case familyAPT:
		name = "factassets/package_inventory_apt.sh"
	case familyRPM:
		name = "factassets/package_inventory_rpm.sh"
	default:
		return nil, false
	}
	data, err := factassets.ReadFile(name)
	if err != nil {
		// Only reachable if the embed itself is broken (wrong filename
		// at build time) - both real files are always present.
		return nil, false
	}
	return data, true
}
