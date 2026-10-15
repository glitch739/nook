// Package extract turns attached files into searchable text (ADR 0009).
//
// The text itself comes from an Apache Tika server (a separate
// container, never exposed outside the Docker network), which handles
// every common format and runs Tesseract OCR on scanned PDF pages and
// images. Worker (worker.go) feeds it the extraction queue one file at
// a time and stores the result in file_text, where SQLite triggers
// index it for internal/search.
package extract

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

// Chunk is one searchable piece of a file: one PDF page, or a whole
// document that has no pages.
type Chunk struct {
	// PageNo is the 1-based PDF page, 0 when the format has no pages.
	PageNo int
	// Part is the path of an embedded document ("/report.docx" inside a
	// zip, an e-mail's attachment), "" for the file itself.
	Part string
	// OCR: the text was read off an image (scanned page, photo).
	OCR  bool
	Text string
}

// Result is everything extracted from one file.
type Result struct {
	Chunks []Chunk
	// UsedOCR: some of the text kept was read off an image.
	UsedOCR bool
	// Problem is Tika's own note when it could only read part of the
	// file (or nothing): a corrupt embedded document, a password.
	Problem string
}

// Extractor is what Worker needs; *Tika in production, a fake in tests.
type Extractor interface {
	Extract(ctx context.Context, r io.Reader, filename string) (Result, error)
}

// ErrUnprocessable means retrying will not help: Tika could not parse
// this file at all (unsupported, corrupt, encrypted).
var ErrUnprocessable = errors.New("extract: file cannot be processed")

// ErrEncrypted is the ErrUnprocessable case worth telling users apart.
var ErrEncrypted = fmt.Errorf("%w: password protected", ErrUnprocessable)

// maxResponseBytes bounds Tika's JSON answer. Its XHTML is a few times
// the size of the plain text, so this leaves room for far more text
// than Worker keeps anyway (MaxTextBytes).
const maxResponseBytes = 64 << 20

// Tika talks to a tika-server's /rmeta/xml endpoint: one JSON object
// per document (the file, then every embedded one), each with its
// content as XHTML that keeps PDF page boundaries
// (<div class="page">).
type Tika struct {
	BaseURL string
	Client  *http.Client
}

func (t *Tika) Extract(ctx context.Context, r io.Reader, filename string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(t.BaseURL, "/")+"/rmeta/xml", r)
	if err != nil {
		return Result{}, fmt.Errorf("extract: build request: %w", err)
	}
	// The name helps Tika's type detection (e.g. old .doc vs .xls).
	req.Header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	req.Header.Set("Accept", "application/json")

	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("extract: tika: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("extract: read tika response: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusUnsupportedMediaType:
		if isEncrypted(string(body)) {
			return Result{}, ErrEncrypted
		}
		return Result{}, fmt.Errorf("%w: %s", ErrUnprocessable, firstLine(string(body)))
	case resp.StatusCode != http.StatusOK:
		return Result{}, fmt.Errorf("extract: tika: HTTP %d: %s", resp.StatusCode, firstLine(string(body)))
	case len(body) > maxResponseBytes:
		return Result{}, fmt.Errorf("%w: too much text", ErrUnprocessable)
	}
	return parseRmeta(body)
}

// parseRmeta reads /rmeta/xml's answer. Key names changed in Tika 4
// ("X-TIKA:content" -> "tk:content"); both are accepted.
func parseRmeta(body []byte) (Result, error) {
	var docs []map[string]any
	if err := json.Unmarshal(body, &docs); err != nil {
		return Result{}, fmt.Errorf("extract: decode tika response: %w", err)
	}
	var res Result
	for _, d := range docs {
		part := str(d, "tk:embedded-resource-path", "X-TIKA:embedded_resource_path")
		ocr := usedOCR(d)
		// An e-mail's subject, sender and recipients are metadata, not
		// content: without this they would not be searchable at all.
		if h := mailHeader(d); h != "" {
			res.Chunks = append(res.Chunks, Chunk{Part: part, Text: h})
		}
		for _, c := range splitPages(str(d, "tk:content", "X-TIKA:content")) {
			c.Part, c.OCR = part, ocr
			res.Chunks = append(res.Chunks, c)
			res.UsedOCR = res.UsedOCR || ocr
		}
		for k, v := range d {
			if !strings.Contains(strings.ToLower(k), "exception") {
				continue
			}
			msg := firstLine(fmt.Sprint(v))
			if isEncrypted(msg) {
				msg = "password protected"
			}
			if res.Problem == "" {
				res.Problem = msg
			}
		}
	}
	return res, nil
}

// mailHeader is the searchable header of an e-mail document (.eml,
// Outlook .msg, an e-mail attached to one): subject, from, to, cc, one
// per line. "" for anything that is not an e-mail.
func mailHeader(d map[string]any) string {
	if !strings.HasPrefix(str(d, "Content-Type"), "message/") && !strings.Contains(str(d, "Content-Type"), "outlook") &&
		strs(d, "message:from") == "" {
		return ""
	}
	var lines []string
	for _, f := range []struct{ label, key, alt string }{
		{"Subject", "dc:title", "dc:subject"},
		{"From", "message:from", "dc:creator"},
		{"To", "message:to", ""},
		{"Cc", "message:cc", ""},
	} {
		v := strs(d, f.key)
		if v == "" && f.alt != "" {
			v = strs(d, f.alt)
		}
		if v != "" {
			lines = append(lines, f.label+": "+v)
		}
	}
	return strings.Join(lines, "\n")
}

// strs is str for a field that may hold several values (recipients),
// joined with ", ".
func strs(d map[string]any, key string) string {
	switch v := d[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var out []string
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return strings.Join(out, ", ")
	}
	return ""
}

func usedOCR(d map[string]any) bool {
	if n := str(d, "pdf:ocr-page-count"); n != "" && n != "0" {
		return true
	}
	parsedBy, _ := json.Marshal(d["tk:parsed-by"])
	return bytes.Contains(parsedBy, []byte("TesseractOCRParser"))
}

func str(d map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := d[k].(type) {
		case string:
			return v
		case []any:
			if len(v) > 0 {
				if s, ok := v[0].(string); ok {
					return s
				}
			}
		}
	}
	return ""
}

func isEncrypted(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "encrypted") || strings.Contains(s, "password")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// blockElements end a line of text; without the break, words of two
// paragraphs or table cells would run together into one token.
var blockElements = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true, "td": true, "th": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"pre": true, "table": true, "ul": true, "ol": true, "title": true,
}

// splitPages turns one document's XHTML into chunks: one per
// <div class="page"> (PDF), plus one for text outside any page (every
// other format, where there are no page divs at all). Empty chunks are
// dropped.
func splitPages(xhtml string) []Chunk {
	if strings.TrimSpace(xhtml) == "" {
		return nil
	}
	dec := xml.NewDecoder(strings.NewReader(xhtml))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var (
		chunks    []Chunk
		outside   strings.Builder
		cur       *strings.Builder
		pageNo    int
		pageDepth int // element depth of the open page div, 0 = none open
		depth     int
		skipDepth int // inside <head>/<script>/<style>
	)
	flush := func() {
		if cur != nil {
			if t := tidy(cur.String()); t != "" {
				chunks = append(chunks, Chunk{PageNo: pageNo, Text: t})
			}
			cur = nil
		}
	}
	out := func() *strings.Builder {
		if cur != nil {
			return cur
		}
		return &outside
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break // io.EOF, or the rest of a malformed document
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			name := strings.ToLower(t.Name.Local)
			if skipDepth == 0 && (name == "head" || name == "script" || name == "style") {
				skipDepth = depth
			}
			if name == "div" && pageDepth == 0 && hasClass(t, "page") {
				flush()
				pageNo++
				pageDepth = depth
				cur = &strings.Builder{}
			}
			if blockElements[name] {
				out().WriteByte('\n')
			}
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			if blockElements[name] {
				out().WriteByte('\n')
			}
			if depth == pageDepth {
				flush()
				pageDepth = 0
			}
			if depth == skipDepth {
				skipDepth = 0
			}
			depth--
		case xml.CharData:
			if skipDepth == 0 {
				out().Write(t)
			}
		}
	}
	flush()
	if t := tidy(outside.String()); t != "" {
		chunks = append(chunks, Chunk{Text: t})
	}
	return chunks
}

func hasClass(e xml.StartElement, class string) bool {
	for _, a := range e.Attr {
		if a.Name.Local == "class" {
			for _, c := range strings.Fields(a.Value) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

// tidy collapses runs of blank space within lines and drops empty lines.
func tidy(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

// HTML converts a document (Word, OpenDocument, RTF) to XHTML with
// tika-server's /tika endpoint -- headings, paragraphs, lists and
// tables, no styling. Used for the in-browser preview of documents the
// browser can't show itself; callers sanitize the result. (/tika/html:
// current tika-server answers a plain /tika with Accept: text/html 406.)
func (t *Tika) HTML(ctx context.Context, r io.Reader, filename string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(t.BaseURL, "/")+"/tika/html", r)
	if err != nil {
		return nil, fmt.Errorf("extract: build request: %w", err)
	}
	req.Header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	client := t.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("extract: tika: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("extract: read tika response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnprocessableEntity && isEncrypted(string(body)) {
			return nil, ErrEncrypted
		}
		return nil, fmt.Errorf("extract: tika: HTTP %d: %s", resp.StatusCode, firstLine(string(body)))
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("%w: too much text", ErrUnprocessable)
	}
	return body, nil
}
