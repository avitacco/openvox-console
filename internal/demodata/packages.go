package demodata

// Package is one installed package as a node's package inventory reports
// it. The triple matches what openvoxdb's package_inventory entity stores.
type Package struct {
	Name     string
	Version  string
	Provider string
}

// basePackages are installed everywhere on a given provider - the
// uninteresting bulk that makes a package inventory page look like a real
// one rather than a list of six hand-picked entries.
var basePackages = map[string][]Package{
	"apt": {
		{Name: "bash", Version: "5.2.15-2"},
		{Name: "coreutils", Version: "9.1-1"},
		{Name: "curl", Version: "7.88.1-10"},
		{Name: "dpkg", Version: "1.21.22"},
		{Name: "grep", Version: "3.8-5"},
		{Name: "libc6", Version: "2.36-9"},
		{Name: "openssh-server", Version: "9.2p1-2"},
		{Name: "openssl", Version: "3.0.11-1"},
		{Name: "python3", Version: "3.11.2-6"},
		{Name: "rsync", Version: "3.2.7-1"},
		{Name: "systemd", Version: "252.22-1"},
		{Name: "tar", Version: "1.34+dfsg-1.2"},
		{Name: "vim", Version: "9.0.1378-2"},
		{Name: "zlib1g", Version: "1.2.13.dfsg-1"},
	},
	"dnf": {
		{Name: "bash", Version: "5.1.8-9.el9"},
		{Name: "coreutils", Version: "8.32-35.el9"},
		{Name: "curl", Version: "7.76.1-29.el9"},
		{Name: "glibc", Version: "2.34-100.el9"},
		{Name: "grep", Version: "3.6-5.el9"},
		{Name: "openssh-server", Version: "8.7p1-38.el9"},
		{Name: "openssl", Version: "3.0.7-27.el9"},
		{Name: "python3", Version: "3.9.18-1.el9"},
		{Name: "rpm", Version: "4.16.1.3-29.el9"},
		{Name: "rsync", Version: "3.2.3-19.el9"},
		{Name: "systemd", Version: "252-32.el9"},
		{Name: "tar", Version: "1.34-6.el9"},
		{Name: "vim-enhanced", Version: "8.2.2637-20.el9"},
		{Name: "zlib", Version: "1.2.11-40.el9"},
	},
	"zypper": {
		{Name: "bash", Version: "4.4-150400.25.22"},
		{Name: "coreutils", Version: "8.32-150400.9.6"},
		{Name: "curl", Version: "8.0.1-150400.5.35"},
		{Name: "glibc", Version: "2.31-150300.63.1"},
		{Name: "openssh", Version: "8.4p1-150300.3.24"},
		{Name: "openssl-3", Version: "3.0.8-150600.5.3"},
		{Name: "python3", Version: "3.6.15-150300.10.42"},
		{Name: "rsync", Version: "3.2.7-150500.3.3"},
		{Name: "systemd", Version: "249.17-150400.8.40"},
		{Name: "tar", Version: "1.34-150000.3.15"},
		{Name: "zlib", Version: "1.2.13-150500.4.3"},
	},
	"windows": {
		{Name: "7-Zip 23.01 (x64)", Version: "23.01"},
		{Name: "Microsoft Edge", Version: "125.0.2535.51"},
		{Name: "Microsoft Visual C++ 2015-2022 Redistributable (x64)", Version: "14.38.33130"},
		{Name: "OpenVox Agent", Version: "8.10.0"},
		{Name: "Windows Admin Center", Version: "2311"},
	},
}

// rolePackages are what a node's role adds on top of the base set. This is
// what makes "which nodes have nginx" a question with an interesting
// answer on the Packages page.
var rolePackages = map[string][]Package{
	"web":          {{Name: "nginx", Version: "1.22.1-9"}, {Name: "libssl3", Version: "3.0.11-1"}},
	"database":     {{Name: "postgresql-15", Version: "15.6-0"}, {Name: "pgbouncer", Version: "1.21.0-1"}},
	"cache":        {{Name: "redis-server", Version: "7.0.15-1"}},
	"loadbalancer": {{Name: "haproxy", Version: "2.6.12-1"}, {Name: "keepalived", Version: "2.2.7-1"}},
	"monitoring":   {{Name: "prometheus", Version: "2.45.0-1"}, {Name: "grafana", Version: "10.4.2-1"}},
	"logging":      {{Name: "rsyslog", Version: "8.2302.0-1"}, {Name: "logrotate", Version: "3.21.0-1"}},
	"mail":         {{Name: "postfix", Version: "3.7.10-1"}, {Name: "dovecot-core", Version: "2.3.19-1"}},
	"application":  {{Name: "Microsoft .NET 8 Runtime", Version: "8.0.4"}},
	"ci":           {{Name: "git", Version: "2.39.2-1"}, {Name: "docker.io", Version: "20.10.24-1"}},
	"api":          {{Name: "nginx", Version: "1.22.1-9"}, {Name: "golang-go", Version: "1.21.5-1"}},
	"backup":       {{Name: "borgbackup", Version: "1.2.8-1"}},
}

// PackagesFor returns the package inventory a node reports: its
// provider's base set plus whatever its role adds, with the provider
// filled in on every entry.
func PackagesFor(n Node) []Package {
	provider := n.Platform.PkgProvider

	var pkgs []Package
	for _, p := range basePackages[provider] {
		p.Provider = provider
		pkgs = append(pkgs, p)
	}
	for _, p := range rolePackages[n.Role] {
		// A role's package list is written for Linux; on Windows only the
		// Windows-shaped entries make sense, and the base set already
		// carries those.
		if provider == "windows" && p.Name != "Microsoft .NET 8 Runtime" {
			continue
		}
		p.Provider = provider
		pkgs = append(pkgs, p)
	}
	return pkgs
}
