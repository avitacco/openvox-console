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
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/voxpupuli/enterprise-console/marketing/shots"
)

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
}

// pages is the whole site. The order here is the order of the subnav.
var pages = []page{
	{
		Name:    "index.html",
		Title:   "OpenVox Console",
		Summary: "A Puppet Enterprise console equivalent for OpenVox: node inventory, classification, code deployment, orchestration, package and vulnerability tracking, and role-based access control - in a single Go binary.",
	},
	{
		Name:       "features/nodes.html",
		Title:      "Nodes and inventory - OpenVox Console",
		NavLabel:   "Nodes",
		Summary:    "See every managed node, what it reported, whether it is reachable and the state of its certificate.",
		Capability: "nodes",
	},
	{
		Name:       "features/classification.html",
		Title:      "Classification - OpenVox Console",
		NavLabel:   "Classification",
		Summary:    "Decide what configuration applies to which nodes with rule-based node groups, served to OpenVox over the standard ENC contract.",
		Capability: "classification",
	},
	{
		Name:       "features/code.html",
		Title:      "Code deployment - OpenVox Console",
		NavLabel:   "Code",
		Summary:    "Deploy Puppet code from one or many control repositories, with a record of every deploy.",
		Capability: "code",
	},
	{
		Name:       "features/orchestration.html",
		Title:      "Orchestration - OpenVox Console",
		NavLabel:   "Orchestration",
		Summary:    "Run Puppet, tasks and plans on demand across the fleet, and see what each node did.",
		Capability: "orchestration",
	},
	{
		Name:       "features/security.html",
		Title:      "Packages and vulnerabilities - OpenVox Console",
		NavLabel:   "Security",
		Summary:    "Know what is installed across the fleet and which known vulnerabilities affect it.",
		Capability: "security",
	},
	{
		Name:       "features/access-control.html",
		Title:      "Access control and audit - OpenVox Console",
		NavLabel:   "Access control",
		Summary:    "Role-based access control, service tokens, and an audit trail of who changed what.",
		Capability: "access-control",
	},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: gen <output-dir>")
	}
	outDir := os.Args[1]

	assetDir := filepath.Join("..", shots.AssetDir)
	if _, err := os.Stat(shots.AssetDir); err == nil {
		assetDir = shots.AssetDir
	}

	if err := verifyScreenshots(assetDir); err != nil {
		return err
	}

	layoutPath := filepath.Join("templates", "layout.html.tmpl")

	for _, p := range pages {
		if err := renderPage(p, layoutPath, outDir); err != nil {
			return err
		}
	}

	fmt.Printf("Generated %d pages into %s\n", len(pages), outDir)
	return nil
}

// verifyScreenshots checks that every declared shot has both its images
// on disk before any page is rendered.
//
// Failing here, before writing anything, is the point: a missing image
// is a broken picture on a published page, and the person who notices is
// a visitor.
func verifyScreenshots(assetDir string) error {
	var missing []string
	for _, shot := range shots.All {
		for _, theme := range shots.Themes {
			path := filepath.Join(assetDir, shot.FileName(theme))
			if _, err := os.Stat(path); err != nil {
				missing = append(missing, shot.FileName(theme))
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

// renderPage renders one page.
func renderPage(p page, layoutPath, outDir string) error {
	pageTmpl := strings.TrimSuffix(filepath.Base(p.Name), ".html") + ".tmpl"
	pagePath := filepath.Join("templates", "pages", filepath.Dir(p.Name), pageTmpl)
	if filepath.Dir(p.Name) == "." {
		pagePath = filepath.Join("templates", "pages", pageTmpl)
	}

	tmpl := template.New("layout.html.tmpl").Funcs(funcsFor(p))
	tmpl, err := tmpl.ParseFiles(layoutPath, pagePath)
	if err != nil {
		return fmt.Errorf("parsing templates for %s: %w", p.Name, err)
	}

	outPath := filepath.Join(outDir, p.Name)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(outPath), err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", outPath, err)
	}
	defer f.Close()

	data := pageData{page: p, Root: rootFor(p.Name), Pages: pages}
	if err := tmpl.ExecuteTemplate(f, "layout.html.tmpl", data); err != nil {
		return fmt.Errorf("rendering %s: %w", p.Name, err)
	}
	return nil
}

// pageData is what a template is executed against.
type pageData struct {
	page
	// Root is the relative prefix back to the site root ("." for a
	// top-level page, ".." for one in features/).
	//
	// Every link and asset reference goes through it, because the site
	// is published under a path prefix on GitHub Pages - a root-relative
	// "/style.css" would resolve to the domain root and 404.
	Root string
	// Pages is the whole site, for rendering navigation.
	Pages []page
}

// rootFor returns the relative path from a page back to the site root.
func rootFor(name string) string {
	depth := strings.Count(filepath.ToSlash(name), "/")
	if depth == 0 {
		return "."
	}
	return strings.TrimSuffix(strings.Repeat("../", depth), "/")
}

// funcsFor builds the template functions available to one page. They are
// per-page because screenshot paths are relative to the page's depth.
func funcsFor(p page) template.FuncMap {
	root := rootFor(p.Name)

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

			dark := root + "/" + shots.AssetDir + "/" + shot.FileName(shots.Dark)
			light := root + "/" + shots.AssetDir + "/" + shot.FileName(shots.Light)

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
			html := fmt.Sprintf(`<figure class="shot">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="%s" data-theme-dark="%s" data-theme-light="%s" />
    <img src="%s" alt="%s" width="%d" />
  </picture>
</figure>`,
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
