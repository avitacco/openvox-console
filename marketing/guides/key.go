package guides

import (
	"strings"

	"golang.org/x/net/html"
)

// Key is the message key of an element marked data-i18n-html: its inner
// HTML in one canonical spelling, with whitespace runs collapsed.
//
// The site's translator looks prose up by this key and the extractor
// offers translators exactly this key, so the two agree by sharing one
// function rather than by two serializers happening to match.
//
// The spelling is html.Render's with one difference: in text, only &, <
// and > are escaped. html.Render also escapes ' and ", which is valid
// but puts "doesn&#39;t" in front of every translator - and a translator
// who types a plain apostrophe back produces a key nothing looks up.
// Attribute values keep " escaped, since they are quoted with it.
func Key(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeKey(&b, c)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// voidElements have no end tag.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true,
	"img": true, "input": true, "link": true, "meta": true, "source": true,
	"track": true, "wbr": true,
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	attrEscaper = strings.NewReplacer("&", "&amp;", `"`, "&quot;")
)

func writeKey(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(textEscaper.Replace(n.Data))
	case html.ElementNode:
		b.WriteByte('<')
		b.WriteString(n.Data)
		for _, a := range n.Attr {
			if a.Key == lineAttr {
				continue
			}
			b.WriteByte(' ')
			b.WriteString(a.Key)
			b.WriteString(`="`)
			b.WriteString(attrEscaper.Replace(a.Val))
			b.WriteByte('"')
		}
		b.WriteByte('>')
		if voidElements[n.Data] {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			writeKey(b, c)
		}
		b.WriteString("</")
		b.WriteString(n.Data)
		b.WriteByte('>')
	}
}
