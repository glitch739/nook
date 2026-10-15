// Package mailview turns an attached e-mail (.eml) into something a
// browser can show: its decoded header fields, and its body as a
// self-contained, inert HTML document.
//
// The body is the sender's HTML, so it is treated as hostile. It is
// cleaned here (no script, no embedded objects, no forms, no meta
// refresh, no event handlers, no javascript: links), its inline cid:
// images become data: URIs, and the HTTP handler serves it under a
// sandbox CSP that blocks scripts and any outside fetch on top of that
// -- remote images included, like a mail client that doesn't "download
// pictures" by default.
package mailview

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/net/html/charset"
)

// Mail is a parsed e-mail, ready to render.
type Mail struct {
	Subject, From, To, Cc string
	Date                  time.Time
	// Body is a complete HTML document: the sender's HTML part cleaned
	// (Sanitize), or the plain-text part wrapped in <pre>.
	Body []byte
	// Attachments lists the files attached to the mail, by name.
	Attachments []Attachment
}

type Attachment struct {
	Name string
	Size int
}

// maxInlineImages bounds the cid: images turned into data: URIs, so a
// mail stuffed with pictures can't blow up the preview.
const maxInlineImages = 30 << 20

var wordDecoder = &mime.WordDecoder{CharsetReader: charset.NewReaderLabel}

// ErrNotMail means the bytes are not an RFC 822 message (an Outlook .msg
// is a binary format, for example); show the extracted text instead.
var ErrNotMail = errors.New("mailview: not an RFC 822 message")

// Parse reads one e-mail.
func Parse(r io.Reader) (*Mail, error) {
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotMail, err)
	}
	h := msg.Header
	m := &Mail{
		Subject: decodeWords(h.Get("Subject")),
		From:    addresses(h.Get("From")),
		To:      addresses(h.Get("To")),
		Cc:      addresses(h.Get("Cc")),
	}
	if d, err := h.Date(); err == nil {
		m.Date = d
	}

	var p parts
	p.inline = map[string]string{}
	if err := p.walk(h, msg.Body); err != nil && p.html == "" && p.text == "" {
		return nil, fmt.Errorf("mailview: read body: %w", err)
	}
	m.Attachments = p.attachments

	switch {
	case p.html != "":
		m.Body = Sanitize(p.html, p.inline)
	default:
		m.Body = []byte(`<!doctype html><html><head><meta charset="utf-8"></head><body>` +
			`<pre style="white-space:pre-wrap;font:14px/1.45 system-ui,sans-serif">` +
			html.EscapeString(p.text) + `</pre></body></html>`)
	}
	return m, nil
}

// header is what walk needs of mail.Header and textproto.MIMEHeader.
type header interface{ Get(string) string }

type parts struct {
	html, text  string
	inline      map[string]string // Content-ID -> data: URI
	inlineBytes int
	attachments []Attachment
}

// walk visits a MIME entity: multipart containers recursively, the
// first text/html and text/plain bodies, cid: images, attachments.
func (p *parts) walk(h header, body io.Reader) error {
	ctype, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil {
		ctype, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(ctype, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			part, err := mr.NextRawPart()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := p.walk(part.Header, part); err != nil {
				return err
			}
		}
	}

	data, err := io.ReadAll(decodeTransfer(h.Get("Content-Transfer-Encoding"), body))
	if err != nil {
		return err
	}
	disp, dparams, _ := mime.ParseMediaType(h.Get("Content-Disposition"))
	name := decodeWords(dparams["filename"])
	if name == "" {
		name = decodeWords(params["name"])
	}
	cid := strings.Trim(h.Get("Content-ID"), "<> ")

	switch {
	case ctype == "text/html" && disp != "attachment" && p.html == "":
		p.html = toUTF8(data, params["charset"])
	case ctype == "text/plain" && disp != "attachment" && p.text == "":
		p.text = toUTF8(data, params["charset"])
	case strings.HasPrefix(ctype, "image/") && cid != "" && disp != "attachment":
		if p.inlineBytes+len(data) <= maxInlineImages {
			p.inline[cid] = "data:" + ctype + ";base64," + base64.StdEncoding.EncodeToString(data)
			p.inlineBytes += len(data)
		}
	default:
		if name == "" {
			name = "attachment"
		}
		p.attachments = append(p.attachments, Attachment{Name: name, Size: len(data)})
	}
	return nil
}

func decodeTransfer(enc string, r io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, &stripSpace{r: r})
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	}
	return r
}

// stripSpace drops the line breaks base64 bodies are wrapped in.
type stripSpace struct{ r io.Reader }

func (s *stripSpace) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	j := 0
	for _, b := range p[:n] {
		if b != '\r' && b != '\n' && b != ' ' && b != '\t' {
			p[j] = b
			j++
		}
	}
	return j, err
}

// toUTF8 converts a body from its declared charset (Hungarian mail is
// often iso-8859-2 or windows-1250); unknown labels pass through.
func toUTF8(b []byte, label string) string {
	if label == "" || strings.EqualFold(label, "utf-8") || strings.EqualFold(label, "us-ascii") {
		return string(b)
	}
	r, err := charset.NewReaderLabel(label, bytes.NewReader(b))
	if err != nil {
		return string(b)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		return string(b)
	}
	return string(out)
}

func decodeWords(s string) string {
	if d, err := wordDecoder.DecodeHeader(s); err == nil {
		return d
	}
	return s
}

// addresses renders an address header as "Name <addr>, ...", decoding
// encoded names; a header that doesn't parse is shown decoded as is.
func addresses(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	p := mail.AddressParser{WordDecoder: wordDecoder}
	list, err := p.ParseList(s)
	if err != nil {
		return decodeWords(s)
	}
	out := make([]string, 0, len(list))
	for _, a := range list {
		if a.Name != "" {
			out = append(out, a.Name+" <"+a.Address+">")
		} else {
			out = append(out, a.Address)
		}
	}
	return strings.Join(out, ", ")
}
