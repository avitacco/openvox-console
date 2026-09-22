package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// This file reads and writes gettext .po catalogues.
//
// A deliberately small subset: msgid, msgid_plural, msgstr, msgstr[n],
// source references, the fuzzy flag and obsolete entries. Not message
// contexts (msgctxt) - nothing here needs to distinguish two senses of
// the same English word yet.
//
// How many plural forms a catalogue carries comes from its own
// Plural-Forms header, so Russian's three and Chinese's one are both
// handled. The header travels into the compiled JSON, where the browser
// evaluates the rule itself (see frontend/src/plural.js).

// defaultPluralForms is assumed when a catalogue declares no rule -
// English's two.
const defaultPluralForms = 2

// npluralsPattern reads the form count out of a Plural-Forms header.
var npluralsPattern = regexp.MustCompile(`nplurals\s*=\s*(\d+)`)

// nplurals is how many plural forms this catalogue's language uses.
func (c *catalog) nplurals() int {
	n := npluralsPattern.FindStringSubmatch(c.Header["Plural-Forms"])
	if n == nil {
		return defaultPluralForms
	}
	count, err := strconv.Atoi(n[1])
	if err != nil || count < 1 {
		return defaultPluralForms
	}
	return count
}

// entry is one catalogue record.
type entry struct {
	ID string
	// Plural is the English plural form; non-empty makes this a plural
	// entry, whose translations live in Plurals rather than Str.
	Plural string
	Str    string
	// Plurals holds one translation per plural form, in the order the
	// locale's Plural-Forms rule indexes them.
	Plurals    []string
	References []string
	// Fuzzy marks a translation carried over from a similar string, or
	// one a tool guessed. gettext treats fuzzy entries as untranslated,
	// and so does the compiler below - shipping a guess is how a UI ends
	// up confidently saying the wrong thing.
	Fuzzy bool
	// Obsolete entries are kept, commented out, so a string that comes
	// back does not lose its translation.
	Obsolete bool
}

// catalog is one locale's messages, keyed by msgid.
type catalog struct {
	Language string
	Header   map[string]string
	Entries  map[string]*entry
	// order preserves the order entries were read or added, so writing
	// a catalogue back produces a small diff rather than reshuffling it.
	order []string
}

func newCatalog(lang string) *catalog {
	return &catalog{
		Language: lang,
		Header:   map[string]string{},
		Entries:  map[string]*entry{},
	}
}

// readPO parses a .po file.
func readPO(path string) (*catalog, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cat := newCatalog(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))

	var (
		cur      *entry
		field    *string
		refs     []string
		fuzzy    bool
		obsolete bool
	)

	flush := func() {
		if cur == nil {
			return
		}
		cur.References = refs
		cur.Fuzzy = fuzzy
		cur.Obsolete = obsolete
		if cur.ID != "" {
			cat.add(cur)
		} else {
			// The empty msgid carries the catalogue header.
			for _, line := range strings.Split(cur.Str, "\n") {
				if k, v, ok := strings.Cut(line, ":"); ok {
					cat.Header[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}
		}
		cur, field, refs, fuzzy, obsolete = nil, nil, nil, false, false
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())

		switch {
		case line == "":
			flush()

		case strings.HasPrefix(line, "#:"):
			refs = append(refs, strings.Fields(strings.TrimPrefix(line, "#:"))...)

		case strings.HasPrefix(line, "#,"):
			if strings.Contains(line, "fuzzy") {
				fuzzy = true
			}

		case strings.HasPrefix(line, "#~"):
			// Obsolete entry: strip the marker and fall through by
			// re-processing the remainder as a normal line.
			obsolete = true
			rest := strings.TrimSpace(strings.TrimPrefix(line, "#~"))
			if cur == nil {
				cur = &entry{}
			}
			if s, ok := parseField(rest, "msgid"); ok {
				cur.ID, field = s, &cur.ID
			} else if s, ok := parseField(rest, "msgstr"); ok {
				cur.Str, field = s, &cur.Str
			}

		case strings.HasPrefix(line, "#"):
			// Translator or extracted comment; not round-tripped.

		case strings.HasPrefix(line, "msgid_plural "):
			if cur == nil {
				cur = &entry{}
			}
			s, _ := parseField(line, "msgid_plural")
			cur.Plural, field = s, &cur.Plural

		case strings.HasPrefix(line, "msgstr["):
			if cur == nil {
				cur = &entry{}
			}
			idx, value, ok := parseIndexedField(line)
			if !ok {
				return nil, fmt.Errorf("%s:%d: cannot parse %q", path, lineNo, line)
			}
			for len(cur.Plurals) <= idx {
				cur.Plurals = append(cur.Plurals, "")
			}
			cur.Plurals[idx] = value
			field = &cur.Plurals[idx]

		case strings.HasPrefix(line, "msgid "):
			if cur != nil && cur.ID != "" {
				flush()
			}
			if cur == nil {
				cur = &entry{}
			}
			s, _ := parseField(line, "msgid")
			cur.ID, field = s, &cur.ID

		case strings.HasPrefix(line, "msgstr "):
			if cur == nil {
				cur = &entry{}
			}
			s, _ := parseField(line, "msgstr")
			cur.Str, field = s, &cur.Str

		case strings.HasPrefix(line, `"`):
			// Continuation of the previous field.
			if field == nil {
				return nil, fmt.Errorf("%s:%d: string continuation with no field to continue", path, lineNo)
			}
			s, err := strconv.Unquote(line)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
			}
			*field += s

		default:
			return nil, fmt.Errorf("%s:%d: cannot parse %q", path, lineNo, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()

	return cat, nil
}

// parseIndexedField reads `msgstr[N] "value"`.
func parseIndexedField(line string) (int, string, bool) {
	rest, ok := strings.CutPrefix(line, "msgstr[")
	if !ok {
		return 0, "", false
	}
	numStr, rest, ok := strings.Cut(rest, "]")
	if !ok {
		return 0, "", false
	}
	idx, err := strconv.Atoi(strings.TrimSpace(numStr))
	if err != nil {
		return 0, "", false
	}
	value, err := strconv.Unquote(strings.TrimSpace(rest))
	if err != nil {
		return 0, "", false
	}
	return idx, value, true
}

// parseField reads `name "value"` and unquotes the value.
func parseField(line, name string) (string, bool) {
	rest, ok := strings.CutPrefix(line, name+" ")
	if !ok {
		return "", false
	}
	s, err := strconv.Unquote(strings.TrimSpace(rest))
	if err != nil {
		return "", false
	}
	return s, true
}

func (c *catalog) add(e *entry) {
	if _, seen := c.Entries[e.ID]; !seen {
		c.order = append(c.order, e.ID)
	}
	c.Entries[e.ID] = e
}

// merge brings the catalogue up to date with the extracted messages,
// returning how many were added and how many became obsolete.
func (c *catalog) merge(messages []message) (added, obsolete int) {
	live := make(map[string]bool, len(messages))

	for _, m := range messages {
		live[m.ID] = true
		if e, ok := c.Entries[m.ID]; ok {
			e.References = m.References
			e.Plural = m.Plural
			// A string coming back from obsolete keeps its translation.
			if e.Obsolete {
				e.Obsolete = false
			}
			continue
		}
		c.add(&entry{ID: m.ID, Plural: m.Plural, References: m.References})
		added++
	}

	for _, id := range c.order {
		e := c.Entries[id]
		if !live[id] && !e.Obsolete {
			e.Obsolete = true
			obsolete++
		}
	}
	return added, obsolete
}

// active counts entries still present in the source.
func (c *catalog) active() int {
	var n int
	for _, e := range c.Entries {
		if !e.Obsolete {
			n++
		}
	}
	return n
}

// translated counts active entries with a usable translation. A fuzzy
// entry does not count: gettext treats it as unverified, and so do we.
func (c *catalog) translated() int {
	var n int
	for _, e := range c.Entries {
		if e.Obsolete || e.Fuzzy {
			continue
		}
		if e.Plural != "" {
			// A plural entry is only done when every form is filled;
			// a half-translated one renders an empty string for the
			// count that is missing.
			forms := c.nplurals()
			if len(e.Plurals) < forms || slices.Contains(e.Plurals[:forms], "") {
				continue
			}
			n++
			continue
		}
		if e.Str != "" {
			n++
		}
	}
	return n
}

// writePOT writes the extraction template: every string, no translations.
func writePOT(path string, messages []message) error {
	cat := newCatalog("")
	for _, m := range messages {
		cat.add(&entry{ID: m.ID, Plural: m.Plural, References: m.References})
	}
	cat.Header = potHeader("")
	return writePO(path, cat)
}

func potHeader(lang string) map[string]string {
	h := map[string]string{
		"Project-Id-Version":        "openvox-console",
		"MIME-Version":              "1.0",
		"Content-Type":              "text/plain; charset=UTF-8",
		"Content-Transfer-Encoding": "8bit",
		"POT-Creation-Date":         time.Now().UTC().Format("2006-01-02 15:04-0700"),
	}
	if lang != "" {
		h["Language"] = lang
	}
	return h
}

// writePO writes a catalogue back out.
func writePO(path string, cat *catalog) error {
	var b strings.Builder

	if len(cat.Header) == 0 {
		cat.Header = potHeader(cat.Language)
	}
	if cat.Language != "" {
		cat.Header["Language"] = cat.Language
	}

	b.WriteString("msgid \"\"\nmsgstr \"\"\n")
	var keys []string
	for k := range cat.Header {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%q\n", k+": "+cat.Header[k]+"\n")
	}

	forms := cat.nplurals()

	for _, id := range cat.order {
		e := cat.Entries[id]
		b.WriteString("\n")

		prefix := ""
		if e.Obsolete {
			prefix = "#~ "
		}

		for _, ref := range e.References {
			fmt.Fprintf(&b, "#: %s\n", ref)
		}
		if e.Fuzzy {
			b.WriteString("#, fuzzy\n")
		}
		fmt.Fprintf(&b, "%smsgid %q\n", prefix, e.ID)
		if e.Plural != "" {
			fmt.Fprintf(&b, "%smsgid_plural %q\n", prefix, e.Plural)
			for i := 0; i < forms; i++ {
				var v string
				if i < len(e.Plurals) {
					v = e.Plurals[i]
				}
				fmt.Fprintf(&b, "%smsgstr[%d] %q\n", prefix, i, v)
			}
			continue
		}
		fmt.Fprintf(&b, "%smsgstr %q\n", prefix, e.Str)
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// pluralFormsHeader is the catalogue's rule, or English's if it
// declares none.
func (c *catalog) pluralFormsHeader() string {
	if h := c.Header["Plural-Forms"]; h != "" {
		return h
	}
	return "nplurals=2; plural=(n != 1);"
}

// writeJSON writes the compact runtime catalogue.
//
// Only entries with a real translation are included. An untranslated or
// fuzzy entry is left out entirely so the runtime falls back to the
// msgid, which is the English source - better than shipping an empty
// string that would render as nothing.
func writeJSON(path string, cat *catalog) error {
	forms := cat.nplurals()

	// The empty key carries the catalogue's own metadata - gettext's
	// convention for the header entry. The runtime needs the plural rule
	// to choose a form; without it, a three-form language would be read
	// with English's two-form assumption.
	out := map[string]any{
		"": map[string]string{
			"language":     cat.Language,
			"plural-forms": cat.pluralFormsHeader(),
		},
	}

	for _, e := range cat.Entries {
		if e.Obsolete || e.Fuzzy {
			continue
		}
		if e.Plural != "" {
			if len(e.Plurals) < forms || slices.Contains(e.Plurals[:forms], "") {
				continue
			}
			// An array marks a plural entry; the runtime picks a form
			// by index.
			out[e.ID] = e.Plurals[:forms]
			continue
		}
		if e.Str == "" || e.Str == e.ID {
			continue
		}
		out[e.ID] = e.Str
	}

	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o644)
}
