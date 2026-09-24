package guides

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Labels the renderer writes into pages. Each is marked for extraction
// here, and emitted with data-i18n-attr so the site's translator swaps
// it per locale.
var (
	labelCopy         = N_("Copy code")
	labelCopied       = N_("Copied")
	labelCopiedNotice = N_("Copied to clipboard")
)

// callouts maps GitHub's alert syntax onto vox-callout's variants, with
// the heading each one shows. GitHub's syntax is used because it is what
// the same file shows on GitHub: a guide stays readable in the
// repository even though the site is canonical.
var callouts = map[string]struct{ variant, heading string }{
	"NOTE":    {"info", N_("Note")},
	"TIP":     {"tip", N_("Tip")},
	"WARNING": {"warning", N_("Warning")},
	"CAUTION": {"danger", N_("Caution")},
}

// codeLanguages are the vox-code-block language values the component
// accepts, and aliases the renderer maps onto one of them. Anything else
// is an error: the component ignores a value it does not know, so a
// wrong one would degrade silently.
var codeLanguages = map[string]string{
	"": "plaintext", "plaintext": "plaintext", "text": "plaintext",
	"bash": "bash", "sh": "sh", "shell": "shell", "zsh": "zsh",
	"yaml": "yaml", "yml": "yml", "json": "json",
	"javascript": "javascript", "js": "js", "typescript": "typescript", "ts": "ts",
	"css": "css", "html": "html", "xml": "xml",
	"ruby": "ruby", "rb": "rb", "puppet": "puppet", "pp": "pp",
	// No grammar in vox-code-block; shown as plain text rather than
	// refused, because guides genuinely need them.
	"powershell": "plaintext", "ini": "plaintext", "conf": "plaintext",
	"sql": "plaintext", "dockerfile": "plaintext", "toml": "plaintext",
}

// Options are the caller's hooks into rendering.
type Options struct {
	// Shot returns the markup for the declared screenshot name, or an
	// error when it is not declared. Required to render a guide that
	// uses one.
	Shot func(name string) (string, error)
}

// Heading is one h2/h3 in a rendered guide, for its table of contents.
type Heading struct {
	Level int
	// ID is the heading's anchor, derived from the English text so a
	// link works in every locale.
	ID string
	// HTML is the heading's inner HTML - also its message key, so a
	// table of contents entry translates exactly as the heading does.
	HTML string
}

// Rendered is a guide ready to place in a page.
type Rendered struct {
	HTML     string
	Headings []Heading
	// Anchors is every id the guide defines.
	Anchors map[string]bool
}

// Message is one translatable string found in a guide.
type Message struct {
	// ID is the message key, exactly as the site looks it up.
	ID string
	// File and Line locate it in the source, partials included.
	File string
	Line int
}

// Render renders the guide at g.Path. root is the guides root, where
// partials are found.
func Render(root string, g *Guide, opts Options) (*Rendered, error) {
	r, _, err := render(root, g.Path, opts)
	return r, err
}

// Messages returns every translatable string in the guide at path, in
// document order: its title and summary, then each block the rendered
// page marks data-i18n-html. It is what the extractor offers
// translators, produced by the same code as the page itself.
func Messages(root, path string) ([]Message, error) {
	meta, _, err := readFile(path)
	if err != nil {
		return nil, err
	}
	// Screenshots are not text; any declared-or-not question belongs to
	// the site build, not to extraction.
	stub := Options{Shot: func(string) (string, error) { return "<figure></figure>", nil }}
	_, msgs, err := render(root, path, stub)
	if err != nil {
		return nil, err
	}
	head := []Message{
		{ID: meta.Title, File: path, Line: 1},
		{ID: meta.Summary, File: path, Line: 1},
	}
	return append(head, msgs...), nil
}

// lineAttr carries a block's source line from goldmark's output into the
// post-processing pass, which strips it again.
const lineAttr = "data-src-line"

func render(root, path string, opts Options) (*Rendered, []Message, error) {
	_, body, err := readFile(path)
	if err != nil {
		return nil, nil, err
	}
	expanded, origins, err := expand(root, path, body, nil)
	if err != nil {
		return nil, nil, err
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(&lineMarker{}, 100))),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(util.Prioritized(&codeRenderer{}, 100)),
		),
	)
	src := expanded
	doc := md.Parser().Parse(text.NewReader(src))

	locate := func(line int) (string, int) {
		if line >= 1 && line <= len(origins) {
			o := origins[line-1]
			return o.file, o.line
		}
		return path, 0
	}

	// Raw HTML would bypass every check the renderer makes, and
	// goldmark would drop it anyway. Refused outright, with the place.
	var rawErr error
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || rawErr != nil {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindHTMLBlock, ast.KindRawHTML:
			f, l := locate(nodeLine(n, src))
			rawErr = fmt.Errorf("%s:%d: raw HTML is not allowed in a guide", f, l)
		case ast.KindHeading:
			if n.(*ast.Heading).Level == 1 {
				f, l := locate(nodeLine(n, src))
				rawErr = fmt.Errorf("%s:%d: a guide's title comes from its front matter; start sections at ##", f, l)
			}
		case ast.KindFencedCodeBlock:
			fc := n.(*ast.FencedCodeBlock)
			if _, _, err := parseInfo(fc, src); err != nil {
				f, l := locate(nodeLine(n, src))
				rawErr = fmt.Errorf("%s:%d: %w", f, l, err)
			}
		}
		return ast.WalkContinue, nil
	})
	if rawErr != nil {
		return nil, nil, rawErr
	}

	var out bytes.Buffer
	if err := md.Renderer().Render(&out, src, doc); err != nil {
		return nil, nil, fmt.Errorf("%s: render: %w", path, err)
	}

	p := &post{locate: locate, opts: opts, anchors: map[string]bool{}}
	rendered, err := p.run(out.String())
	if err != nil {
		return nil, nil, err
	}
	return rendered, p.messages, nil
}

// nodeLine is the 1-based line a block node starts on.
func nodeLine(n ast.Node, src []byte) int {
	for c := n; c != nil; c = c.FirstChild() {
		if c.Type() == ast.TypeBlock && c.Lines().Len() > 0 {
			return 1 + bytes.Count(src[:c.Lines().At(0).Start], []byte("\n"))
		}
		if c.Type() == ast.TypeInline {
			if t, ok := c.(*ast.Text); ok {
				return 1 + bytes.Count(src[:t.Segment.Start], []byte("\n"))
			}
		}
	}
	return 0
}

// lineMarker records each block's source line as an attribute, which
// goldmark's HTML renderer emits for data- attributes.
type lineMarker struct{}

func (*lineMarker) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Type() != ast.TypeBlock || n.Kind() == ast.KindDocument {
			return ast.WalkContinue, nil
		}
		if line := nodeLine(n, src); line > 0 {
			n.SetAttributeString(lineAttr, []byte(strconv.Itoa(line)))
		}
		return ast.WalkContinue, nil
	})
}

// infoTab matches the tab="Label" part of a fenced block's info string.
var infoTab = regexp.MustCompile(`\btab="([^"]+)"`)

// parseInfo reads a fenced block's language and optional tab label.
func parseInfo(fc *ast.FencedCodeBlock, src []byte) (language, tab string, err error) {
	var info string
	if fc.Info != nil {
		info = strings.TrimSpace(string(fc.Info.Segment.Value(src)))
	}
	lang := info
	if i := strings.IndexAny(info, " \t"); i >= 0 {
		lang = info[:i]
	}
	if strings.HasPrefix(lang, "tab=") {
		lang = ""
	}
	mapped, ok := codeLanguages[lang]
	if !ok {
		known := make([]string, 0, len(codeLanguages))
		for k := range codeLanguages {
			if k != "" {
				known = append(known, k)
			}
		}
		sort.Strings(known)
		return "", "", fmt.Errorf("code block language %q is not one vox-code-block renders (use one of: %s)", lang, strings.Join(known, ", "))
	}
	if m := infoTab.FindStringSubmatch(info); m != nil {
		tab = m[1]
	}
	return mapped, tab, nil
}

// codeRenderer emits fenced code as vox-code-block. Tab grouping happens
// afterwards, over the HTML, because it spans sibling blocks.
type codeRenderer struct{}

func (*codeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkSkipChildren, nil
		}
		fc := n.(*ast.FencedCodeBlock)
		lang, tab, err := parseInfo(fc, src)
		if err != nil {
			return ast.WalkStop, err
		}
		fmt.Fprintf(w, `<vox-code-block language="%s" copy-label="%s" copied-label="%s" copied-message="%s" data-i18n-attr="copy-label,copied-label,copied-message"`,
			lang, labelCopy, labelCopied, labelCopiedNotice)
		if tab != "" {
			fmt.Fprintf(w, ` data-tab="%s"`, html.EscapeString(tab))
		}
		w.WriteString("><code>")
		lines := fc.Lines()
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			// RawWrite escapes &, <, > and " itself; escaping first
			// as well would publish "&lt;" to a reader who copies it.
			gmhtml.DefaultWriter.RawWrite(w, seg.Value(src))
		}
		w.WriteString("</code></vox-code-block>\n")
		return ast.WalkSkipChildren, nil
	})
}

// post turns goldmark's HTML into the site's: it marks translatable
// blocks, builds callouts, tabs and screenshots, assigns heading anchors,
// and collects messages.
type post struct {
	locate   func(line int) (string, int)
	opts     Options
	anchors  map[string]bool
	headings []Heading
	messages []Message
	tabGroup int
	err      error
}

// blockTags are elements that cannot sit inside a translatable unit.
var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.Ul: true, atom.Ol: true, atom.Li: true, atom.Pre: true,
	atom.Blockquote: true, atom.Table: true, atom.Div: true, atom.Figure: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Hr: true,
}

func isBlock(n *html.Node) bool {
	return n.Type == html.ElementNode && (blockTags[n.DataAtom] || strings.HasPrefix(n.Data, "vox-"))
}

func (p *post) run(markup string) (*Rendered, error) {
	container := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(markup), container)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		container.AppendChild(n)
	}

	p.walk(container)
	p.groupTabs(container)
	if p.err != nil {
		return nil, p.err
	}
	stripLines(container)

	var b strings.Builder
	for c := container.FirstChild; c != nil; c = c.NextSibling {
		if err := html.Render(&b, c); err != nil {
			return nil, err
		}
	}
	return &Rendered{HTML: b.String(), Headings: p.headings, Anchors: p.anchors}, nil
}

func (p *post) fail(n *html.Node, format string, args ...any) {
	if p.err != nil {
		return
	}
	f, l := p.where(n)
	p.err = fmt.Errorf("%s:%d: %s", f, l, fmt.Sprintf(format, args...))
}

func (p *post) where(n *html.Node) (string, int) {
	for c := n; c != nil; c = c.Parent {
		if v, ok := getAttr(c, lineAttr); ok {
			line, _ := strconv.Atoi(v)
			return p.locate(line)
		}
	}
	return p.locate(0)
}

// mark makes n a translatable unit and records its message.
//
// A unit with no words outside its code - a table cell holding only a
// setting name, say - is left unmarked: there is nothing in it to
// translate, and a unit that can never be translated would otherwise
// count as missing in every locale forever.
func (p *post) mark(n *html.Node) {
	key := Key(n)
	if key == "" || !hasProse(n) {
		return
	}
	setAttr(n, "data-i18n-html", "")
	f, l := p.where(n)
	p.messages = append(p.messages, Message{ID: key, File: f, Line: l})
}

func (p *post) walk(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if c.Type == html.ElementNode {
			p.element(c)
		}
		c = next
	}
}

func (p *post) element(n *html.Node) {
	switch n.DataAtom {
	case atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		p.heading(n)
	case atom.P:
		if p.shotParagraph(n) {
			return
		}
		p.checkInline(n)
		p.mark(n)
	case atom.Li:
		p.listItem(n)
	case atom.Th, atom.Td:
		p.checkInline(n)
		p.mark(n)
	case atom.Blockquote:
		p.blockquote(n)
	case atom.Table:
		// voxblocks styles tables with a class rather than a component,
		// and a wide one scrolls inside its own box so a phone-width
		// page never scrolls sideways.
		setAttr(n, "class", "vox-table")
		wrap := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
		setAttr(wrap, "class", "guide-table")
		// Focusable, so a keyboard user can scroll a table wider than
		// the screen (WCAG 2.1.1) - its cells hold nothing focusable.
		setAttr(wrap, "tabindex", "0")
		n.Parent.InsertBefore(wrap, n)
		n.Parent.RemoveChild(n)
		wrap.AppendChild(n)
		p.walk(n)
	default:
		if n.Data == "vox-code-block" {
			return
		}
		p.walk(n)
	}
}

// checkInline rejects an image anywhere but on a line of its own: a
// screenshot is a figure, not a word in a sentence.
func (p *post) checkInline(n *html.Node) {
	var find func(*html.Node)
	find = func(c *html.Node) {
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			if k.Type == html.ElementNode && k.DataAtom == atom.Img {
				p.fail(n, "an image must be a paragraph of its own: ![](shot:name)")
			}
			find(k)
		}
	}
	find(n)
}

func (p *post) heading(n *html.Node) {
	level := int(n.Data[1] - '0')
	id := p.uniqueID(slugify(textContent(n)))
	setAttr(n, "id", id)
	p.mark(n)
	if level <= 3 {
		p.headings = append(p.headings, Heading{Level: level, ID: id, HTML: Key(n)})
	}
}

func (p *post) uniqueID(base string) string {
	if base == "" {
		base = "section"
	}
	id := base
	for i := 2; p.anchors[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	p.anchors[id] = true
	return id
}

// shotParagraph replaces a paragraph holding only ![...](shot:name) with
// the screenshot's markup. It reports whether it did.
func (p *post) shotParagraph(n *html.Node) bool {
	var img *html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.TextNode && strings.TrimSpace(c.Data) == "":
		case c.Type == html.ElementNode && c.DataAtom == atom.Img && img == nil:
			img = c
		default:
			return false
		}
	}
	if img == nil {
		return false
	}
	src, _ := getAttr(img, "src")
	name, ok := strings.CutPrefix(src, "shot:")
	if !ok {
		p.fail(n, "images must be declared screenshots, written ![](shot:name); got %q", src)
		return true
	}
	if p.opts.Shot == nil {
		p.fail(n, "screenshot %q: no screenshot renderer configured", name)
		return true
	}
	markup, err := p.opts.Shot(name)
	if err != nil {
		p.fail(n, "%v", err)
		return true
	}
	frag, err := html.ParseFragment(strings.NewReader(markup), n.Parent)
	if err != nil {
		p.fail(n, "screenshot %q: %v", name, err)
		return true
	}
	for _, f := range frag {
		n.Parent.InsertBefore(f, n)
	}
	n.Parent.RemoveChild(n)
	return true
}

// listItem marks a list item whole when it holds only inline content.
// One that holds blocks - a loose list's paragraphs, or a nested list -
// has its block children handled in turn, and each run of inline content
// between them wrapped in a span of its own, so no piece of text escapes
// translation and no unit contains a block.
func (p *post) listItem(n *html.Node) {
	hasBlock := false
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if isBlock(c) {
			hasBlock = true
			break
		}
	}
	if !hasBlock {
		p.checkInline(n)
		p.mark(n)
		return
	}

	var run []*html.Node
	flush := func(before *html.Node) {
		if len(run) == 0 {
			return
		}
		blank := true
		for _, r := range run {
			if !(r.Type == html.TextNode && strings.TrimSpace(r.Data) == "") {
				blank = false
			}
		}
		if !blank {
			span := &html.Node{Type: html.ElementNode, Data: "span", DataAtom: atom.Span}
			n.InsertBefore(span, before)
			for _, r := range run {
				n.RemoveChild(r)
				span.AppendChild(r)
			}
			// The line lives on the li; where() climbs to it.
			p.mark(span)
		}
		run = nil
	}
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if isBlock(c) {
			flush(c)
			p.element(c)
		} else {
			run = append(run, c)
		}
		c = next
	}
	flush(nil)
}

// alertMarker matches the first line of a GitHub alert.
var alertMarker = regexp.MustCompile(`^\s*\[!([A-Z]+)\]\s*`)

// blockquote turns a GitHub alert into a vox-callout; any other quote
// is kept and its contents marked.
func (p *post) blockquote(n *html.Node) {
	first := firstElement(n)
	if first == nil || first.DataAtom != atom.P || first.FirstChild == nil || first.FirstChild.Type != html.TextNode {
		p.walk(n)
		return
	}
	m := alertMarker.FindStringSubmatch(first.FirstChild.Data)
	if m == nil {
		p.walk(n)
		return
	}
	c, ok := callouts[m[1]]
	if !ok {
		p.fail(n, "unknown alert type [!%s] (NOTE, TIP, WARNING, CAUTION)", m[1])
		return
	}
	first.FirstChild.Data = strings.TrimLeft(first.FirstChild.Data[len(m[0]):], " \n")
	if first.FirstChild.Data == "" {
		first.RemoveChild(first.FirstChild)
	}
	// goldmark ends the marker line with a soft break; drop a leading
	// <br> left behind by a hard one.
	if fc := first.FirstChild; fc != nil && fc.Type == html.ElementNode && fc.DataAtom == atom.Br {
		first.RemoveChild(fc)
	}
	if first.FirstChild == nil {
		n.RemoveChild(first)
	}

	n.Data, n.DataAtom = "vox-callout", 0
	setAttr(n, "variant", c.variant)
	setAttr(n, "heading", c.heading)
	setAttr(n, "data-i18n-attr", "heading")
	p.walk(n)
}

// groupTabs gathers each run of adjacent code blocks carrying a tab label
// into one vox-tabs. Tabs are keyed by label so the site's script can
// switch every group on a page together.
func (p *post) groupTabs(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data != "vox-code-block" {
			p.groupTabs(c)
		}
	}
	for c := n.FirstChild; c != nil; {
		if !isTabBlock(c) {
			c = c.NextSibling
			continue
		}
		var group []*html.Node
		k := c
		for k != nil && (isTabBlock(k) || (k.Type == html.TextNode && strings.TrimSpace(k.Data) == "")) {
			if isTabBlock(k) {
				group = append(group, k)
			}
			k = k.NextSibling
		}
		p.tabGroup++
		tabs := &html.Node{Type: html.ElementNode, Data: "vox-tabs"}
		setAttr(tabs, "class", "guide-tabs")
		n.InsertBefore(tabs, c)
		seen := map[string]bool{}
		for i, block := range group {
			label, _ := getAttr(block, "data-tab")
			key := slugify(label)
			if seen[key] {
				p.fail(block, "tab %q appears twice in one group", label)
			}
			seen[key] = true
			panel := fmt.Sprintf("tab%d-%s", p.tabGroup, key)

			tab := &html.Node{Type: html.ElementNode, Data: "vox-tab"}
			setAttr(tab, "slot", "tab")
			setAttr(tab, "panel", panel)
			setAttr(tab, "data-tab-key", key)
			if i == 0 {
				setAttr(tab, "selected", "")
			}
			tab.AppendChild(&html.Node{Type: html.TextNode, Data: label})
			tabs.AppendChild(tab)

			f, l := p.where(block)
			removeAttr(block, "data-tab")
			pane := &html.Node{Type: html.ElementNode, Data: "vox-tab-panel"}
			setAttr(pane, "name", panel)
			block.Parent.RemoveChild(block)
			pane.AppendChild(block)
			tabs.AppendChild(pane)

			setAttr(tab, "data-i18n-html", "")
			p.messages = append(p.messages, Message{ID: Key(tab), File: f, Line: l})
		}
		// Remove the whitespace the group used to span.
		for c2 := tabs.NextSibling; c2 != k; {
			next := c2.NextSibling
			n.RemoveChild(c2)
			c2 = next
		}
		c = k
	}
}

func isTabBlock(n *html.Node) bool {
	if n == nil || n.Type != html.ElementNode || n.Data != "vox-code-block" {
		return false
	}
	_, ok := getAttr(n, "data-tab")
	return ok
}

func stripLines(n *html.Node) {
	removeAttr(n, lineAttr)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		stripLines(c)
	}
}

func firstElement(n *html.Node) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			return c
		}
	}
	return nil
}

// hasProse reports whether n has a letter anywhere outside <code>.
func hasProse(n *html.Node) bool {
	if n.Type == html.ElementNode && n.DataAtom == atom.Code {
		return false
	}
	if n.Type == html.TextNode {
		for _, r := range n.Data {
			if unicode.IsLetter(r) {
				return true
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if hasProse(c) {
			return true
		}
	}
	return false
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
		for k := c.FirstChild; k != nil; k = k.NextSibling {
			walk(k)
		}
	}
	walk(n)
	return b.String()
}

// slugify turns heading text into an anchor: lower case, letters and
// digits kept, every other run collapsed to one hyphen.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

func getAttr(n *html.Node, name string) (string, bool) {
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
