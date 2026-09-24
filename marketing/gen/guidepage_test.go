package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// guidePage is a minimal translated-page shape: a partial-translation
// notice and a guide body with two prose units.
const guidePage = `<html><body>
<vox-callout data-partial-notice heading="Not fully translated yet"><p>notice</p></vox-callout>
<div data-guide-body>
<p data-i18n-html>Run <code>make up</code> first.</p>
<p data-i18n-html>Then check it.</p>
</div></body></html>`

func TestPartialNotice_ShownOnlyWhenPassagesAreMissing(t *testing.T) {
	cases := []struct {
		name       string
		cat        *catalogue
		wantNotice bool
		wantMiss   int
	}{
		{"english never shows it", englishCatalogue(), false, 2},
		{"fully translated", testCatalogue(map[string]string{
			"Run <code>make up</code> first.": "Zuerst <code>make up</code> ausführen.",
			"Then check it.":                  "Dann prüfen.",
		}), false, 0},
		{"one passage missing", testCatalogue(map[string]string{
			"Then check it.": "Dann prüfen.",
		}), true, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, stats, err := translateHTML([]byte(guidePage), c.cat)
			if err != nil {
				t.Fatalf("translateHTML: %v", err)
			}
			if got := strings.Contains(string(out), "Not fully translated yet"); got != c.wantNotice {
				t.Errorf("notice shown = %v, want %v", got, c.wantNotice)
			}
			if strings.Contains(string(out), partialNoticeAttr) || strings.Contains(string(out), guideBodyAttr) {
				t.Error("a build-time marker was published to the page")
			}
			if stats.units != 2 || stats.missing != c.wantMiss {
				t.Errorf("stats = %+v, want 2 units, %d missing", stats, c.wantMiss)
			}
		})
	}
}

// Code and link targets must come back from a translation unchanged -
// reordered is fine, altered, dropped or added is a build failure.
func TestFrozenCode(t *testing.T) {
	page := `<html><body><p data-i18n-html>Run <code>make up</code>, then read <a href="x.html#a">this</a>.</p></body></html>`
	source := "Run <code>make up</code>, then read <a href=\"x.html#a\">this</a>."
	cases := map[string]struct {
		translation string
		ok          bool
	}{
		"reordered":    {`Lies <a href="x.html#a">dies</a>, dann <code>make up</code>.`, true},
		"altered code": {`Führe <code>make hoch</code> aus, lies <a href="x.html#a">dies</a>.`, false},
		"dropped code": {`Ausführen, dann <a href="x.html#a">dies</a> lesen.`, false},
		"added code":   {`<code>make up</code> <code>sudo</code>, <a href="x.html#a">dies</a>.`, false},
		"changed link": {`<code>make up</code>, <a href="x.html#b">dies</a>.`, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := translateHTML([]byte(page), testCatalogue(map[string]string{source: c.translation}))
			if c.ok && err != nil {
				t.Fatalf("a translation keeping code and links intact was refused: %v", err)
			}
			if !c.ok {
				if err == nil {
					t.Fatal("a translation that changes code or links was accepted")
				}
				if !strings.Contains(err.Error(), "de translation of") || !strings.Contains(err.Error(), "make up") {
					t.Errorf("error %q does not name the locale and the source string", err)
				}
			}
		})
	}
}

func TestUnknownSettings(t *testing.T) {
	known := map[string]bool{"CONSOLE_HTTP_ADDR": true, "CONSOLE_CLUSTER_SECRET": true}
	text := `Set CONSOLE_HTTP_ADDR, CONSOLE_CLUSTER_SECRET_FILE and the CONSOLE_CLUSTER_* settings,
not CONSOLE_HTTP_PORT or CONSOLE_NOPE_*.`
	got := unknownSettings(text, known)
	want := []string{"CONSOLE_HTTP_PORT", "CONSOLE_NOPE_"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unknownSettings = %v, want %v", got, want)
	}
}

// The real configuration file is where the settings list comes from; if
// it moves or stops matching, the check must fail loudly rather than
// find nothing and accept everything.
func TestLoadSettings_ReadsTheConsolesConfiguration(t *testing.T) {
	names, err := loadSettings(filepath.Join("..", settingsSource), filepath.Join("..", composeSource))
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	// Two console settings, and two compose-file variables.
	for _, want := range []string{"CONSOLE_HTTP_ADDR", "CONSOLE_CLUSTER_LEAF_SECRET", "CONSOLE_IMAGE_TAG", "CONSOLE_CERTS_DIR"} {
		if !names[want] {
			t.Errorf("%s not found among the console's settings", want)
		}
	}

	empty := filepath.Join(t.TempDir(), "config.go")
	if err := os.WriteFile(empty, []byte("package runtime\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSettings(empty, filepath.Join("..", composeSource)); err == nil {
		t.Error("a file with no settings was accepted")
	}
}

func TestCheckLinks(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "site.css"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	pageWith := func(name, body string) renderedPage {
		return renderedPage{page: page{Name: name}, html: []byte("<html><body>" + body + "</body></html>")}
	}
	base := []renderedPage{
		pageWith("index.html", `<a href="guides/a.html#setup">x</a><link href="site.css">`),
		pageWith("guides/a.html", `<h2 id="setup">Setup</h2><a href="#setup">top</a><a href="../index.html">home</a>`),
	}
	if err := checkLinks(base, root, "en"); err != nil {
		t.Fatalf("valid links refused: %v", err)
	}

	cases := map[string]struct{ body, want string }{
		"missing page":         {`<a href="guides/b.html">x</a>`, "no such page"},
		"missing anchor":       {`<a href="guides/a.html#nope">x</a>`, "no anchor #nope"},
		"missing same-page id": {`<a href="#nope">x</a>`, "no element on this page"},
		"missing asset":        {`<img src="gone.png">`, "no such file"},
		"root-relative":        {`<a href="/guides/a.html">x</a>`, "path prefix"},
		"series nav":           {`<vox-series-nav next-href="guides/c.html"></vox-series-nav>`, "no such page"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			pages := append([]renderedPage{pageWith("index.html", c.body)}, base[1:]...)
			err := checkLinks(pages, root, "en")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want one containing %q", err, c.want)
			}
			if !strings.Contains(err.Error(), "index.html") {
				t.Errorf("error %q does not name the page", err)
			}
		})
	}

	// A link to another locale's copy of a page resolves against this
	// locale's page list: every locale publishes the same pages.
	de := []renderedPage{
		pageWith("index.html", `<a href="../index.html">en</a><a href="../ar/guides/a.html#setup">ar</a>`),
		base[1],
	}
	if err := checkLinks(de, root, "de"); err != nil {
		t.Errorf("cross-locale links refused: %v", err)
	}
}
