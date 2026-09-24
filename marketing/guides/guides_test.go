package guides

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

var update = flag.Bool("update", false, "rewrite golden files")

const fixtureRoot = "testdata/tree"

func fixtureShot(name string) (string, error) {
	if name != "dashboard" {
		return "", fmt.Errorf("screenshot %q is not declared", name)
	}
	return `<figure class="shot"><img src="dashboard.png" alt="The dashboard"/></figure>`, nil
}

func guide(t *testing.T, section, slug string) *Guide {
	t.Helper()
	secs, err := Load(fixtureRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, s := range secs {
		for _, g := range s.Guides {
			if s.Name == section && g.Slug == slug {
				return g
			}
		}
	}
	t.Fatalf("no guide %s/%s", section, slug)
	return nil
}

func TestLoad_SectionsAndOrder(t *testing.T) {
	secs, err := Load(fixtureRoot)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var got []string
	for _, s := range secs {
		for _, g := range s.Guides {
			got = append(got, s.Name+"/"+g.Slug)
		}
	}
	want := []string{"install/plan", "install/containers", "nodes/add"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("guides = %v, want %v (Sections order, then front matter order)", got, want)
	}
	if g := secs[0].Guides[1]; g.Title != "Install with containers" || g.PageName() != "guides/containers.html" {
		t.Errorf("metadata = %+v, page %s", g.Meta, g.PageName())
	}
}

func TestLoad_Refusals(t *testing.T) {
	cases := map[string]map[string]string{
		"unknown section": {"misc/a.md": validFront + "x\n"},
		"duplicate slug":  {"install/a.md": validFront + "x\n", "nodes/a.md": validFront + "x\n"},
		"no front matter": {"install/a.md": "## hi\n"},
		"unknown key":     {"install/a.md": "---\ntitle: T\nsummary: S\nauthor: me\n---\n"},
		"missing summary": {"install/a.md": "---\ntitle: T\n---\n"},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for rel, body := range files {
				writeFile(t, filepath.Join(root, rel), body)
			}
			if _, err := Load(root); err == nil {
				t.Errorf("Load accepted a tree with %s", name)
			}
		})
	}
}

const validFront = "---\ntitle: T\nsummary: S\n---\n"

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Golden files pin the whole rendering: block marking, code blocks,
// callouts, tabs, screenshots, anchors and partials. Run with -update to
// rewrite them after a deliberate change, and read the diff.
func TestRender_Golden(t *testing.T) {
	for _, g := range []*Guide{guide(t, "install", "containers"), guide(t, "nodes", "add"), guide(t, "install", "plan")} {
		t.Run(g.Slug, func(t *testing.T) {
			r, err := Render(fixtureRoot, g, Options{Shot: fixtureShot})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			path := filepath.Join("testdata", "golden", g.Slug+".html")
			if *update {
				writeFile(t, path, r.HTML)
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if r.HTML != string(want) {
				t.Errorf("rendering of %s differs from %s:\n--- got\n%s", g.Slug, path, r.HTML)
			}
		})
	}
}

// Code reaches the page escaped exactly once: what a reader copies out
// of the code block is what the guide's author wrote.
func TestRender_CodeEscapedOnce(t *testing.T) {
	r, err := Render(fixtureRoot, guide(t, "install", "containers"), Options{Shot: fixtureShot})
	if err != nil {
		t.Fatal(err)
	}
	var code string
	eachElement(parse(t, r.HTML), func(n *html.Node) {
		if n.Data == "vox-code-block" && code == "" {
			code = textContent(n)
		}
	})
	want := "docker compose up -d openvoxserver\nprintf '%s' '<password>' > \"secrets/a & b\"\n\necho after a blank line\n"
	if code != want {
		t.Errorf("code block text = %q\n want %q", code, want)
	}
}

// Code is never offered for translation.
func TestRender_CodeBlocksAreNeverMarked(t *testing.T) {
	r, err := Render(fixtureRoot, guide(t, "install", "containers"), Options{Shot: fixtureShot})
	if err != nil {
		t.Fatal(err)
	}
	doc := parse(t, r.HTML)
	eachElement(doc, func(n *html.Node) {
		if n.Data != "vox-code-block" {
			return
		}
		for p := n; p != nil; p = p.Parent {
			if _, ok := getAttr(p, "data-i18n-html"); ok {
				t.Errorf("a code block sits inside a translatable unit <%s>", p.Data)
			}
		}
		if lang, _ := getAttr(n, "language"); lang == "" {
			t.Error("a code block has no language")
		}
	})
	if !strings.Contains(r.HTML, `language="plaintext"`) {
		t.Error("powershell was not mapped to a language vox-code-block accepts")
	}
}

func TestRender_Refusals(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"unknown alert":    {"> [!DANGER]\n> no\n", "unknown alert type"},
		"undeclared shot":  {"![](shot:nope)\n", `"nope" is not declared`},
		"non-shot image":   {"![](x.png)\n", "declared screenshots"},
		"inline image":     {"see ![](shot:dashboard) here\n", "paragraph of its own"},
		"raw html":         {"<div>x</div>\n", "raw HTML"},
		"h1":               {"# Title\n", "start sections at ##"},
		"unknown language": {"```fortran\nx\n```\n", `"fortran"`},
		"duplicate tab":    {"```sh tab=\"A\"\na\n```\n```sh tab=\"A\"\nb\n```\n", "appears twice"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "install", "x.md")
			writeFile(t, path, validFront+"\n"+c.body)
			_, err := Render(root, &Guide{Path: path}, Options{Shot: fixtureShot})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want one containing %q", err, c.want)
			}
			// Every refusal names the file, and a line within it.
			if !strings.Contains(err.Error(), path+":") {
				t.Errorf("error %q does not name %s and a line", err, path)
			}
		})
	}
}

func TestRender_Includes(t *testing.T) {
	r, err := Render(fixtureRoot, guide(t, "install", "containers"), Options{Shot: fixtureShot})
	if err != nil {
		t.Fatal(err)
	}
	verify := strings.Index(r.HTML, `id="verify"`)
	second := strings.Index(r.HTML, `id="start-the-ca-2"`)
	if verify < 0 || second < 0 || verify > second {
		t.Error("the partial was not rendered in place of its include")
	}

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "install", "a.md"), validFront+"<!-- include: missing.md -->\n")
	if _, err := Render(root, &Guide{Path: filepath.Join(root, "install", "a.md")}, Options{}); err == nil {
		t.Error("an include of a missing partial rendered")
	}

	writeFile(t, filepath.Join(root, PartialsDir, "p.md"), "<!-- include: q.md -->\n")
	writeFile(t, filepath.Join(root, PartialsDir, "q.md"), "<!-- include: p.md -->\n")
	writeFile(t, filepath.Join(root, "install", "b.md"), validFront+"<!-- include: p.md -->\n")
	_, err = Render(root, &Guide{Path: filepath.Join(root, "install", "b.md")}, Options{})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Errorf("include cycle: err = %v", err)
	}
}

// A message from a partial is attributed to the partial, at its line.
func TestMessages_PartialReferences(t *testing.T) {
	msgs, err := Messages(fixtureRoot, guide(t, "install", "containers").Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.ID == "Check it answers:" {
			if !strings.HasSuffix(m.File, "_partials/verify.md") || m.Line != 3 {
				t.Errorf("partial message located at %s:%d, want _partials/verify.md:3", m.File, m.Line)
			}
			return
		}
	}
	t.Error("the partial's paragraph was not among the messages")
}

func TestRender_Anchors(t *testing.T) {
	g := guide(t, "install", "containers")
	a, err := Render(fixtureRoot, g, Options{Shot: fixtureShot})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Render(fixtureRoot, g, Options{Shot: fixtureShot})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.Headings, b.Headings) {
		t.Error("anchors differ between two renders of the same guide")
	}
	var ids []string
	for _, h := range a.Headings {
		ids = append(ids, h.ID)
	}
	want := []string{"start-the-ca", "verify", "start-the-ca-2"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("heading ids = %v, want %v", ids, want)
	}
	for _, id := range want {
		if !a.Anchors[id] {
			t.Errorf("anchor %q missing from Anchors", id)
		}
	}
}

// The messages offered to translators are exactly the units the page
// marks - no more, no fewer - for every fixture guide.
func TestMessages_MatchRenderedUnits(t *testing.T) {
	secs, err := Load(fixtureRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secs {
		for _, g := range s.Guides {
			t.Run(g.Slug, func(t *testing.T) {
				msgs, err := Messages(fixtureRoot, g.Path)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, m := range msgs[2:] { // after title and summary
					got = append(got, m.ID)
				}
				if msgs[0].ID != g.Title || msgs[1].ID != g.Summary {
					t.Errorf("first messages = %q, %q; want the title and summary", msgs[0].ID, msgs[1].ID)
				}

				r, err := Render(fixtureRoot, g, Options{Shot: fixtureShot})
				if err != nil {
					t.Fatal(err)
				}
				var want []string
				eachElement(parse(t, r.HTML), func(n *html.Node) {
					if _, ok := getAttr(n, "data-i18n-html"); ok {
						want = append(want, Key(n))
					}
				})
				sort.Strings(got)
				sort.Strings(want)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("messages %q\n  do not match marked units %q", got, want)
				}
			})
		}
	}
}

// Apostrophes and quotes stay readable in keys; markup-significant
// characters stay escaped so a key is valid HTML.
func TestKey_Spelling(t *testing.T) {
	n := parse(t, `<p>It's "done" &amp; <code>&lt;certname&gt;</code> <a href="a&amp;b">x</a></p>`)
	var p *html.Node
	eachElement(n, func(e *html.Node) {
		if e.Data == "p" {
			p = e
		}
	})
	want := `It's "done" &amp; <code>&lt;certname&gt;</code> <a href="a&amp;b">x</a>`
	if got := Key(p); got != want {
		t.Errorf("Key = %s\n want %s", got, want)
	}
}

func parse(t *testing.T, markup string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func eachElement(n *html.Node, fn func(*html.Node)) {
	if n.Type == html.ElementNode {
		fn(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		eachElement(c, fn)
	}
}
