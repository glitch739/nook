package mailview

import (
	"bytes"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// dropWithContent are removed together with everything inside them.
var dropWithContent = map[atom.Atom]bool{
	atom.Script: true, atom.Noscript: true, atom.Iframe: true, atom.Frame: true,
	atom.Frameset: true, atom.Object: true, atom.Embed: true, atom.Applet: true,
	atom.Template: true, atom.Svg: true, atom.Math: true, atom.Title: true,
}

// dropTag are removed, their content kept (a <form>'s fields still show
// what the sender wrote, they just can't be submitted anywhere).
var dropTag = map[atom.Atom]bool{
	atom.Meta: true, atom.Base: true, atom.Link: true, atom.Form: true,
	atom.Html: true, atom.Head: true, atom.Body: true,
}

// urlAttrs may carry a URL; only http(s), mailto and our own data:
// images survive in them.
var urlAttrs = map[string]bool{
	"href": true, "src": true, "action": true, "formaction": true, "background": true,
	"poster": true, "srcset": true, "xlink:href": true, "lowsrc": true, "dynsrc": true,
}

// Sanitize turns an e-mail's HTML part into a standalone, inert document:
// see the package comment. inline maps Content-IDs to data: URIs for
// the cid: images the HTML refers to. Every link opens in a new tab.
func Sanitize(src string, inline map[string]string) []byte {
	var out bytes.Buffer
	out.WriteString(`<!doctype html><html><head><meta charset="utf-8"><base target="_blank">`)
	out.WriteString(`<style>body{margin:16px;font:14px/1.45 system-ui,sans-serif;overflow-wrap:anywhere}img{max-width:100%;height:auto}</style>`)
	out.WriteString(`</head><body>`)

	z := html.NewTokenizer(strings.NewReader(src))
	skip := 0 // depth inside a dropWithContent element
	inStyle := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		tok := z.Token()
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			if dropWithContent[tok.DataAtom] || tok.DataAtom == 0 && isNamespaced(tok.Data) {
				if tt == html.StartTagToken && !isVoid(tok.DataAtom) {
					skip++
				}
				continue
			}
			if skip > 0 || dropTag[tok.DataAtom] {
				continue
			}
			tok.Attr = cleanAttrs(tok.Attr, inline)
			out.WriteString(tok.String())
			inStyle = tok.DataAtom == atom.Style && tt == html.StartTagToken
		case html.EndTagToken:
			if tok.DataAtom == atom.Style {
				inStyle = false
			}
			if dropWithContent[tok.DataAtom] || tok.DataAtom == 0 && isNamespaced(tok.Data) {
				if skip > 0 {
					skip--
				}
				continue
			}
			if skip > 0 || dropTag[tok.DataAtom] {
				continue
			}
			out.WriteString(tok.String())
		case html.TextToken:
			switch {
			case skip > 0:
			case inStyle:
				// Raw CSS: escaping would break "a > b". The tokenizer
				// already ended it at "</style", so it can't break out.
				out.WriteString(tok.Data)
			default:
				out.WriteString(tok.String()) // re-escaped by Token.String
			}
		}
		// Comments and doctypes are dropped: conditional comments are
		// how Outlook HTML smuggles in markup.
	}
	out.WriteString(`</body></html>`)
	return out.Bytes()
}

// cleanAttrs drops event handlers and anything that could load or run
// something from elsewhere, and swaps cid: references for data: URIs.
func cleanAttrs(attrs []html.Attribute, inline map[string]string) []html.Attribute {
	kept := attrs[:0]
	for _, a := range attrs {
		key := strings.ToLower(a.Key)
		if a.Namespace != "" || strings.HasPrefix(key, "on") || key == "formaction" || key == "srcdoc" {
			continue
		}
		if key == "style" && unsafeCSS(a.Val) {
			continue
		}
		if urlAttrs[key] {
			v := strings.TrimSpace(a.Val)
			lower := strings.ToLower(v)
			switch {
			case strings.HasPrefix(lower, "cid:"):
				data, ok := inline[strings.Trim(v[4:], "<> ")]
				if !ok {
					continue
				}
				a.Val = data
			case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"),
				strings.HasPrefix(lower, "mailto:"), strings.HasPrefix(lower, "#"):
				// Links work; remote images are blocked by the CSP anyway.
			default:
				continue
			}
		}
		kept = append(kept, a)
	}
	return kept
}

// unsafeCSS: old-browser script vectors in inline styles. The CSP already
// blocks url() fetches; this keeps the markup itself clean.
func unsafeCSS(v string) bool {
	l := strings.ToLower(v)
	return strings.Contains(l, "expression(") || strings.Contains(l, "javascript:") ||
		strings.Contains(l, "behavior:") || strings.Contains(l, "-moz-binding")
}

func isNamespaced(tag string) bool { return strings.Contains(tag, ":") }

func isVoid(a atom.Atom) bool {
	switch a {
	case atom.Area, atom.Br, atom.Col, atom.Embed, atom.Hr, atom.Img, atom.Input,
		atom.Link, atom.Meta, atom.Param, atom.Source, atom.Track, atom.Wbr:
		return true
	}
	return false
}
