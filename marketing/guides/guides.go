// Package guides loads and renders the site's step-by-step guides.
//
// A guide is a Markdown file under marketing/guides/<section>/<slug>.md
// with a short front matter block. This package is the only Markdown
// implementation in the tree, and it has exactly two callers:
// marketing/gen, which renders guides into pages, and frontend/i18n,
// which extracts their translatable text. Both go through Render, so the
// strings offered to translators are, by construction, the strings the
// site later looks up - see Messages and Key.
//
// Build-time only. Nothing here is part of any shipped binary, and a
// test in internal/demodata asserts that.
package guides

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// N_ marks a string for extraction without translating it here. The
// extractor finds N_("...") in any .go file under a source root, which
// is how labels this package puts into rendered pages - callout
// headings, the code block's copy labels, section titles - reach the
// catalogues.
func N_(s string) string { return s }

// PartialsDir is the directory, relative to the guides root, holding
// fragments included into more than one guide. It is not a section and
// its files are not guides.
const PartialsDir = "_partials"

// SectionDef is one group of guides, in the order the site lists them.
type SectionDef struct {
	// Name is the directory under the guides root, and the anchor the
	// guides landing page gives the section.
	Name string
	// Title is the section's heading.
	Title string
}

// Sections is the guide set's fixed organization. A directory under the
// guides root that is not listed here is an error rather than a silently
// unlisted section.
var Sections = []SectionDef{
	{Name: "install", Title: N_("Install")},
	{Name: "nodes", Title: N_("Nodes")},
	{Name: "scale", Title: N_("Scale")},
	{Name: "use", Title: N_("Use")},
	{Name: "operate", Title: N_("Operate")},
}

// Meta is a guide's front matter.
type Meta struct {
	Title   string
	Summary string
	// Order sorts guides within their section, ascending.
	Order int
}

// Guide is one guide's identity and metadata.
type Guide struct {
	// Section is the SectionDef.Name the guide belongs to.
	Section string
	// Slug is the file name without its extension. Guide pages are
	// published flat, at guides/<slug>.html, so slugs are unique across
	// the whole guide set.
	Slug string
	// Path is the source file.
	Path string
	Meta
}

// PageName is the guide's output path relative to the site root.
func (g *Guide) PageName() string { return "guides/" + g.Slug + ".html" }

// Section is a SectionDef with the guides found in it.
type Section struct {
	SectionDef
	Guides []*Guide
}

// Load finds every guide under root, grouped by section in Sections
// order, each section's guides in Order order. A section with no guides
// is omitted.
func Load(root string) ([]Section, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read guides root: %w", err)
	}
	// testdata holds this package's own test fixtures, which live beside
	// the real guides.
	known := map[string]bool{PartialsDir: true, "testdata": true}
	for _, s := range Sections {
		known[s.Name] = true
	}
	for _, e := range entries {
		if e.IsDir() && !known[e.Name()] {
			return nil, fmt.Errorf("%s: directory %q is not a guide section (sections: %s)",
				root, e.Name(), sectionNames())
		}
	}

	slugs := map[string]string{}
	var out []Section
	for _, def := range Sections {
		files, err := filepath.Glob(filepath.Join(root, def.Name, "*.md"))
		if err != nil {
			return nil, err
		}
		sec := Section{SectionDef: def}
		for _, path := range files {
			meta, _, err := readFile(path)
			if err != nil {
				return nil, err
			}
			slug := strings.TrimSuffix(filepath.Base(path), ".md")
			if prev, dup := slugs[slug]; dup {
				return nil, fmt.Errorf("%s: slug %q is also used by %s; guides are published flat, so slugs must be unique", path, slug, prev)
			}
			slugs[slug] = path
			sec.Guides = append(sec.Guides, &Guide{Section: def.Name, Slug: slug, Path: path, Meta: meta})
		}
		sort.SliceStable(sec.Guides, func(i, j int) bool {
			if sec.Guides[i].Order != sec.Guides[j].Order {
				return sec.Guides[i].Order < sec.Guides[j].Order
			}
			return sec.Guides[i].Slug < sec.Guides[j].Slug
		})
		if len(sec.Guides) > 0 {
			out = append(out, sec)
		}
	}
	return out, nil
}

func sectionNames() string {
	names := make([]string, len(Sections))
	for i, s := range Sections {
		names[i] = s.Name
	}
	return strings.Join(names, ", ")
}

// readFile splits a guide into its front matter and body. The body is
// returned with its line numbers intact relative to the file: front
// matter lines are blanked rather than removed, so a line reported
// against the body is a line in the file.
func readFile(path string) (Meta, []byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Meta{}, nil, err
	}
	return parseFrontMatter(path, data)
}

// parseFrontMatter reads a leading block of "key: value" lines between
// two "---" lines. Only title, summary and order are accepted; anything
// else is an error, so a misspelt key cannot quietly do nothing.
func parseFrontMatter(path string, data []byte) (Meta, []byte, error) {
	var meta Meta
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return meta, nil, fmt.Errorf("%s:1: a guide must start with front matter (---)", path)
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return meta, nil, fmt.Errorf("%s:%d: front matter line is not key: value", path, i+1)
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "title":
			meta.Title = value
		case "summary":
			meta.Summary = value
		case "order":
			n, err := strconv.Atoi(value)
			if err != nil {
				return meta, nil, fmt.Errorf("%s:%d: order must be a number", path, i+1)
			}
			meta.Order = n
		default:
			return meta, nil, fmt.Errorf("%s:%d: unknown front matter key %q (title, summary, order)", path, i+1, key)
		}
	}
	if end < 0 {
		return meta, nil, fmt.Errorf("%s: front matter is not closed with ---", path)
	}
	if meta.Title == "" || meta.Summary == "" {
		return meta, nil, fmt.Errorf("%s: front matter needs both title and summary", path)
	}

	var body bytes.Buffer
	for i, l := range lines {
		if i <= end {
			// Keep the line count, drop the content.
			if strings.HasSuffix(l, "\n") {
				body.WriteByte('\n')
			}
			continue
		}
		body.WriteString(l)
	}
	return meta, body.Bytes(), nil
}

// origin is where one line of an expanded guide came from.
type origin struct {
	file string
	line int
}

// includePrefix and includeSuffix delimit the include directive. An
// HTML comment, so it is invisible when the file is viewed on GitHub and
// cannot be mistaken for content.
const (
	includePrefix = "<!-- include:"
	includeSuffix = "-->"
)

// expand replaces every include directive in body with the named
// partial, recursively, returning the expanded text and, for each of its
// lines, where that line came from.
func expand(root, file string, body []byte, stack []string) ([]byte, []origin, error) {
	for _, s := range stack {
		if s == file {
			return nil, nil, fmt.Errorf("%s: include cycle: %s -> %s", file, strings.Join(stack, " -> "), file)
		}
	}
	stack = append(stack, file)

	var out bytes.Buffer
	var origins []origin
	sc := bufio.NewScanner(bytes.NewReader(body))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, includePrefix) && strings.HasSuffix(trimmed, includeSuffix) {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, includePrefix), includeSuffix))
			if name == "" || strings.ContainsAny(name, `/\`) || !strings.HasSuffix(name, ".md") {
				return nil, nil, fmt.Errorf("%s:%d: include names a file in %s, like <!-- include: name.md -->", file, n, PartialsDir)
			}
			path := filepath.Join(root, PartialsDir, name)
			partial, err := os.ReadFile(path)
			if err != nil {
				return nil, nil, fmt.Errorf("%s:%d: include %s: %w", file, n, name, err)
			}
			text, o, err := expand(root, path, partial, stack)
			if err != nil {
				return nil, nil, err
			}
			out.Write(text)
			if len(text) > 0 && text[len(text)-1] != '\n' {
				out.WriteByte('\n')
			}
			origins = append(origins, o...)
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
		origins = append(origins, origin{file: file, line: n})
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", file, err)
	}
	return out.Bytes(), origins, nil
}
