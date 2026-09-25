// Command gen renders the marketing site's static pages from
// marketing/templates/layout.html.tmpl (the shared chrome) plus one
// marketing/templates/pages/*.tmpl per page.
//
// It mirrors frontend/gen deliberately: a contributor who has edited the
// console's pages should not have to learn a second toolchain to edit
// these. Invoked by marketing/build.sh; not part of any shipped binary.
//
// It also enforces the link between the site and the screenshot
// manifest. A page asks for an image by the name marketing/shots
// declares, and gen fails the build if that name is not declared or its
// file is missing - so a typo or a missing capture is a build failure
// here rather than a broken image on a published page.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/voxpupuli/enterprise-console/marketing/guides"
	"github.com/voxpupuli/enterprise-console/marketing/shots"
)

// N_ marks a string for extraction without translating it here.
//
// The page table below is evaluated at package load, long before a
// catalogue is chosen, so the lookup happens later - translateHTML
// resolves these once they have been rendered into the page. Without
// the marker they would be invisible to the extractor and would stay
// English on every translated page, which is exactly the failure this
// whole convention exists to prevent.
func N_(s string) string { return s }

// page is one rendered output file.
type page struct {
	// Name is the output path relative to the output directory.
	Name string
	// Title is the <title>, and the text of the page's nav entry.
	Title string
	// NavLabel is the (shorter) text used in the capability subnav.
	NavLabel string
	// Summary is the meta description.
	Summary string
	// Capability marks which subnav entry is current; empty on the
	// landing page, which is not in the subnav.
	Capability string
	// Guide is set on a guide page, which renders from Markdown through
	// templates/guide.html.tmpl rather than from a page template.
	Guide *guides.Guide
	// DoIt names, by slug, the guides that carry out what a feature page
	// describes - rendered as its "Do it" links. A slug that names no
	// guide fails the build.
	DoIt []string
}

// pages is the whole site. The order here is the order of the subnav.
var pages = []page{
	{
		Name:    "index.html",
		Title:   N_("OpenVox Console"),
		Summary: N_("A Puppet Enterprise console equivalent for OpenVox: node inventory, classification, code deployment, orchestration, package and vulnerability tracking, and role-based access control - in a single Go binary."),
	},
	{
		Name:       "features/nodes.html",
		Title:      N_("Nodes and inventory - OpenVox Console"),
		NavLabel:   N_("Nodes"),
		Summary:    N_("See every managed node, what it reported, whether it is reachable and the state of its certificate."),
		Capability: "nodes",
		DoIt:       []string{"add", "certificates", "remove"},
	},
	{
		Name:       "features/classification.html",
		Title:      N_("Classification - OpenVox Console"),
		NavLabel:   N_("Classification"),
		Summary:    N_("Decide what configuration applies to which nodes with rule-based node groups, served to OpenVox over the standard ENC contract."),
		Capability: "classification",
		DoIt:       []string{"classify"},
	},
	{
		Name:       "features/code.html",
		Title:      N_("Code deployment - OpenVox Console"),
		NavLabel:   N_("Code"),
		Summary:    N_("Deploy Puppet code from one or many control repositories, and see which nodes each repository's code actually reaches."),
		Capability: "code",
		DoIt:       []string{"deploy-code"},
	},
	{
		Name:       "features/orchestration.html",
		Title:      N_("Orchestration - OpenVox Console"),
		NavLabel:   N_("Orchestration"),
		Summary:    N_("Run Puppet on demand across the fleet, and see what each node did."),
		Capability: "orchestration",
		DoIt:       []string{"run-jobs"},
	},
	{
		Name:       "features/security.html",
		Title:      N_("Packages and vulnerabilities - OpenVox Console"),
		NavLabel:   N_("Security"),
		Summary:    N_("Know what is installed across the fleet and which known vulnerabilities affect it."),
		Capability: "security",
		DoIt:       []string{"vulnerabilities"},
	},
	{
		Name:       "features/access-control.html",
		Title:      N_("Access control and audit - OpenVox Console"),
		NavLabel:   N_("Access control"),
		Summary:    N_("Single sign-on through your identity provider, role-based access control, service tokens, and an audit trail of who changed what."),
		Capability: "access-control",
		DoIt:       []string{"access"},
	},
	{
		Name:       "features/scaling.html",
		Title:      N_("Scaling - OpenVox Console"),
		NavLabel:   N_("Scaling"),
		Summary:    N_("One binary that runs as a single instance or a clustered fleet of web, worker, orchestrator and ENC instances."),
		Capability: "scaling",
		DoIt:       []string{"run-modes", "cluster"},
	},
	{
		Name:    "guides/index.html",
		Title:   N_("Guides - OpenVox Console"),
		Summary: N_("Step-by-step guides to installing, scaling, using and operating OpenVox Console."),
	},
}

// guidesRoot is where the guide sources live, relative to marketing/.
const guidesRoot = "guides"

// sitePages is every page in the build: the fixed table above, then one
// page per guide, in guide order.
func sitePages(sections []guides.Section) []page {
	out := append([]page(nil), pages...)
	for _, sec := range sections {
		for _, g := range sec.Guides {
			out = append(out, page{Name: g.PageName(), Title: g.Title, Summary: g.Summary, Guide: g})
		}
	}
	return out
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	locale := flag.String("locale", "en", "locale to render (en renders at the site root)")
	catPath := flag.String("catalogue", "", "compiled JSON catalogue for -locale; omit for English")
	dir := flag.String("dir", "ltr", "text direction for -locale: ltr or rtl")
	flag.Parse()

	if flag.NArg() != 1 {
		return fmt.Errorf("usage: gen [-locale xx] [-catalogue path] [-dir ltr|rtl] <site-root>")
	}
	siteRoot := flag.Arg(0)

	cat := englishCatalogue()
	if *locale != "en" {
		if *catPath == "" {
			return fmt.Errorf("-locale %s needs -catalogue", *locale)
		}
		loaded, err := loadCatalogue(*catPath, *locale, *dir)
		if err != nil {
			return err
		}
		cat = loaded
	}

	// The screenshots live inside the site root rather than being copied
	// into it, so this is both where they are checked and where the
	// published pages will reference them from. Checked against the
	// locale being built: a German page points at German captures.
	assetDir := filepath.Join(siteRoot, shots.AssetDir)
	if err := verifyScreenshots(assetDir, *locale); err != nil {
		return err
	}

	// English at the root, every other locale one directory down. Assets
	// (CSS, scripts, screenshots) stay at the root and are shared, so a
	// locale costs its pages and nothing else.
	outDir := siteRoot
	if *locale != "en" {
		outDir = filepath.Join(siteRoot, *locale)
	}

	layoutPath := filepath.Join("templates", "layout.html.tmpl")

	sections, err := guides.Load(guidesRoot)
	if err != nil {
		return err
	}
	settings, err := loadSettings(settingsSource, composeSource)
	if err != nil {
		return err
	}
	site := &siteData{sections: sections, all: sitePages(sections), settings: settings}

	// Every page is rendered before any is written: the link check needs
	// the whole locale, and a failure should leave no half-built site.
	rendered := make([]renderedPage, 0, len(site.all))
	var guideUnits, guideMissing int
	for _, p := range site.all {
		out, stats, err := renderPage(p, site, layoutPath, *locale, cat)
		if err != nil {
			return err
		}
		guideUnits += stats.units
		guideMissing += stats.missing
		rendered = append(rendered, renderedPage{page: p, html: out})
	}

	if err := checkLinks(rendered, siteRoot, *locale); err != nil {
		return err
	}

	for _, r := range rendered {
		outPath := filepath.Join(outDir, r.page.Name)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(outPath), err)
		}
		if err := os.WriteFile(outPath, r.html, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", outPath, err)
		}
	}

	fmt.Printf("Generated %d pages into %s (%s)", len(rendered), outDir, *locale)
	if *locale != "en" && guideUnits > 0 {
		fmt.Printf(" - guides %d/%d passages translated", guideUnits-guideMissing, guideUnits)
	}
	fmt.Println()
	return nil
}

// siteData is what every page's rendering can see of the whole build.
type siteData struct {
	sections []guides.Section
	all      []page
	settings map[string]bool
}

// renderedPage is one page, rendered and translated, not yet written.
type renderedPage struct {
	page page
	html []byte
}

// verifyScreenshots checks that every declared shot has both its images
// on disk before any page is rendered.
//
// Failing here, before writing anything, is the point: a missing image
// is a broken picture on a published page, and the person who notices is
// a visitor.
func verifyScreenshots(assetDir, locale string) error {
	var missing []string
	for _, shot := range shots.All {
		for _, theme := range shots.Themes {
			path := filepath.Join(assetDir, shot.FileName(theme, locale))
			if _, err := os.Stat(path); err != nil {
				missing = append(missing, shot.FileName(theme, locale))
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}

	return fmt.Errorf(
		"%d declared screenshot(s) are missing from %s:\n  %s\n\n"+
			"Capture them with `make marketing-screenshots`, or remove the shot from\n"+
			"marketing/shots if the site no longer needs it",
		len(missing), assetDir, strings.Join(missing, "\n  "))
}

// renderPage renders and translates one page, in memory.
func renderPage(p page, site *siteData, layoutPath, locale string, cat *catalogue) ([]byte, translateStats, error) {
	var none translateStats

	pagePath := filepath.Join("templates", "guide.html.tmpl")
	if p.Guide == nil {
		pageTmpl := strings.TrimSuffix(filepath.Base(p.Name), ".html") + ".tmpl"
		pagePath = filepath.Join("templates", "pages", filepath.Dir(p.Name), pageTmpl)
		if filepath.Dir(p.Name) == "." {
			pagePath = filepath.Join("templates", "pages", pageTmpl)
		}
	}

	funcs := funcsFor(p, locale)
	tmpl := template.New("layout.html.tmpl").Funcs(funcs)
	tmpl, err := tmpl.ParseFiles(layoutPath, pagePath)
	if err != nil {
		return nil, none, fmt.Errorf("parsing templates for %s: %w", p.Name, err)
	}

	data := pageData{
		page:       p,
		Root:       rootFor(p.Name, locale),
		SiteRoot:   rootFor(p.Name, locale),
		LocaleRoot: rootFor(p.Name, "en"),
		Pages:      pages,
		Sections:   site.sections,
		Locale:     locale,
		Locales:    localeLinks(p.Name, locale),
		LocaleName: localeName(locale),
		Direction:  cat.dir,
	}
	if p.Guide != nil {
		view, err := guideViewFor(p.Guide, site, funcs, cat)
		if err != nil {
			return nil, none, err
		}
		data.GuideView = view
		// A guide's <title> is its own title plus the site's, which is
		// not a message of its own - so it is composed here, from the
		// translated guide title, rather than looked up whole.
		data.Title = cat.get(p.Guide.Title) + " - OpenVox Console"
		data.Summary = cat.get(p.Guide.Summary)
	}
	for _, slug := range p.DoIt {
		g := findGuide(site.sections, slug)
		if g == nil {
			return nil, none, fmt.Errorf("%s links guide %q, which does not exist", p.Name, slug)
		}
		data.DoIt = append(data.DoIt, g)
	}

	// Rendered into memory, then translated: the translator needs the
	// whole document, and nothing is written until every page of the
	// locale has rendered and its links check out.
	var rendered bytes.Buffer
	if err := tmpl.ExecuteTemplate(&rendered, "layout.html.tmpl", data); err != nil {
		return nil, none, fmt.Errorf("rendering %s: %w", p.Name, err)
	}

	translated, stats, err := translateHTML(rendered.Bytes(), cat)
	if err != nil {
		return nil, none, fmt.Errorf("translating %s into %s: %w", p.Name, cat.lang, err)
	}
	return translated, stats, nil
}

// pageData is what a template is executed against.
type pageData struct {
	page
	// LocaleRoot is the relative prefix back to this locale's root -
	// the site root for English, the locale's directory otherwise. Links
	// between pages go through it, so a reader stays in their language;
	// assets, which every locale shares, go through Root.
	LocaleRoot string
	// Sections is the guide set, for the guides index and sidebar.
	Sections []guides.Section
	// GuideView is set on a guide page.
	GuideView *guideView
	// DoIt is the guides a feature page links to.
	DoIt []*guides.Guide
	// Root is the relative prefix back to the site root ("." for a
	// top-level page, ".." for one in features/).
	//
	// Every link and asset reference goes through it, because the site
	// is published under a path prefix on GitHub Pages - a root-relative
	// "/style.css" would resolve to the domain root and 404.
	Root string
	// Pages is the whole site, for rendering navigation.
	Pages []page
	// SiteRoot is Root - kept as a second name because "root" in a
	// template reads as "the site", and for a translated page the two
	// happen to coincide only because assets are not duplicated.
	SiteRoot string
	// Locale is the code being rendered ("en" at the site root).
	Locale string
	// Locales is every locale this page exists in, for the switcher.
	Locales []localeLink
	// LocaleName is the language being rendered, written in itself -
	// the switcher's trigger label. "Deutsch", not "German".
	LocaleName string
	// Direction is "ltr" or "rtl", mirrored onto <html dir>.
	Direction string
}

// localeLink is one entry in the language switcher.
type localeLink struct {
	// Code is the locale, e.g. "de".
	Code string
	// Name is the language in itself - somebody looking for German is
	// looking for "Deutsch".
	Name string
	// Href points at this same page in that locale.
	Href string
	// Current marks the locale being rendered.
	Current bool
}

// siteLocales is every locale the marketing site is built in, in the
// order the switcher lists them.
//
// The first five are the world's most spoken languages, which is the
// coverage this site is meant to have. German, Japanese and Arabic
// follow them because they were already translated and reviewed, and
// because between them they are what proves the pipeline handles
// left-to-right, CJK and right-to-left.
//
// Still shorter than the console's fifteen, and for a reason worth
// keeping in mind before extending it: the console's strings are
// labels, these are argument, and a machine-translated argument reads
// worse than an untranslated one. Every locale here should be reviewed
// by somebody who reads it.
var siteLocales = []struct {
	Code, Name, Dir string
}{
	{"en", "English", "ltr"},
	{"zh", "\u4e2d\u6587", "ltr"},
	{"hi", "\u0939\u093f\u0928\u094d\u0926\u0940", "ltr"},
	{"es", "Espa\u00f1ol", "ltr"},
	{"fr", "Fran\u00e7ais", "ltr"},
	{"de", "Deutsch", "ltr"},
	{"ja", "\u65e5\u672c\u8a9e", "ltr"},
	{"ar", "\u0627\u0644\u0639\u0631\u0628\u064a\u0629", "rtl"},
}

// localeLinks builds the switcher for one page.
func localeLinks(name, current string) []localeLink {
	root := rootFor(name, current)
	out := make([]localeLink, 0, len(siteLocales))
	for _, l := range siteLocales {
		href := root + "/" + filepath.ToSlash(name)
		if l.Code != "en" {
			href = root + "/" + l.Code + "/" + filepath.ToSlash(name)
		}
		out = append(out, localeLink{
			Code: l.Code, Name: l.Name, Href: href, Current: l.Code == current,
		})
	}
	return out
}

// localeName returns a locale's name in itself, for the switcher's
// trigger. An unknown code falls back to the code so a mislabeled
// build is visibly wrong rather than silently blank.
func localeName(code string) string {
	for _, l := range siteLocales {
		if l.Code == code {
			return l.Name
		}
	}
	return code
}

// rootFor returns the relative path from a page back to the SITE root -
// not the locale's directory.
//
// Assets are shared across locales and live at the site root, so a
// translated page has to climb one extra level: de/features/nodes.html
// reaches site.css via ../../site.css, where the English
// features/nodes.html needs only ../.
func rootFor(name, locale string) string {
	depth := strings.Count(filepath.ToSlash(name), "/")
	if locale != "" && locale != "en" {
		depth++
	}
	if depth == 0 {
		return "."
	}
	return strings.TrimSuffix(strings.Repeat("../", depth), "/")
}

// funcsFor builds the template functions available to one page. They are
// per-page because screenshot paths are relative to the page's depth.
func funcsFor(p page, locale string) template.FuncMap {
	root := rootFor(p.Name, locale)

	return template.FuncMap{
		// screenshot renders one declared shot as a theme-aware
		// <picture>. It is a build error to ask for a name the manifest
		// does not declare.
		"screenshot": func(name string) (template.HTML, error) {
			shot, ok := shots.Find(name)
			if !ok {
				declared := make([]string, 0, len(shots.All))
				for _, s := range shots.All {
					declared = append(declared, s.Name)
				}
				return "", fmt.Errorf("page %s asks for screenshot %q, which marketing/shots does not declare (declared: %s)",
					p.Name, name, strings.Join(declared, ", "))
			}

			dark := root + "/" + shots.AssetDir + "/" + shot.FileName(shots.Dark, locale)
			light := root + "/" + shots.AssetDir + "/" + shot.FileName(shots.Light, locale)

			// <picture> with a prefers-color-scheme source, rather than
			// a script that swaps src: this works with JavaScript
			// disabled and cannot flash the wrong theme on load. The
			// theme toggle's override is layered on top of it by
			// theme.js.
			//
			// Deliberately not loading="lazy". There are one to three
			// of these per page, so deferring them saves nothing worth
			// having, and a lazy image below the fold is simply absent
			// from anything that renders the page without scrolling -
			// including the screenshots taken to review this site,
			// where it showed up as a large empty panel.
			// The picture is wrapped in a link to the full-size image, so
			// a screenshot too small to read in place can be opened: with
			// JavaScript, in a viewer on this page (lightbox.js); without
			// it, as the image itself. The link's name is the caption plus
			// what following it does.
			html := fmt.Sprintf(`<figure class="shot">
  <a class="shot-link" href="%s">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="%s" data-theme-dark="%s" data-theme-light="%s" />
      <img src="%s" alt="%s" width="%d" data-i18n-attr="alt" />
    </picture>
    <span class="vox-sr-only" data-i18n>Open full size</span>
  </a>
</figure>`,
				template.HTMLEscapeString(light),
				template.HTMLEscapeString(dark),
				template.HTMLEscapeString(dark),
				template.HTMLEscapeString(light),
				template.HTMLEscapeString(light),
				template.HTMLEscapeString(shot.Caption),
				shot.Width)
			return template.HTML(html), nil
		},

		// asset resolves a path relative to the site root.
		"asset": func(path string) string {
			return root + "/" + strings.TrimPrefix(path, "/")
		},
	}
}

// findGuide returns the guide with slug, or nil.
func findGuide(sections []guides.Section, slug string) *guides.Guide {
	for _, sec := range sections {
		for _, g := range sec.Guides {
			if g.Slug == slug {
				return g
			}
		}
	}
	return nil
}
