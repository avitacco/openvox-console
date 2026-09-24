package main

import (
	"html/template"
	"strings"
	"testing"

	"github.com/voxpupuli/enterprise-console/marketing/shots"
)

// The marketing site is translated at build time, so a mistake here does
// not surface as a broken page in a browser - it surfaces as a published
// page that is silently still in English, or one whose markup has been
// mangled by a substitution. Both are things a reader notices before we
// do, which is why this is tested rather than eyeballed.

func testCatalogue(messages map[string]string) *catalogue {
	return &catalogue{lang: "de", dir: "ltr", messages: messages}
}

func TestTranslateElementText(t *testing.T) {
	in := []byte(`<html><body><h2 data-i18n>What it does</h2></body></html>`)
	out, _, err := translateHTML(in, testCatalogue(map[string]string{
		"What it does": "Was es kann",
	}))
	if err != nil {
		t.Fatalf("translateHTML: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "<h2>Was es kann</h2>") {
		t.Errorf("text not translated, or the marker survived:\n%s", got)
	}
	if strings.Contains(got, "data-i18n") {
		t.Errorf("the data-i18n marker was published to the page:\n%s", got)
	}
}

func TestTranslateAttributes(t *testing.T) {
	in := []byte(`<html><body><vox-cta-band heading="Run it yourself" data-i18n-attr="heading"></vox-cta-band></body></html>`)
	out, _, err := translateHTML(in, testCatalogue(map[string]string{
		"Run it yourself": "Selbst ausführen",
	}))
	if err != nil {
		t.Fatalf("translateHTML: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `heading="Selbst ausführen"`) {
		t.Errorf("attribute not translated:\n%s", got)
	}
	if strings.Contains(got, "data-i18n-attr") {
		t.Errorf("the marker was published to the page:\n%s", got)
	}
}

func TestTranslateProseKeepsInlineMarkup(t *testing.T) {
	// The msgid is the inner HTML, so the translator moves the <code>
	// with the words it belongs to rather than being handed fragments.
	in := []byte(`<html><body><p data-i18n-html>Run <code>docker compose up</code> first.</p></body></html>`)
	out, _, err := translateHTML(in, testCatalogue(map[string]string{
		"Run <code>docker compose up</code> first.": "Zuerst <code>docker compose up</code> ausführen.",
	}))
	if err != nil {
		t.Fatalf("translateHTML: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "<code>docker compose up</code>") {
		t.Errorf("inline markup lost in translation:\n%s", got)
	}
	if !strings.Contains(got, "Zuerst") {
		t.Errorf("prose not translated:\n%s", got)
	}
}

func TestUntranslatedFallsBackToEnglish(t *testing.T) {
	// A missing string must render as English, not as a blank element
	// and not as a build failure that blocks the other locales.
	in := []byte(`<html><body><h2 data-i18n>Only in English</h2></body></html>`)
	out, _, err := translateHTML(in, testCatalogue(map[string]string{}))
	if err != nil {
		t.Fatalf("translateHTML: %v", err)
	}
	if !strings.Contains(string(out), "Only in English") {
		t.Errorf("untranslated string was lost rather than left in English:\n%s", out)
	}
}

func TestIndentedProseMatchesItsMsgid(t *testing.T) {
	// Templates wrap and indent their prose; the extractor collapses
	// whitespace when building the msgid, so the lookup has to collapse
	// it the same way or nothing long ever matches.
	in := []byte("<html><body><p data-i18n>\n      See every node,\n      then act.\n    </p></body></html>")
	out, _, err := translateHTML(in, testCatalogue(map[string]string{
		"See every node, then act.": "Jeden Node sehen, dann handeln.",
	}))
	if err != nil {
		t.Fatalf("translateHTML: %v", err)
	}
	if !strings.Contains(string(out), "Jeden Node sehen") {
		t.Errorf("indented prose did not match its collapsed msgid:\n%s", out)
	}
}

func TestDirectionAndLangAreStamped(t *testing.T) {
	// One dir on <html> is the whole of voxblocks' RTL contract.
	in := []byte(`<html lang="en" dir="ltr"><body></body></html>`)
	out, _, err := translateHTML(in, &catalogue{lang: "ar", dir: "rtl", messages: map[string]string{}})
	if err != nil {
		t.Fatalf("translateHTML: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `lang="ar"`) || !strings.Contains(got, `dir="rtl"`) {
		t.Errorf("lang/dir not stamped for an RTL locale:\n%s", got)
	}
}

func TestEnglishCatalogueIsIdentity(t *testing.T) {
	cat := englishCatalogue()
	if got := cat.get("Anything at all"); got != "Anything at all" {
		t.Errorf("English lookup changed the string: %q", got)
	}
	if cat.dir != "ltr" || cat.lang != "en" {
		t.Errorf("unexpected English catalogue: %+v", cat)
	}
}

// A screenshot's alt text is prose like any other, and a screen reader
// user on a translated page should hear it in their language.
func TestScreenshotAltIsTranslated(t *testing.T) {
	shot := shots.All[0]
	render := funcsFor(pages[0], "de")["screenshot"].(func(string) (template.HTML, error))
	figure, err := render(shot.Name)
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	in := []byte("<html><body>" + string(figure) + "</body></html>")
	out, _, err := translateHTML(in, testCatalogue(map[string]string{
		shot.Caption: "Übersetzte Beschreibung",
	}))
	if err != nil {
		t.Fatalf("translateHTML: %v", err)
	}
	if !strings.Contains(string(out), `alt="Übersetzte Beschreibung"`) {
		t.Errorf("screenshot alt text was not translated:\n%s", out)
	}
}
