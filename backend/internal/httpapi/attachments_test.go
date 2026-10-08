package httpapi

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestAttachments(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice := e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	var g, sp, pg, other idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "G"}, &g)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "S", "type": "private", "group_id": g.ID}, &sp)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "P", "kind": "document"}, &pg)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Other", "kind": "document"}, &other)

	upload := func(c *http.Client, pageID, name string, body []byte) (*http.Response, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", name)
		fw.Write(body)
		mw.Close()
		req, _ := http.NewRequest("POST", e.srv.URL+"/api/pages/"+pageID+"/attachments", &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return res, string(b)
	}

	pdf := []byte("%PDF-1.7\nfake pdf body")
	res, body := upload(alice, pg.ID, "../../etc/Számla 2026.pdf", pdf)
	if res.StatusCode != 201 || !strings.Contains(body, `"filename":"Számla 2026.pdf"`) {
		t.Fatalf("upload pdf = %d %s", res.StatusCode, body)
	}
	url := body[strings.Index(body, `"url":"`)+7:]
	url = url[:strings.Index(url, `"`)]

	if res, _ := upload(alice, pg.ID, "virus.exe", []byte("MZ")); res.StatusCode != 400 {
		t.Fatalf("exe upload = %d, want 400", res.StatusCode)
	}
	if res, _ := upload(alice, pg.ID, "page.html", []byte("<script>")); res.StatusCode != 400 {
		t.Fatalf("html upload = %d, want 400", res.StatusCode)
	}
	if res, _ := upload(alice, pg.ID, "big.zip", make([]byte, 25<<20+1)); res.StatusCode != 413 {
		t.Fatalf("oversized upload = %d, want 413", res.StatusCode)
	}

	resp, err := alice.Get(e.srv.URL + url)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(got, pdf) {
		t.Fatalf("download = %d", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "Sz") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") {
		t.Fatalf("download CSP = %q", csp)
	}
	if resp.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}

	// Scoped to its page; unknown to other users.
	wrong := strings.Replace(url, pg.ID, other.ID, 1)
	if r2, _ := alice.Get(e.srv.URL + wrong); r2.StatusCode != 404 {
		t.Fatalf("attachment via another page = %d, want 404", r2.StatusCode)
	}
	bob := e.client()
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)
	if r3, _ := bob.Get(e.srv.URL + url); r3.StatusCode != 403 && r3.StatusCode != 404 {
		t.Fatalf("bob download = %d", r3.StatusCode)
	}
}
