package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeDocs stands in for Tika: echoes the document's bytes as the body
// of an XHTML page, with a script the sanitizer must drop.
type fakeDocs struct{}

func (fakeDocs) HTML(_ context.Context, r io.Reader, _ string) ([]byte, error) {
	b, _ := io.ReadAll(r)
	// Like net/http with a request body: close it once read.
	if c, ok := r.(io.Closer); ok {
		c.Close()
	}
	text := string(b)
	if bytes.HasPrefix(b, []byte("PK")) {
		text = "(zip)"
	}
	return []byte(`<html><body><h1>Cím</h1><p>` + text + `</p><img src="embedded:image1.png"><script>alert(1)</script></body></html>`), nil
}

func TestDocView(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	anna, bob := e.client(), e.client()
	for _, u := range []struct {
		c    *http.Client
		name string
	}{{anna, "anna"}, {bob, "bob"}} {
		if code := e.do(t, u.c, "POST", "/api/auth/register", m{"email": u.name + "@example.hu", "username": u.name, "display_name": u.name, "password": "hosszujelszo"}, nil); code != 201 {
			t.Fatalf("register %s = %d", u.name, code)
		}
	}
	sp := e.privateSpace(t, anna)
	var page pageResponse
	e.do(t, anna, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Doksi"}, &page)
	_, doc := uploadFile(t, e, anna, page.ID, "ajanlat.docx", []byte("Ajánlat szövege"))
	_, pdf := uploadFile(t, e, anna, page.ID, "lap.pdf", []byte("%PDF-1.4 x"))

	get := func(c *http.Client, url string) (*http.Response, string) {
		resp, err := c.Get(e.srv.URL + url)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, string(b)
	}
	resp, body := get(anna, doc.URL+"/view")
	if resp.StatusCode != 200 || !strings.Contains(body, "Ajánlat szövege") || !strings.Contains(body, "<h1>") {
		t.Fatalf("view = %d %q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "<title>ajanlat.docx</title>") {
		t.Fatalf("missing title: %q", body)
	}
	if strings.Contains(body, "<script") || strings.Contains(body, "alert(1)") {
		t.Fatalf("script survived sanitizing: %q", body)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("CSP = %q", csp)
	}
	if strings.Contains(body, "embedded:") {
		t.Fatalf("unresolved picture reference kept: %q", body)
	}

	// A real zip package (.docx): its word/media picture is inlined.
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("word/media/image1.png")
	w.Write(testPNG)
	zw.Close()
	_, pic := uploadFile(t, e, anna, page.ID, "kepes.docx", zbuf.Bytes())
	if _, body := get(anna, pic.URL+"/view"); !strings.Contains(body, `src="data:image/png;base64,`) {
		t.Fatalf("picture not inlined: %q", body)
	}

	if resp, _ := get(anna, pdf.URL+"/view"); resp.StatusCode != 404 {
		t.Fatalf("view of a pdf = %d, want 404", resp.StatusCode)
	}
	if resp, _ := get(bob, doc.URL+"/view"); resp.StatusCode/100 == 2 {
		t.Fatalf("outsider view = %d, want refused", resp.StatusCode)
	}
}

// An attached e-mail opens as a page: its header fields above the
// sender's HTML, which is cleaned (no script) and sandboxed by CSP.
func TestMailView(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	anna, bob := e.client(), e.client()
	e.do(t, anna, "POST", "/api/auth/register", m{"email": "anna@example.hu", "username": "anna", "password": "hosszujelszo"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)
	sp := e.privateSpace(t, anna)
	var page pageResponse
	e.do(t, anna, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Levelek"}, &page)
	eml := "From: Kiss Anna <anna@example.hu>\r\nTo: bob@example.hu\r\nSubject: =?UTF-8?Q?Aj=C3=A1nlat?=\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		"<p>Hello <b>Bob</b></p><script>alert(1)</script><img src=\"https://tracker.example/p.gif\">"
	code, att := uploadFile(t, e, anna, page.ID, "ajanlat.eml", []byte(eml))
	if code != http.StatusCreated {
		t.Fatalf("upload .eml = %d", code)
	}
	resp, err := anna.Get(e.srv.URL + att.URL + "/view")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(b)
	if resp.StatusCode != 200 || !strings.Contains(body, "Ajánlat") || !strings.Contains(body, "Kiss Anna") || !strings.Contains(body, "<b>Bob</b>") {
		t.Fatalf("mail view = %d %q", resp.StatusCode, body)
	}
	if strings.Contains(body, "<script") || !strings.Contains(body, "<title>Ajánlat</title></head>") {
		t.Fatalf("mail view = %q", body)
	}
	// Remote pictures stay in the markup but the CSP never fetches them.
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "img-src data:;") {
		t.Fatalf("CSP = %q", resp.Header.Get("Content-Security-Policy"))
	}
	if r, _ := bob.Get(e.srv.URL + att.URL + "/view"); r.StatusCode != http.StatusNotFound {
		t.Fatalf("someone else's mail = %d, want 404", r.StatusCode)
	}
}
