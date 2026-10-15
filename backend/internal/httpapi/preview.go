package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"html"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"nook/internal/db"
	"nook/internal/extract"
	"nook/internal/mailview"
	"nook/internal/spaces"
)

// DocConverter turns a document into HTML (extract.Tika.HTML). nil: no
// document preview.
type DocConverter interface {
	HTML(ctx context.Context, r io.Reader, filename string) ([]byte, error)
}

// docViewTimeout bounds one conversion; a document preview is a normal
// download otherwise (transferKind), so it has a deadline of its own.
const docViewTimeout = 60 * time.Second

var (
	docExt  = regexp.MustCompile(`(?i)\.(docx|doc|odt|rtf)$`)
	mailExt = regexp.MustCompile(`(?i)\.eml$`)
)

// viewCSP: what a preview page may do -- nothing active, nothing fetched
// from anywhere, no access to the Nook origin; links open in a new tab.
const viewCSP = "sandbox allow-popups allow-popups-to-escape-sandbox; default-src 'none'; " +
	"img-src data:; style-src 'unsafe-inline'; font-src data:; " +
	"frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// viewHandler handles GET .../attachments/{attachmentID}/view: an e-mail
// (.eml, mailview) or a Word / OpenDocument / RTF document (Tika) as a
// readable page. Both go through the same sanitizer (mailview.Sanitize)
// and the same sandbox CSP. Other files: 404 (PDFs and images open with
// ?preview=1 on the file itself).
func (a attachmentsAPI) viewHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.covers.authorizePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	f, meta, err := a.covers.assets.OpenAttachment(r.Context(), page.ID, r.PathValue("attachmentID"))
	if errors.Is(err, db.ErrNotFound) || err == nil && !docExt.MatchString(meta.Filename) && !mailExt.MatchString(meta.Filename) {
		if err == nil {
			f.Close()
		}
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	defer f.Close()
	h := w.Header()
	if mailExt.MatchString(meta.Filename) {
		m, err := mailview.Parse(f)
		if err != nil {
			http.Error(w, "this e-mail can't be shown -- download it to open it", http.StatusUnprocessableEntity)
			return
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "private, no-store")
		h.Set("Content-Security-Policy", viewCSP)
		h.Set("Referrer-Policy", "no-referrer")
		title := `<title>` + html.EscapeString(orDefault(m.Subject, meta.Filename)) + `</title></head>`
		page := bytes.Replace(m.Body, []byte("</head>"), []byte(title), 1)
		w.Write(bytes.Replace(page, []byte("<body>"), []byte("<body>"+mailHeader(m)), 1))
		return
	}
	if a.docs == nil {
		http.Error(w, "document preview is not available on this server -- download the file instead", http.StatusNotImplemented)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), docViewTimeout)
	defer cancel()
	// Hidden Close: net/http closes a request body once sent, and the
	// file is read again below for its pictures.
	xhtml, err := a.docs.HTML(ctx, struct{ io.Reader }{f}, meta.Filename)
	if errors.Is(err, extract.ErrEncrypted) {
		http.Error(w, "this document is password protected -- download it to open it", http.StatusUnprocessableEntity)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Security-Policy", viewCSP)
	h.Set("Referrer-Policy", "no-referrer")
	// Pictures: Tika leaves <img src="embedded:image1.png"> pointing into
	// the package; for .docx/.odt (zip files) they are pulled out here
	// and passed as inline data: images, the way an e-mail's cid: images
	// are -- nothing is fetched from anywhere.
	images := docImages(f, meta.SizeBytes)
	xhtml = embeddedRef.ReplaceAllFunc(xhtml, func(m []byte) []byte {
		name := string(embeddedRef.FindSubmatch(m)[1])
		if _, ok := images[name]; !ok {
			return m
		}
		return []byte(`src="cid:` + name + `"`)
	})

	// Readable defaults on top of the sanitizer's own: a page-width
	// column, ruled tables, and the file name as the tab title.
	head := `<title>` + html.EscapeString(meta.Filename) + `</title><style>` +
		`body{max-width:60rem;margin:24px auto;padding:0 16px}` +
		`table{border-collapse:collapse;margin:8px 0}td,th{border:1px solid #c8c8c8;padding:4px 8px;vertical-align:top}` +
		`</style></head>`
	w.Write(bytes.Replace(mailview.Sanitize(string(xhtml), images), []byte("</head>"), []byte(head), 1))
}

// mailHeader is the e-mail's header fields as an escaped block above its
// body.
func mailHeader(m *mailview.Mail) string {
	var b strings.Builder
	b.WriteString(`<table style="border-collapse:collapse;margin:0 0 16px;font:13px/1.5 system-ui,sans-serif">`)
	row := func(k, v string) {
		if v != "" {
			b.WriteString(`<tr><th style="text-align:left;padding:2px 12px 2px 0;color:#666">` + k +
				`</th><td>` + html.EscapeString(v) + `</td></tr>`)
		}
	}
	row("Subject", m.Subject)
	row("From", m.From)
	row("To", m.To)
	row("Cc", m.Cc)
	if !m.Date.IsZero() {
		row("Date", m.Date.Format("2006-01-02 15:04 -0700"))
	}
	names := make([]string, 0, len(m.Attachments))
	for _, att := range m.Attachments {
		names = append(names, att.Name)
	}
	row("Attachments", strings.Join(names, ", "))
	b.WriteString(`</table><hr style="border:0;border-top:1px solid #ddd;margin:0 0 16px">`)
	return b.String()
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

var embeddedRef = regexp.MustCompile(`src="embedded:([^"/]+)"`)

// Pictures a browser can show by themselves; Word's EMF/WMF clip-art
// can't be, and stays out.
var imageTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
}

const (
	maxDocImage  = 10 << 20 // per picture
	maxDocImages = 25 << 20 // all pictures of one document
)

// docImages reads the pictures of a zip-based document (.docx keeps
// them in word/media/, .odt in Pictures/) as data: URIs keyed by file
// name, the name Tika's embedded: references use. Anything else (not
// a zip: .doc, .rtf) gives none.
func docImages(r io.ReaderAt, size int64) map[string]string {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	total := int64(0)
	for _, zf := range zr.File {
		typ, ok := imageTypes[strings.ToLower(path.Ext(zf.Name))]
		n := int64(zf.UncompressedSize64)
		if !ok || n > maxDocImage || total+n > maxDocImages {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxDocImage+1))
		rc.Close()
		if err != nil || int64(len(data)) > maxDocImage {
			continue
		}
		total += int64(len(data))
		out[path.Base(zf.Name)] = "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	return out
}
