// Package shots declares which console views the marketing site shows a
// screenshot of.
//
// It is the single source of truth for that list, imported by both the
// capture tool (which drives a browser through it) and the site
// generator (which renders an <img> for each name). Keeping it one Go
// declaration rather than a config file makes "the site references a
// screenshot capture does not produce" a compile-time or build-time
// error instead of a broken image on a published page.
//
// Adding a view means adding a Shot here, running the capture, and
// referencing its Name from a page template. Nothing else.
package shots

// N_ marks a caption for extraction without translating it here: a
// caption becomes the screenshot's alt text, which the site translates
// per locale like any other prose (see marketing/gen's screenshot
// function, which marks the alt attribute).
func N_(s string) string { return s }

// Theme is one of the two variants every shot is captured in.
type Theme string

const (
	Light Theme = "light"
	Dark  Theme = "dark"
)

// Themes is every theme a shot is captured in, in a stable order.
var Themes = []Theme{Light, Dark}

// Interaction is one step performed after the page loads and before the
// screenshot is taken, to reach the state worth showing.
type Interaction struct {
	// Click is a CSS selector to click, if set.
	Click string
	// Type fills the element at Selector with Text, if Selector is set.
	Selector string
	Text     string
	// WaitFor is a selector that must appear before the next step runs.
	WaitFor string
}

// Shot is one captured view.
type Shot struct {
	// Name is the image's base name, without theme suffix or extension.
	// It is what a page template references.
	Name string
	// Path is the console path to visit, including any query string.
	Path string
	// Caption describes what the console is showing, and becomes the
	// image's alt text. It must describe the content, not name the page:
	// "Fleet dashboard with 26 nodes ..." rather than "Dashboard".
	Caption string
	// ReadyWhen is a CSS selector that exists only once the view's data
	// has rendered. Capture waits for it rather than for a fixed delay,
	// and fails if it never appears - which is what stops an empty or
	// still-loading page being published as a screenshot.
	ReadyWhen string
	// MustContain is text the rendered page must include. It guards
	// against a view that renders its container but with an empty state
	// inside, which ReadyWhen alone would accept.
	//
	// It must be text only the demo fleet produces. A generic word -
	// a status like "unchanged", or a common package name - is also
	// produced by whatever else happens to be in a development
	// database, so the check passes while the screenshot shows
	// somebody's leftover test data. Prefer a demo hostname, user or
	// group name.
	MustContain string
	// Interactions are performed in order before capture.
	Interactions []Interaction
	// Width and Height are the viewport in CSS pixels.
	Width, Height int
	// FullPage captures the whole scrollable page rather than the
	// viewport.
	//
	// Off by default, and worth leaving off. A full-page capture of a
	// console view with a long table is two thousand pixels tall; shown
	// at a page's width it becomes an unreadable wall that pushes
	// everything after it off the screen. A viewport capture is a
	// representative view, which is what a marketing page wants.
	FullPage bool
}

// defaultWidth and defaultHeight are the viewport a shot gets unless it
// asks for another. 1440x900 is a common laptop size and wide enough
// that the console's tables do not collapse to their narrow layout.
const (
	defaultWidth  = 1440
	defaultHeight = 900
)

// All is every screenshot the site can show, covering at least one view
// per capability page.
var All = withDefaults([]Shot{
	// --- landing page / nodes ---
	{
		Name:        "dashboard",
		Path:        "/",
		Caption:     N_("The OpenVox Console dashboard showing fleet run status across 26 nodes, with counts of unchanged, changed and failed runs, recent jobs and recent activity"),
		ReadyWhen:   "#status-summary vox-stat",
		MustContain: "prod.example.com",
	},
	{
		Name:        "nodes-list",
		Path:        "/nodes.html",
		Caption:     N_("The node inventory listing managed nodes with their connection state, certificate status and last report time"),
		ReadyWhen:   "#results tbody tr",
		MustContain: "prod.example.com",
	},
	{
		Name:      "node-detail",
		Path:      "/node.html?name=web-03.prod.example.com",
		Caption:   N_("A single node's detail page showing its facts, recent Puppet runs and the classes applied to it"),
		ReadyWhen: "#facts tbody tr",
		// The heading alone would be satisfied by the certname echoed
		// from the query string, which a node the console knows nothing
		// about would also produce. A fact row means openvoxdb answered.
		MustContain: "web-03.prod.example.com",
	},

	// --- classification ---
	{
		Name:        "groups-list",
		Path:        "/groups.html",
		Caption:     N_("Node groups listed with their environment, priority and the number of nodes each rule currently matches"),
		ReadyWhen:   "#results tbody tr",
		MustContain: "Web Tier",
	},

	// --- code deployment ---
	{
		Name:      "code-deploys",
		Path:      "/code.html",
		Caption:   N_("Code deployment history showing successful deploys from two separate control repositories, each with the source it came from, the environment it wrote, its ref, who triggered it and when it ran"),
		ReadyWhen: "#code-tabs",
		// The Code page opens on Repositories; deploy history is the
		// second tab. Without this the screenshot shows the repository
		// overview under a caption describing deploy history.
		Interactions: []Interaction{
			{Click: `vox-tab[panel="deploys"]`, WaitFor: "#deploy-results tbody tr"},
		},
		MustContain: "team_a_production",
	},

	// --- orchestration ---
	{
		Name:        "jobs-list",
		Path:        "/jobs.html",
		Caption:     N_("Orchestration job history listing ad-hoc runs, tasks and plans with the nodes each targeted and whether it succeeded"),
		ReadyWhen:   "#results tbody tr",
		MustContain: "o.operator",
	},

	// --- packages and vulnerabilities ---
	{
		Name:        "packages",
		Path:        "/packages.html",
		Caption:     N_("Fleet-wide package inventory showing each package, its versions in use and how many nodes report it"),
		ReadyWhen:   "#results tbody tr",
		MustContain: "haproxy",
	},
	{
		Name:        "vulnerabilities",
		Path:        "/vulnerabilities.html",
		Caption:     N_("Vulnerability findings across the fleet, ordered by severity, showing affected node counts and whether a fix is available"),
		ReadyWhen:   "#results tbody tr",
		MustContain: "CVE-2024-31449",
	},

	// --- access control and audit ---
	{
		Name:        "roles",
		Path:        "/roles.html",
		Caption:     N_("Role-based access control showing roles and the specific permissions each one grants"),
		ReadyWhen:   "#results tbody tr",
		MustContain: "Auditor",
	},
	{
		Name:        "activity",
		Path:        "/activity.html",
		Caption:     N_("The audit trail recording who changed what in the console and when"),
		ReadyWhen:   "#results tbody tr",
		MustContain: "j.newstarter",
	},
})

// withDefaults fills in the viewport on any shot that did not set one.
func withDefaults(in []Shot) []Shot {
	for i := range in {
		if in[i].Width == 0 {
			in[i].Width = defaultWidth
		}
		if in[i].Height == 0 {
			in[i].Height = defaultHeight
		}
	}
	return in
}

// FileName is the image file a shot produces for one theme and locale,
// relative to the screenshot asset directory.
//
// English keeps the unsuffixed name it has always had. That is not
// cosmetic: those twenty files are committed, and giving them a "-en"
// suffix would rewrite every one of them in the history for no change
// in content, and break any link to them from outside this repository.
func (s Shot) FileName(theme Theme, locale string) string {
	name := s.Name + "-" + string(theme)
	if locale != "" && locale != "en" {
		name += "-" + locale
	}
	return name + ".png"
}

// Find returns the shot with the given name.
func Find(name string) (Shot, bool) {
	for _, s := range All {
		if s.Name == name {
			return s, true
		}
	}
	return Shot{}, false
}

// AssetDir is where captured images live, relative to the marketing
// directory. The site generator reads from here and capture writes here.
const AssetDir = "assets/screenshots"

// MaxBytes bounds one captured PNG.
//
// These are committed files that are replaced on every refresh, so an
// unbounded size grows the repository's history every time. 1.5MB is
// generous for a full-page console screenshot after lossless
// optimisation and small enough that a runaway image is noticed.
const MaxBytes = 1_500_000
