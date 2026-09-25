package main

import (
	"fmt"
	"html/template"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/voxpupuli/enterprise-console/marketing/guides"
)

// guideView is what templates/guide.html.tmpl renders a guide from.
type guideView struct {
	*guides.Guide
	// Section is the section the guide belongs to, for the sidebar's
	// open group and the series navigation's label.
	Section guides.Section
	// Body is the rendered guide, still carrying its translation
	// markers - the page is translated whole, after rendering.
	Body template.HTML
	// TOC is the guide's h2s, each with its h3s.
	TOC []tocEntry
	// Prev and Next are the neighboring guides within the section.
	Prev, Next *guides.Guide
}

// tocEntry is one line of a guide's table of contents.
type tocEntry struct {
	ID       string
	HTML     template.HTML
	Children []tocEntry
}

// guideViewFor renders g's body and assembles everything around it. It
// is also where a guide is checked against the console: a setting it
// names that the console does not read fails the build here.
func guideViewFor(g *guides.Guide, site *siteData, funcs template.FuncMap, cat *catalogue) (*guideView, error) {
	shot := funcs["screenshot"].(func(string) (template.HTML, error))
	r, err := guides.Render(guidesRoot, g, guides.Options{
		Shot: func(name string) (string, error) {
			h, err := shot(name)
			return string(h), err
		},
	})
	if err != nil {
		return nil, err
	}

	if unknown := unknownSettings(r.HTML, site.settings); len(unknown) > 0 {
		return nil, fmt.Errorf("guide %s names setting(s) neither the console nor its compose file reads:\n  %s\n\n"+
			"Settings are checked against %s and %s. Rename them in the guide, or remove them if they no longer exist",
			g.Path, strings.Join(unknown, "\n  "), settingsSource, composeSource)
	}

	view := &guideView{Guide: g, Body: template.HTML(r.HTML)}
	for _, sec := range site.sections {
		for i, other := range sec.Guides {
			if other != g {
				continue
			}
			view.Section = sec
			if i > 0 {
				view.Prev = sec.Guides[i-1]
			}
			if i+1 < len(sec.Guides) {
				view.Next = sec.Guides[i+1]
			}
		}
	}

	for _, h := range r.Headings {
		entry := tocEntry{ID: h.ID, HTML: template.HTML(h.HTML)}
		if h.Level == 3 && len(view.TOC) > 0 {
			last := &view.TOC[len(view.TOC)-1]
			last.Children = append(last.Children, entry)
			continue
		}
		view.TOC = append(view.TOC, entry)
	}
	return view, nil
}

// settingsSource is where the console declares every setting it reads,
// and composeSource the production compose file, whose own ${...}
// variables share the CONSOLE_ prefix (CONSOLE_IMAGE_TAG,
// CONSOLE_CERTS_DIR) without being console settings. Both relative to
// marketing/.
const (
	settingsSource = "../internal/runtime/config.go"
	composeSource  = "../docker-compose.yml"
)

var (
	settingLiteral = regexp.MustCompile(`"(CONSOLE_[A-Z0-9_]+)"`)
	composeVar     = regexp.MustCompile(`\$\{(CONSOLE_[A-Z0-9_]+)`)
	settingMention = regexp.MustCompile(`CONSOLE_[A-Z0-9_]+`)
)

// loadSettings reads every CONSOLE_* name a guide may legitimately
// mention: each quoted name in the console's configuration code, and
// each variable the compose file interpolates.
//
// A textual scan of the one file where settings are read, rather than an
// import of the runtime package: gen would otherwise pull in the
// console's dependency tree to learn a list of names, and a setting read
// anywhere else is itself worth this check surfacing.
func loadSettings(configFile, composeFile string) (map[string]bool, error) {
	names := map[string]bool{}
	for _, src := range []struct {
		file string
		re   *regexp.Regexp
	}{{configFile, settingLiteral}, {composeFile, composeVar}} {
		data, err := os.ReadFile(src.file)
		if err != nil {
			return nil, fmt.Errorf("read settings from %s: %w", src.file, err)
		}
		found := src.re.FindAllStringSubmatch(string(data), -1)
		if len(found) == 0 {
			return nil, fmt.Errorf("found no CONSOLE_* settings in %s - has configuration moved?", src.file)
		}
		for _, m := range found {
			names[m[1]] = true
		}
	}
	return names, nil
}

// unknownSettings returns each CONSOLE_* name in text that the console
// does not read. NAME_FILE is accepted for any known NAME (every setting
// can be read from a file), and a name ending in "_" - prose such as
// "the CONSOLE_CLUSTER_* settings" - is accepted when some setting
// starts with it.
func unknownSettings(text string, known map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range settingMention.FindAllString(text, -1) {
		if seen[name] {
			continue
		}
		seen[name] = true
		if known[name] || known[strings.TrimSuffix(name, "_FILE")] {
			continue
		}
		if strings.HasSuffix(name, "_") && hasPrefix(known, name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func hasPrefix(known map[string]bool, prefix string) bool {
	for k := range known {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// linkAttrs are the attributes that carry a site-internal target: the
// ordinary ones, and the voxblocks components' own.
var linkAttrs = map[string]bool{
	"href": true, "src": true, "previous-href": true, "next-href": true,
}

// checkLinks resolves every internal link and asset reference on every
// page of one locale's build, failing on the first page with any that
// do not resolve - naming the page and each broken reference.
//
// Links to another locale's copy of a page are checked against this
// locale's page list and anchors: every locale publishes the same pages,
// and a guide's anchors come from its English headings, so they are the
// same in every locale.
func checkLinks(pagesOut []renderedPage, siteRoot, locale string) error {
	prefix := ""
	if locale != "en" {
		prefix = locale + "/"
	}
	locales := map[string]bool{}
	for _, l := range siteLocales {
		if l.Code != "en" {
			locales[l.Code] = true
		}
	}

	ids := map[string]map[string]bool{}
	docs := map[string]*html.Node{}
	for _, r := range pagesOut {
		doc, err := html.Parse(strings.NewReader(string(r.html)))
		if err != nil {
			return fmt.Errorf("link check: parse %s: %w", r.page.Name, err)
		}
		docs[r.page.Name] = doc
		set := map[string]bool{}
		eachNode(doc, func(n *html.Node) {
			if id, ok := attr(n, "id"); ok {
				set[id] = true
			}
		})
		ids[r.page.Name] = set
	}

	for _, r := range pagesOut {
		sitePath := prefix + r.page.Name
		var broken []string
		eachNode(docs[r.page.Name], func(n *html.Node) {
			for _, a := range n.Attr {
				if !linkAttrs[a.Key] {
					continue
				}
				if problem := checkTarget(a.Val, sitePath, r.page.Name, siteRoot, ids, locales); problem != "" {
					broken = append(broken, fmt.Sprintf("%s=%q: %s", a.Key, a.Val, problem))
				}
			}
		})
		if len(broken) > 0 {
			where := r.page.Name
			if r.page.Guide != nil {
				where = r.page.Guide.Path
			}
			return fmt.Errorf("%s (%s) has %d broken link(s):\n  %s", where, locale, len(broken), strings.Join(broken, "\n  "))
		}
	}
	return nil
}

// checkTarget returns why target, found on the page at sitePath, does
// not resolve - or "" when it does.
func checkTarget(target, sitePath, pageName, siteRoot string, ids map[string]map[string]bool, locales map[string]bool) string {
	if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "//") ||
		strings.HasPrefix(target, "mailto:") || strings.HasPrefix(target, "data:") {
		return ""
	}
	file, fragment, _ := strings.Cut(target, "#")
	if file == "" {
		if fragment != "" && !ids[pageName][fragment] {
			return "no element on this page has that id"
		}
		return ""
	}
	if strings.HasPrefix(file, "/") {
		return "root-relative links break under the site's path prefix"
	}

	resolved := path.Clean(path.Join(path.Dir(sitePath), file))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "points outside the site"
	}

	if !strings.HasSuffix(resolved, ".html") {
		if _, err := os.Stat(filepath.Join(siteRoot, filepath.FromSlash(resolved))); err != nil {
			return "no such file in the built site"
		}
		return ""
	}

	name := resolved
	if first, rest, ok := strings.Cut(resolved, "/"); ok && locales[first] {
		name = rest
	}
	pageIDs, ok := ids[name]
	if !ok {
		return "no such page"
	}
	if fragment != "" && !pageIDs[fragment] {
		return fmt.Sprintf("%s has no anchor #%s", name, fragment)
	}
	return ""
}

func eachNode(n *html.Node, fn func(*html.Node)) {
	if n.Type == html.ElementNode {
		fn(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		eachNode(c, fn)
	}
}
