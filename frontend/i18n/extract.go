package main

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/voxpupuli/enterprise-console/marketing/guides"
)

// Extraction scans the frontend's source for translatable text and
// writes a PO template (.pot).
//
// Three marking conventions, all of which use the English source text as
// the msgid. That is the gettext way round, and it has a property worth
// having here: a string with no translation falls back to English on its
// own, with no lookup table of keys to maintain and no risk of a page
// rendering "nodes.title" because somebody mistyped a key.
//
//   JS:        t('Add node')
//   Go:        N_("Nodes and inventory")
//   Element:   <h1 data-i18n>Nodes</h1>
//   Attribute: <vox-input data-i18n-attr="label" label="Node name">
//   Prose:     <p data-i18n-html><strong>Assigned</strong> counts ...</p>
//
// A msgid appearing in several places gets one entry with several
// source references, so a translator sees everywhere it is used.

var (
	// t('...') and t("...") in JavaScript, one pattern per quote style
	// because RE2 has no backreference to say "the same quote again".
	//
	// Template literals are deliberately not matched: a string with an
	// interpolation in it cannot be a msgid, and silently extracting
	// one would produce a catalogue entry no translation could match.
	jsCalls = []*regexp.Regexp{
		regexp.MustCompile(`\bt\(\s*'((?:\\.|[^'\\])*)'\s*[,)]`),
		regexp.MustCompile(`\bt\(\s*"((?:\\.|[^"\\])*)"\s*[,)]`),
		// N_('...') marks a string for extraction without translating
		// it at that point - for tables evaluated before any catalogue
		// is loaded. Without matching it here those strings never reach
		// a translator and silently render in English.
		regexp.MustCompile(`\bN_\(\s*'((?:\\.|[^'\\])*)'\s*\)`),
		regexp.MustCompile(`\bN_\(\s*"((?:\\.|[^"\\])*)"\s*\)`),
	}

	// The opening tag of an element marked data-i18n. Go's regexp is
	// RE2, which has no backreferences, so the matching close tag
	// cannot be part of this pattern - it is found by scanning forward
	// from the match instead (see scanTemplate).
	elementOpen = regexp.MustCompile(`<([a-zA-Z][\w-]*)\b([^>]*\bdata-i18n\b(?:=["'][^"']*["'])?[^>]*)>`)

	// The opening tag of an element marked data-i18n-html, whose msgid
	// is its inner HTML rather than its text. Same forward-scan for the
	// close tag as elementOpen, for the same RE2 reason.
	elementOpenHTML = regexp.MustCompile(`<([a-zA-Z][\w-]*)\b[^>]*\bdata-i18n-html\b[^>]*>`)

	// tn('one', 'many', count) - a string with a plural form.
	tnCalls = []*regexp.Regexp{
		regexp.MustCompile(`\btn\(\s*'((?:\\.|[^'\\])*)'\s*,\s*'((?:\\.|[^'\\])*)'\s*,`),
		regexp.MustCompile(`\btn\(\s*"((?:\\.|[^"\\])*)"\s*,\s*"((?:\\.|[^"\\])*)"\s*,`),
	}

	// data-i18n-attr="label,placeholder" names which attributes of this
	// element carry translatable values.
	attrList = regexp.MustCompile(`data-i18n-attr=["']([^"']+)["']`)
)

// message is one translatable string and everywhere it was found.
type message struct {
	ID string
	// Plural is the English plural form, set only for strings extracted
	// from tn(). Its presence is what makes this a plural entry.
	Plural     string
	References []string
}

// extract scans every source file under the given roots.
func extract(roots []string) ([]message, error) {
	found := map[string]*message{}

	add := func(id, ref string) {
		addPlural(found, id, "", ref)
	}
	addN := func(id, plural, ref string) {
		addPlural(found, id, plural, ref)
	}

	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				// Test fixtures are not site content: the guide
				// renderer's live beside the real guides.
				if info.Name() == "testdata" && path != root {
					return filepath.SkipDir
				}
				return nil
			}
			switch filepath.Ext(path) {
			case ".js":
				return scanJS(path, add, addN)
			case ".go":
				// Go sources carry strings the templates cannot: the
				// marketing site's page titles and meta descriptions
				// are a Go table, and without this they would be the
				// only text on a translated page still in English.
				// The same N_("...") marker as JavaScript, matched by
				// the same double-quoted pattern.
				return scanJS(path, add, addN)
			case ".tmpl":
				return scanTemplate(path, add)
			case ".md":
				return scanGuide(path, found)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	_ = addN

	out := make([]message, 0, len(found))
	for _, m := range found {
		sort.Strings(m.References)
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func scanJS(path string, add func(id, ref string), addN func(id, plural, ref string)) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, re := range jsCalls {
		for _, m := range re.FindAllSubmatchIndex(body, -1) {
			id := unescapeJS(string(body[m[2]:m[3]]))
			add(id, fmt.Sprintf("%s:%d", path, lineOf(body, m[0])))
		}
	}
	for _, re := range tnCalls {
		for _, m := range re.FindAllSubmatchIndex(body, -1) {
			one := unescapeJS(string(body[m[2]:m[3]]))
			many := unescapeJS(string(body[m[4]:m[5]]))
			addN(one, many, fmt.Sprintf("%s:%d", path, lineOf(body, m[0])))
		}
	}
	return nil
}

// addPlural records a message, upgrading an existing entry to a plural
// one if this occurrence carries a plural form.
func addPlural(found map[string]*message, id, plural, ref string) {
	id = strings.TrimSpace(id)
	if id == "" || !worthTranslating(id) {
		return
	}
	m, ok := found[id]
	if !ok {
		m = &message{ID: id}
		found[id] = m
	}
	if plural != "" {
		m.Plural = plural
	}
	if !contains(m.References, ref) {
		m.References = append(m.References, ref)
	}
}

// scanGuide adds a site guide's messages (see marketing/guides).
//
// The guide renderer produces them - the same code that renders the
// page, so each message ID is exactly the key the site later looks up.
// That is also why worthTranslating is not applied: the renderer has
// already decided what is prose, and dropping one of its units here
// would leave it untranslatable on every page that shows it.
//
// Partials are skipped as files of their own: they are extracted
// through the guides that include them, with references pointing back
// at the partial's own lines, so a shared paragraph is one message.
func scanGuide(path string, found map[string]*message) error {
	sectionDir := filepath.Dir(path)
	if filepath.Base(sectionDir) == guides.PartialsDir {
		return nil
	}
	msgs, err := guides.Messages(filepath.Dir(sectionDir), path)
	if err != nil {
		return err
	}
	for _, m := range msgs {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		ref := fmt.Sprintf("%s:%d", m.File, m.Line)
		entry, ok := found[id]
		if !ok {
			entry = &message{ID: id}
			found[id] = entry
		}
		if !contains(entry.References, ref) {
			entry.References = append(entry.References, ref)
		}
	}
	return nil
}

func scanTemplate(path string, add func(id, ref string)) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(body)

	for _, m := range elementOpen.FindAllStringSubmatchIndex(text, -1) {
		// data-i18n-attr is a different marking; do not treat it as a
		// request to translate the element's text.
		if !hasBareDataI18n(text[m[4]:m[5]]) {
			continue
		}

		tagName := text[m[2]:m[3]]
		contentStart := m[1]
		closing := "</" + tagName + ">"
		rel := strings.Index(text[contentStart:], closing)
		if rel < 0 {
			continue
		}
		inner := text[contentStart : contentStart+rel]

		// Only plain text is a msgid. Nested markup would make the
		// translation carry HTML, which is how translated pages end up
		// with broken tags - and a Go template action would make the
		// msgid depend on code.
		if strings.ContainsAny(inner, "<>") || strings.Contains(inner, "{{") {
			continue
		}
		// Entities are decoded, because the runtime looks the string up
		// by what the parsed document holds - "-" rather than the
		// "&mdash;" the template was written with. Leaving the entity
		// in the msgid made every sentence containing one unmatchable:
		// the catalogue held one spelling and the page asked for the
		// other, so the string stayed English with nothing to show why.
		add(html.UnescapeString(collapse(inner)), fmt.Sprintf("%s:%d", path, lineOf(body, m[0])))
	}

	// Prose marked data-i18n-html, whose msgid is the inner HTML rather
	// than the text. The loop above skips anything containing markup;
	// these elements are exactly that case, marked deliberately so the
	// emphasis travels with the words instead of the sentence being
	// split into untranslatable fragments around each <strong>.
	for _, m := range elementOpenHTML.FindAllStringSubmatchIndex(text, -1) {
		tagName := text[m[2]:m[3]]
		contentStart := m[1]
		closing := "</" + tagName + ">"
		rel := strings.Index(text[contentStart:], closing)
		if rel < 0 {
			continue
		}
		inner := text[contentStart : contentStart+rel]

		// A Go template action would make the msgid depend on code, and
		// a nested block element is beyond what the runtime's
		// sanitiser keeps.
		if strings.Contains(inner, "{{") {
			continue
		}
		add(html.UnescapeString(collapse(inner)), fmt.Sprintf("%s:%d", path, lineOf(body, m[0])))
	}

	// Attribute values named by data-i18n-attr.
	for _, m := range attrList.FindAllStringSubmatchIndex(text, -1) {
		tagStart := strings.LastIndex(text[:m[0]], "<")
		tagEnd := strings.Index(text[m[0]:], ">")
		if tagStart < 0 || tagEnd < 0 {
			continue
		}
		tag := text[tagStart : m[0]+tagEnd]
		for _, name := range strings.Split(text[m[2]:m[3]], ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			// One pattern per quote style, not a single `["']([^"']*)["']`:
			// that stops the value at the first quote of either kind, so
			// note="...this group doesn't set one." was extracted as
			// "...this group doesn" and the rest of the sentence was
			// silently unreachable. RE2 has no backreference to say "the
			// same quote that opened this", hence two patterns.
			quoted := []*regexp.Regexp{
				regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `="([^"]*)"`),
				regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `='([^']*)'`),
			}
			for _, re := range quoted {
				if v := re.FindStringSubmatch(tag); len(v) == 2 {
					// Entities are decoded, because the runtime looks
					// the string up by what getAttribute() returns -
					// already-decoded text. Leaving &quot; in the msgid
					// made a note mentioning "name" and "parameters"
					// unmatchable: the catalogue held one spelling and
					// the page asked for the other.
					add(html.UnescapeString(v[1]), fmt.Sprintf("%s:%d", path, lineOf(body, tagStart)))
					break
				}
			}
		}
	}
	return nil
}

// worthTranslating filters out strings that are identifiers rather than
// language: CSS selectors, field names, single symbols. Extracting them
// wastes a translator's time and invites them to "translate" something
// the code compares against.
func worthTranslating(s string) bool {
	if len(s) < 2 {
		return false
	}
	if !strings.ContainsAny(s, " ") && strings.ContainsAny(s, "-_.#/") {
		return false
	}
	// Must contain at least one letter.
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

// collapse normalises the whitespace inside an element's text, so that
// wrapping markup does not change the msgid.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func lineOf(body []byte, offset int) int {
	return 1 + strings.Count(string(body[:offset]), "\n")
}

func unescapeJS(s string) string {
	r := strings.NewReplacer(`\'`, `'`, `\"`, `"`, `\\`, `\`, `\n`, "\n")
	return r.Replace(s)
}

// hasBareDataI18n reports whether the attribute text carries data-i18n
// itself, as opposed to only the data-i18n-attr variant.
func hasBareDataI18n(attrs string) bool {
	for _, f := range strings.Fields(attrs) {
		name, _, _ := strings.Cut(f, "=")
		if name == "data-i18n" {
			return true
		}
	}
	return false
}

func contains(h []string, n string) bool {
	return slices.Contains(h, n)
}
