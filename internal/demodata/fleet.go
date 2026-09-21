package demodata

import "time"

// Every name in this file is fictional. Hostnames sit under example.com,
// which RFC 2606 reserves for documentation precisely so that a name like
// this cannot collide with a real host; addresses sit in RFC 1918 private
// space. Nothing here resembles a real organisation's infrastructure.
const (
	// Domain is the reserved documentation domain every demo node lives
	// under.
	Domain = "example.com"
)

// Platform is one operating system the demo fleet runs, with the fact
// values the console reads to group and display a node.
type Platform struct {
	Name         string // os.name, e.g. "Debian"
	Family       string // os.family, e.g. "Debian"
	Release      string // os.release.full, e.g. "12.5"
	Major        string // os.release.major, e.g. "12"
	Architecture string
	Kernel       string
	PkgProvider  string // the package provider its package inventory reports
}

// Platforms is the OS mix the demo fleet spans. Six distributions across
// three families plus Windows: enough that a fleet-wide breakdown shows a
// distribution rather than a single bar, which is the whole point of
// screenshotting one.
var Platforms = []Platform{
	{Name: "Debian", Family: "Debian", Release: "12.5", Major: "12", Architecture: "x86_64", Kernel: "Linux", PkgProvider: "apt"},
	{Name: "Ubuntu", Family: "Debian", Release: "24.04", Major: "24.04", Architecture: "x86_64", Kernel: "Linux", PkgProvider: "apt"},
	{Name: "Ubuntu", Family: "Debian", Release: "22.04", Major: "22.04", Architecture: "aarch64", Kernel: "Linux", PkgProvider: "apt"},
	{Name: "Rocky", Family: "RedHat", Release: "9.4", Major: "9", Architecture: "x86_64", Kernel: "Linux", PkgProvider: "dnf"},
	{Name: "AlmaLinux", Family: "RedHat", Release: "9.4", Major: "9", Architecture: "x86_64", Kernel: "Linux", PkgProvider: "dnf"},
	{Name: "SLES", Family: "Suse", Release: "15.6", Major: "15", Architecture: "x86_64", Kernel: "Linux", PkgProvider: "zypper"},
	{Name: "windows", Family: "windows", Release: "2022", Major: "2022", Architecture: "x64", Kernel: "windows", PkgProvider: "windows"},
}

// RunStatus is a node's latest Puppet run outcome.
type RunStatus string

const (
	StatusUnchanged RunStatus = "unchanged"
	StatusChanged   RunStatus = "changed"
	StatusFailed    RunStatus = "failed"
)

// Node is one fabricated machine in the demo fleet.
type Node struct {
	Certname    string
	Platform    Platform
	Environment string
	Role        string
	IPAddress   string
	Status      RunStatus
	// ReportAge is how long before the demo Instant this node last
	// reported. Spreading these out is what makes the dashboard's
	// "recently reported" grouping show more than one bucket.
	ReportAge time.Duration
	// Uptime in days, so node detail pages do not all read the same.
	UptimeDays int
	// CorrectiveChange marks a run that corrected drift rather than
	// simply applying a change - the console displays the two
	// differently.
	CorrectiveChange bool
}

// FQDN is the node's fully qualified name, which is also its certname.
func (n Node) FQDN() string { return n.Certname }

// Fleet is the demo fleet: 26 nodes across six roles, seven platform
// entries, three environments and all three run outcomes.
//
// The shape is deliberate rather than random. Most nodes are unchanged,
// because a fleet where a third of everything is failing does not look
// like a working deployment; a handful are changed; four are failed,
// enough that a failure list has something in it and a percentage figure
// is not 0%. Report ages are spread from minutes to over a day so that
// connectivity grouping has more than one bucket to show.
var Fleet = buildFleet()

func buildFleet() []Node {
	debian := Platforms[0]
	ubuntu24 := Platforms[1]
	ubuntu22 := Platforms[2]
	rocky := Platforms[3]
	alma := Platforms[4]
	sles := Platforms[5]
	windows := Platforms[6]

	return []Node{
		// --- production web tier ---
		{Certname: "web-01.prod." + Domain, Platform: debian, Environment: "production", Role: "web", IPAddress: "10.20.1.11", Status: StatusUnchanged, ReportAge: 4 * time.Minute, UptimeDays: 96},
		{Certname: "web-02.prod." + Domain, Platform: debian, Environment: "production", Role: "web", IPAddress: "10.20.1.12", Status: StatusUnchanged, ReportAge: 6 * time.Minute, UptimeDays: 96},
		{Certname: "web-03.prod." + Domain, Platform: debian, Environment: "production", Role: "web", IPAddress: "10.20.1.13", Status: StatusChanged, ReportAge: 3 * time.Minute, UptimeDays: 12, CorrectiveChange: true},
		{Certname: "web-04.prod." + Domain, Platform: ubuntu24, Environment: "production", Role: "web", IPAddress: "10.20.1.14", Status: StatusUnchanged, ReportAge: 9 * time.Minute, UptimeDays: 41},
		{Certname: "web-05.prod." + Domain, Platform: ubuntu24, Environment: "production", Role: "web", IPAddress: "10.20.1.15", Status: StatusFailed, ReportAge: 12 * time.Minute, UptimeDays: 41},

		// --- production database tier ---
		{Certname: "db-01.prod." + Domain, Platform: rocky, Environment: "production", Role: "database", IPAddress: "10.20.2.11", Status: StatusUnchanged, ReportAge: 7 * time.Minute, UptimeDays: 213},
		{Certname: "db-02.prod." + Domain, Platform: rocky, Environment: "production", Role: "database", IPAddress: "10.20.2.12", Status: StatusUnchanged, ReportAge: 8 * time.Minute, UptimeDays: 213},
		{Certname: "db-03.prod." + Domain, Platform: alma, Environment: "production", Role: "database", IPAddress: "10.20.2.13", Status: StatusChanged, ReportAge: 2 * time.Minute, UptimeDays: 58},

		// --- production cache and load balancing ---
		{Certname: "cache-01.prod." + Domain, Platform: debian, Environment: "production", Role: "cache", IPAddress: "10.20.3.11", Status: StatusUnchanged, ReportAge: 11 * time.Minute, UptimeDays: 134},
		{Certname: "cache-02.prod." + Domain, Platform: debian, Environment: "production", Role: "cache", IPAddress: "10.20.3.12", Status: StatusUnchanged, ReportAge: 13 * time.Minute, UptimeDays: 134},
		{Certname: "lb-01.prod." + Domain, Platform: ubuntu22, Environment: "production", Role: "loadbalancer", IPAddress: "10.20.0.11", Status: StatusUnchanged, ReportAge: 5 * time.Minute, UptimeDays: 302},
		{Certname: "lb-02.prod." + Domain, Platform: ubuntu22, Environment: "production", Role: "loadbalancer", IPAddress: "10.20.0.12", Status: StatusFailed, ReportAge: 26 * time.Minute, UptimeDays: 302},

		// --- production supporting services ---
		{Certname: "monitor-01.prod." + Domain, Platform: rocky, Environment: "production", Role: "monitoring", IPAddress: "10.20.4.11", Status: StatusUnchanged, ReportAge: 14 * time.Minute, UptimeDays: 77},
		{Certname: "log-01.prod." + Domain, Platform: sles, Environment: "production", Role: "logging", IPAddress: "10.20.4.21", Status: StatusUnchanged, ReportAge: 18 * time.Minute, UptimeDays: 149},
		{Certname: "log-02.prod." + Domain, Platform: sles, Environment: "production", Role: "logging", IPAddress: "10.20.4.22", Status: StatusChanged, ReportAge: 21 * time.Minute, UptimeDays: 149},
		{Certname: "mail-01.prod." + Domain, Platform: debian, Environment: "production", Role: "mail", IPAddress: "10.20.4.31", Status: StatusUnchanged, ReportAge: 16 * time.Minute, UptimeDays: 188},

		// --- Windows estate ---
		{Certname: "app-win-01.prod." + Domain, Platform: windows, Environment: "production", Role: "application", IPAddress: "10.20.5.11", Status: StatusUnchanged, ReportAge: 19 * time.Minute, UptimeDays: 23},
		{Certname: "app-win-02.prod." + Domain, Platform: windows, Environment: "production", Role: "application", IPAddress: "10.20.5.12", Status: StatusChanged, ReportAge: 24 * time.Minute, UptimeDays: 23, CorrectiveChange: true},
		{Certname: "app-win-03.prod." + Domain, Platform: windows, Environment: "production", Role: "application", IPAddress: "10.20.5.13", Status: StatusFailed, ReportAge: 31 * time.Minute, UptimeDays: 4},

		// --- staging ---
		{Certname: "web-01.staging." + Domain, Platform: ubuntu24, Environment: "staging", Role: "web", IPAddress: "10.30.1.11", Status: StatusChanged, ReportAge: 1 * time.Minute, UptimeDays: 3},
		{Certname: "web-02.staging." + Domain, Platform: ubuntu24, Environment: "staging", Role: "web", IPAddress: "10.30.1.12", Status: StatusUnchanged, ReportAge: 10 * time.Minute, UptimeDays: 3},
		{Certname: "db-01.staging." + Domain, Platform: alma, Environment: "staging", Role: "database", IPAddress: "10.30.2.11", Status: StatusUnchanged, ReportAge: 22 * time.Minute, UptimeDays: 31},
		{Certname: "ci-01.staging." + Domain, Platform: ubuntu22, Environment: "staging", Role: "ci", IPAddress: "10.30.6.11", Status: StatusFailed, ReportAge: 37 * time.Minute, UptimeDays: 9},

		// --- a second control repo's environment, to show multi-source
		// code deployment having actually landed somewhere ---
		{Certname: "api-01.team-a." + Domain, Platform: debian, Environment: "team_a_production", Role: "api", IPAddress: "10.40.1.11", Status: StatusUnchanged, ReportAge: 15 * time.Minute, UptimeDays: 62},
		{Certname: "api-02.team-a." + Domain, Platform: debian, Environment: "team_a_production", Role: "api", IPAddress: "10.40.1.12", Status: StatusChanged, ReportAge: 17 * time.Minute, UptimeDays: 62},

		// --- one node that has not reported in a while, so the
		// "stale"/"unreachable" presentation has something to show ---
		{Certname: "backup-01.prod." + Domain, Platform: sles, Environment: "production", Role: "backup", IPAddress: "10.20.7.11", Status: StatusUnchanged, ReportAge: 29 * time.Hour, UptimeDays: 401},
	}
}

// Environments is every Puppet environment the demo fleet reports from.
func Environments() []string {
	seen := map[string]bool{}
	var envs []string
	for _, n := range Fleet {
		if !seen[n.Environment] {
			seen[n.Environment] = true
			envs = append(envs, n.Environment)
		}
	}
	return envs
}

// CountByStatus returns how many demo nodes carry each run outcome.
func CountByStatus() map[RunStatus]int {
	counts := map[RunStatus]int{}
	for _, n := range Fleet {
		counts[n.Status]++
	}
	return counts
}
