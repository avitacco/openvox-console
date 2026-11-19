package demodata

import "time"

// PatchUpdate is one pending update a demo node reports.
type PatchUpdate struct {
	Name      string
	Installed string
	Available string
	Security  bool
}

// PatchScan is the result of a demo node's last successful scan for pending
// updates. Like the fleet's packages it is fabricated, but only for nodes
// whose package manager the console can scan (apt, dnf, yum): a SLES or
// Windows node gets no scan, which the Patching page shows as unable to
// report - the same as it would in a real fleet.
type PatchScan struct {
	Certname       string
	Manager        string
	RebootRequired bool
	// Age is how long before the demo Instant the scan succeeded.
	Age     time.Duration
	Updates []PatchUpdate
}

// PatchScans is the demo patch inventory: most Linux nodes have a few
// updates pending, a few of them security fixes, one needs a reboot and
// some are fully patched, so the page shows a spread rather than a wall of
// the same row.
var PatchScans = buildPatchScans()

func buildPatchScans() []PatchScan {
	apt := func(name, installed, available string, security bool) PatchUpdate {
		return PatchUpdate{Name: name, Installed: installed, Available: available, Security: security}
	}
	web := []PatchUpdate{
		apt("openssl", "3.0.13-1~deb12u1", "3.0.15-1~deb12u1", true),
		apt("libssl3", "3.0.13-1~deb12u1", "3.0.15-1~deb12u1", true),
		apt("nginx", "1.22.1-9", "1.22.1-9+deb12u1", true),
		apt("curl", "7.88.1-10+deb12u5", "7.88.1-10+deb12u8", false),
		apt("tzdata", "2024a-0+deb12u1", "2024b-0+deb12u1", false),
	}
	ubuntu := []PatchUpdate{
		apt("openssl", "3.0.13-0ubuntu3.1", "3.0.13-0ubuntu3.4", true),
		apt("linux-image-generic", "6.8.0-45.45", "6.8.0-48.48", true),
		apt("python3", "3.12.3-0ubuntu2", "3.12.3-0ubuntu2.1", false),
	}
	rhel := []PatchUpdate{
		apt("kernel", "5.14.0-427.13.1.el9_4", "5.14.0-427.37.1.el9_4", true),
		apt("openssl-libs", "3.0.7-27.el9", "3.0.7-28.el9_4", true),
		apt("postgresql-server", "16.2-1.el9", "16.4-1.el9", false),
		apt("systemd", "252-32.el9_4", "252-38.el9_4", false),
	}

	return []PatchScan{
		{Certname: "web-01.prod." + Domain, Manager: "apt", Age: 40 * time.Minute, Updates: web},
		{Certname: "web-02.prod." + Domain, Manager: "apt", Age: 41 * time.Minute, Updates: web},
		{Certname: "web-03.prod." + Domain, Manager: "apt", Age: 43 * time.Minute, Updates: web[:3]},
		{Certname: "web-04.prod." + Domain, Manager: "apt", Age: 52 * time.Minute, Updates: ubuntu[:2]},
		{Certname: "web-05.prod." + Domain, Manager: "apt", Age: 54 * time.Minute, Updates: ubuntu},
		{Certname: "db-01.prod." + Domain, Manager: "dnf", RebootRequired: true, Age: 62 * time.Minute, Updates: rhel},
		{Certname: "db-02.prod." + Domain, Manager: "dnf", Age: 63 * time.Minute, Updates: rhel[2:]},
		{Certname: "db-03.prod." + Domain, Manager: "dnf", Age: 66 * time.Minute, Updates: rhel[1:3]},
		{Certname: "cache-01.prod." + Domain, Manager: "apt", Age: 71 * time.Minute, Updates: web[3:]},
		{Certname: "cache-02.prod." + Domain, Manager: "apt", Age: 72 * time.Minute},
		{Certname: "lb-01.prod." + Domain, Manager: "apt", Age: 80 * time.Minute, Updates: ubuntu[:1]},
		{Certname: "lb-02.prod." + Domain, Manager: "apt", Age: 81 * time.Minute, Updates: ubuntu[:2]},
		{Certname: "monitor-01.prod." + Domain, Manager: "dnf", Age: 90 * time.Minute, Updates: rhel[:2]},
		{Certname: "mail-01.prod." + Domain, Manager: "apt", Age: 95 * time.Minute},
		{Certname: "web-01.staging." + Domain, Manager: "apt", Age: 30 * time.Minute, Updates: ubuntu},
		{Certname: "web-02.staging." + Domain, Manager: "apt", Age: 31 * time.Minute, Updates: ubuntu[2:]},
		{Certname: "db-01.staging." + Domain, Manager: "dnf", Age: 35 * time.Minute, Updates: rhel[3:]},
		{Certname: "api-01.team-a." + Domain, Manager: "apt", Age: 100 * time.Minute, Updates: web[:2]},
		{Certname: "api-02.team-a." + Domain, Manager: "apt", Age: 101 * time.Minute, Updates: web[3:]},
	}
}
