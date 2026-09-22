package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
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
	if t, ok := c.messages[source]; ok && t != "" {
		return t
	}
	return source
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
func translateHTML(page []byte, cat *catalogue) ([]byte, error) {
	doc, err := html.Parse(strings.NewReader(string(page)))
	if err != nil {
		return nil, fmt.Errorf("parsing rendered page: %w", err)
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.DataAtom == atom.Html {
				setAttr(n, "lang", cat.lang)
				// dir is voxblocks' whole RTL contract - one attribute
				// on <html>, nothing per component.
				setAttr(n, "dir", cat.dir)
			}
			translateNode(n, cat)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	var out strings.Builder
	if err := html.Render(&out, doc); err != nil {
		return nil, fmt.Errorf("rendering translated page: %w", err)
	}
	return []byte(out.String()), nil
}

// translateNode applies whichever markings one element carries.
func translateNode(n *html.Node, cat *catalogue) {
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
		inner, err := innerHTML(n)
		if err == nil {
			translated := cat.get(collapseSpace(inner))
			if err := setInnerHTML(n, translated); err != nil {
				// Leave the English in place rather than emptying the
				// element: unreadable beats absent.
				_ = err
			}
		}
		removeAttr(n, "data-i18n-html")
		return
	}

	if _, ok := attr(n, "data-i18n"); ok {
		setText(n, cat.get(collapseSpace(textOf(n))))
		removeAttr(n, "data-i18n")
	}
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
