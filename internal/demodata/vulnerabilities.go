package demodata

// Advisory is one vulnerability the demo provider reports against a
// package the demo fleet actually has installed.
//
// The CVE identifiers here are real, published advisories, and the
// package/version pairs they are matched against are the ones this
// fabricated fleet reports. That combination is invented: no real host
// is being described, and nothing here is a statement about whether any
// real system is affected.
type Advisory struct {
	VulnID string
	// RecordID is the provider's own identifier for the record, which is
	// not always the CVE - real providers distinguish the two.
	RecordID string
	// Package is the package name as the fleet's inventory reports it.
	Package string
	// Severity is one of: critical, high, medium, low.
	Severity string
	// FixedVersion is empty when no fix is published yet.
	FixedVersion string
	URL          string
	// Providers that report this advisory. Listing an advisory under
	// both demo providers is what makes the "reported by" column on the
	// vulnerability page show more than one source.
	Providers []string
}

// Provider ids used by the demo advisories.
const (
	ProviderAdvisoryFeed = "demo-advisory-feed"
	ProviderScanner      = "demo-scanner"
)

// Advisories are the demo findings. Severities are spread deliberately -
// a couple of criticals so the severity filter has a top band with
// something in it, more highs and mediums, a few lows - and some have no
// published fix, so the "fix available" distinction is visible.
var Advisories = []Advisory{
	{
		VulnID: "CVE-2023-44487", RecordID: "GHSA-qppj-fm5r-hxr3", Package: "nginx",
		Severity: "high", FixedVersion: "1.24.0-1",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2023-44487",
		Providers: []string{ProviderAdvisoryFeed, ProviderScanner},
	},
	{
		VulnID: "CVE-2024-6387", RecordID: "USN-6859-1", Package: "openssh-server",
		Severity: "critical", FixedVersion: "9.2p1-2+deb12u3",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2024-6387",
		Providers: []string{ProviderAdvisoryFeed, ProviderScanner},
	},
	{
		VulnID: "CVE-2024-2511", RecordID: "DSA-5673-1", Package: "openssl",
		Severity: "medium", FixedVersion: "3.0.13-1",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2024-2511",
		Providers: []string{ProviderAdvisoryFeed},
	},
	{
		VulnID: "CVE-2023-4911", RecordID: "RHSA-2023:5453", Package: "glibc",
		Severity: "high", FixedVersion: "2.34-100.el9_4.2",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2023-4911",
		Providers: []string{ProviderAdvisoryFeed},
	},
	{
		VulnID: "CVE-2022-48174", RecordID: "GHSA-8w6f-9xhc-h6qq", Package: "bash",
		Severity: "critical", FixedVersion: "",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2022-48174",
		Providers: []string{ProviderScanner},
	},
	{
		VulnID: "CVE-2023-38545", RecordID: "DSA-5522-1", Package: "curl",
		Severity: "high", FixedVersion: "7.88.1-10+deb12u5",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2023-38545",
		Providers: []string{ProviderAdvisoryFeed, ProviderScanner},
	},
	{
		VulnID: "CVE-2023-39804", RecordID: "DSA-5588-1", Package: "tar",
		Severity: "low", FixedVersion: "1.34+dfsg-1.2+deb12u1",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2023-39804",
		Providers: []string{ProviderAdvisoryFeed},
	},
	{
		VulnID: "CVE-2024-28085", RecordID: "USN-6718-1", Package: "systemd",
		Severity: "medium", FixedVersion: "252.22-1~deb12u1",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2024-28085",
		Providers: []string{ProviderScanner},
	},
	{
		VulnID: "CVE-2023-45853", RecordID: "GHSA-9jgg-88mc-972h", Package: "zlib1g",
		Severity: "medium", FixedVersion: "",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2023-45853",
		Providers: []string{ProviderAdvisoryFeed},
	},
	{
		VulnID: "CVE-2024-3094", RecordID: "RHSA-2024:1535", Package: "rsync",
		Severity: "low", FixedVersion: "3.2.7-1+deb12u1",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2024-3094",
		Providers: []string{ProviderScanner},
	},
	{
		VulnID: "CVE-2023-5678", RecordID: "DSA-5588-2", Package: "postgresql-15",
		Severity: "high", FixedVersion: "15.7-0+deb12u1",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2023-5678",
		Providers: []string{ProviderAdvisoryFeed},
	},
	{
		VulnID: "CVE-2024-31449", RecordID: "GHSA-whxg-wx83-85cm", Package: "redis-server",
		Severity: "critical", FixedVersion: "7.0.15-2",
		URL:       "https://nvd.nist.gov/vuln/detail/CVE-2024-31449",
		Providers: []string{ProviderScanner},
	},
}

// DemoProvider describes one configured provider instance the seed
// creates.
type DemoProvider struct {
	ID          string
	DisplayName string
}

// DemoProviders are the two provider instances the demo findings are
// attributed to. Two rather than one so the console's merging of several
// sources - the highest severity wins, and the vulnerability page names
// every provider reporting it - is actually exercised.
var DemoProviders = []DemoProvider{
	{ID: ProviderAdvisoryFeed, DisplayName: "OSV advisory feed"},
	{ID: ProviderScanner, DisplayName: "Fleet scanner"},
}

// AdvisoriesFor returns the advisories a provider reports.
func AdvisoriesFor(providerID string) []Advisory {
	var out []Advisory
	for _, a := range Advisories {
		for _, p := range a.Providers {
			if p == providerID {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

// InstalledVersion returns the version of pkg that node has installed,
// and whether it has it at all. Findings are only generated for packages
// a node actually reports, so a vulnerability never names a node that
// does not have the package.
func InstalledVersion(node Node, pkg string) (string, bool) {
	for _, p := range PackagesFor(node) {
		if p.Name == pkg {
			return p.Version, true
		}
	}
	return "", false
}
