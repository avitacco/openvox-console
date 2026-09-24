package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/voxpupuli/enterprise-console/marketing/guides"
)

// A catalogue is the compiled msgid -> translation map that
// `i18n compile` writes, the same file the console's browser fetches.
//
// Reading the compiled JSON rather than the .po keeps exactly one PO
// parser in the tree (frontend/i18n's). The marketing site needs only
// lookup, and encoding/json gives that for free.
type catalogue struct {
	// lang is the locale code, "" for English.
	lang string
	// dir is "rtl" or "ltr", for the html element.
	dir string
	// messages maps an English source string to its translation. A
	// plural entry compiles to a JSON array; the marketing copy has no
	// plurals, so those are ignored rather than half-supported.
	messages map[string]string
}

// englishCatalogue is the identity: every lookup returns its input, so
// the English build runs the same code path as every other locale
// instead of a special case that can rot.
func englishCatalogue() *catalogue {
	return &catalogue{lang: "en", dir: "ltr", messages: map[string]string{}}
}

// loadCatalogue reads one compiled locale.
func loadCatalogue(path, lang, dir string) (*catalogue, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading catalogue %s: %w", path, err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing catalogue %s: %w", path, err)
	}

	messages := make(map[string]string, len(raw))
	for id, value := range raw {
		// The empty msgid is gettext's header entry, and a JSON array is
		// a plural form. Neither is a string this site asks for.
		if id == "" {
			continue
		}
		var s string
		if err := json.Unmarshal(value, &s); err != nil {
			continue
		}
		messages[id] = s
	}

	return &catalogue{lang: lang, dir: dir, messages: messages}, nil
}

// get returns the translation for source, or source itself.
//
// Falling back to the English rather than erroring is deliberate and
// matches the console: a half-translated page is readable, and a missing
// string is visible as English rather than as a blank or a build
// failure that blocks every other locale.
func (c *catalogue) get(source string) string {
	t, _ := c.lookup(source)
	return t
}

// lookup is get, also reporting whether a translation was found.
func (c *catalogue) lookup(source string) (string, bool) {
	if t, ok := c.messages[source]; ok && t != "" {
		return t, true
	}
	return source, false
}

// translateHTML rewrites a rendered page in place.
//
// The markings are the console's, exactly: data-i18n on an element whose
// text is translatable, data-i18n-attr naming translatable attributes,
// and data-i18n-html for prose carrying inline markup. Using one
// convention across both means the same extractor finds both sites'
// strings and a contributor learns it once.
//
// This runs over parsed HTML rather than over the template source. A
// regex pass would be shorter but would be editing markup with a tool
// that cannot see markup, which is how "translated" pages end up with
// mismatched tags.
func translateHTML(page []byte, cat *catalogue) ([]byte, translateStats, error) {
	var stats translateStats
	doc, err := html.Parse(strings.NewReader(string(page)))
	if err != nil {
		return nil, stats, fmt.Errorf("parsing rendered page: %w", err)
	}

	var notice *html.Node
	var firstErr error
	var walk func(n *html.Node, inGuide bool)
	walk = func(n *html.Node, inGuide bool) {
		if n.Type == html.ElementNode {
			if n.DataAtom == atom.Html {
				setAttr(n, "lang", cat.lang)
				// dir is voxblocks' whole RTL contract - one attribute
				// on <html>, nothing per component.
				setAttr(n, "dir", cat.dir)
			}
			if _, ok := attr(n, guideBodyAttr); ok {
				inGuide = true
				removeAttr(n, guideBodyAttr)
			}
			if _, ok := attr(n, partialNoticeAttr); ok {
				notice = n
			}
			unit, translated, err := translateNode(n, cat)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if unit && inGuide {
				stats.units++
				if !translated {
					stats.missing++
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inGuide)
		}
	}
	walk(doc, false)
	if firstErr != nil {
		return nil, stats, firstErr
	}

	// The notice is in every guide page's markup, and kept only where it
	// is true: a translated page with at least one passage still in
	// English. English is the source, so it never shows there.
	if notice != nil {
		if cat.lang != "en" && stats.missing > 0 {
			removeAttr(notice, partialNoticeAttr)
		} else {
			notice.Parent.RemoveChild(notice)
		}
	}

	var out strings.Builder
	if err := html.Render(&out, doc); err != nil {
		return nil, stats, fmt.Errorf("rendering translated page: %w", err)
	}
	return []byte(out.String()), stats, nil
}

// Markers gen's guide template puts in a page for translateHTML.
const (
	// guideBodyAttr marks the element holding a guide's own text, whose
	// units are counted toward that guide's translation coverage.
	guideBodyAttr = "data-guide-body"
	// partialNoticeAttr marks the "not yet fully translated" callout.
	partialNoticeAttr = "data-partial-notice"
)

// translateStats counts a page's guide units and how many had no
// translation in the catalogue.
type translateStats struct {
	units, missing int
}

// translateNode applies whichever markings one element carries. It
// reports whether the element was a prose unit (data-i18n-html) and, if
// so, whether a translation was found for it.
func translateNode(n *html.Node, cat *catalogue) (unit, translated bool, err error) {
	if names, ok := attr(n, "data-i18n-attr"); ok {
		for _, name := range strings.Split(names, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if value, ok := attr(n, name); ok && value != "" {
				setAttr(n, name, cat.get(value))
			}
		}
		removeAttr(n, "data-i18n-attr")
	}

	if _, ok := attr(n, "data-i18n-html"); ok {
		removeAttr(n, "data-i18n-html")
		source := guides.Key(n)
		result, found := cat.lookup(source)
		if !found {
			return true, false, nil
		}
		if err := checkFrozen(n, result); err != nil {
			return true, true, fmt.Errorf("%s translation of %q: %w", cat.lang, source, err)
		}
		if err := setInnerHTML(n, result); err != nil {
			// Leave the English in place rather than emptying the
			// element: unreadable beats absent.
			return true, false, nil
		}
		return true, true, nil
	}

	if _, ok := attr(n, "data-i18n"); ok {
		setText(n, cat.get(collapseSpace(textOf(n))))
		removeAttr(n, "data-i18n")
	}
	return false, false, nil
}

// checkFrozen compares a translation against the source element it
// replaces and refuses one that changes what must not change: the
// contents of every <code> element, and every link target.
//
// Code is a command, a path or a setting name. A reader copies it; a
// translated one does not work, and a subtly different one works
// differently. Links are checked for the same reason - a translated
// anchor points nowhere - and because the site's link check runs on
// the English targets it can see, not on what a catalogue substitutes.
func checkFrozen(source *html.Node, translation string) error {
	parsed, err := html.ParseFragment(strings.NewReader(translation), source)
	if err != nil {
		return fmt.Errorf("does not parse as HTML: %w", err)
	}
	holder := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	for _, n := range parsed {
		holder.AppendChild(n)
	}
	want, got := frozen(source), frozen(holder)
	if strings.Join(want, "\x00") != strings.Join(got, "\x00") {
		return fmt.Errorf("changes code or links: source has %q, translation has %q", want, got)
	}
	return nil
}

// frozen lists an element's code contents and link targets, sorted, so
// a translation may reorder them but not change, add or drop any.
func frozen(n *html.Node) []string {
	var out []string
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.ElementNode {
			switch c.DataAtom {
			case atom.Code:
				out = append(out, "code:"+textOf(c))
				return
			case atom.A:
				href, _ := attr(c, "href")
				out = append(out, "href:"+href)
			}
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	sort.Strings(out)
	return out
}

// collapseSpace normalises whitespace so a msgid does not depend on how
// the template happened to be indented. Matches the extractor's own
// collapse().
func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func attr(n *html.Node, name string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val, true
		}
	}
	return "", false
}

func setAttr(n *html.Node, name, value string) {
	for i, a := range n.Attr {
		if a.Key == name {
			n.Attr[i].Val = value
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: name, Val: value})
}

func removeAttr(n *html.Node, name string) {
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		if a.Key != name {
			kept = append(kept, a)
		}
	}
	n.Attr = kept
}

func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// setText replaces an element's children with a single text node.
//
// Children that are elements are kept only if there were any to begin
// with - data-i18n is for text-only elements, and the extractor refuses
// to extract anything containing markup, so reaching here with element
// children means the marking is on the wrong element.
func setText(n *html.Node, text string) {
	for n.FirstChild != nil {
		n.RemoveChild(n.FirstChild)
	}
	n.AppendChild(&html.Node{Type: html.TextNode, Data: text})
}

func innerHTML(n *html.Node) (string, error) {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return "", err
		}
	}
	return b.String(), nil
}

func setInnerHTML(n *html.Node, markup string) error {
	parsed, err := html.ParseFragment(strings.NewReader(markup), n)
	if err != nil {
		return err
	}
	for n.FirstChild != nil {
		n.RemoveChild(n.FirstChild)
	}
	for _, child := range parsed {
		n.AppendChild(child)
	}
	return nil
}
