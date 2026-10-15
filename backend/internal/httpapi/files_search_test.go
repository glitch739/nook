package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
)

type fakeIndex struct{ wakes atomic.Int32 }

func (f *fakeIndex) Wake() { f.wakes.Add(1) }

func uploadFile(t *testing.T, e *testEnv, c *http.Client, pageID, name string, data []byte) (int, attachmentResponse) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/pages/"+pageID+"/attachments", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out attachmentResponse
	if resp.StatusCode == http.StatusCreated {
		json.NewDecoder(resp.Body).Decode(&out)
	}
	return resp.StatusCode, out
}

func TestFileSearchEndpoints(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	anna := e.client()
	if code := e.do(t, anna, "POST", "/api/auth/register", m{"email": "anna@example.hu", "username": "anna", "display_name": "Anna", "password": "hosszujelszo"}, nil); code != 201 {
		t.Fatalf("register anna = %d", code)
	}
	sp := e.privateSpace(t, anna)
	var page pageResponse
	if code := e.do(t, anna, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Szerződések", "kind": "document"}, &page); code != 201 {
		t.Fatalf("create page = %d", code)
	}

	code, f := uploadFile(t, e, anna, page.ID, "berleti-szerzodes.pdf", []byte("%PDF-1.4 x"))
	if code != http.StatusCreated {
		t.Fatalf("upload = %d", code)
	}
	if e.index.wakes.Load() != 1 {
		t.Fatalf("upload woke the extractor %d times, want 1", e.index.wakes.Load())
	}

	var st fileIndexResponse
	if code := e.do(t, anna, "GET", f.URL+"/index", nil, &st); code != 200 || st.Status != "pending" || st.HasText {
		t.Fatalf("index status = %d %+v, want pending, no text", code, st)
	}

	// What the editor and the extractor would do: the block goes into
	// the page, the text arrives (page 3 of a scan).
	e.db.Exec(`UPDATE pages SET content_json = ? WHERE id = ?`,
		`{"type":"doc","content":[{"type":"fileAttachment","attrs":{"src":"`+f.URL+`"}}]}`, page.ID)
	e.db.Exec(`DELETE FROM file_text WHERE file_id = ?`, f.ID)
	e.db.Exec(`INSERT INTO file_text (file_id, page_no, ocr, body) VALUES (?, 3, 1, 'A bérleti díj havonta esedékes')`, f.ID)
	e.db.Exec(`UPDATE file_extractions SET status = 'done', used_ocr = 1 WHERE file_id = ?`, f.ID)

	st = fileIndexResponse{}
	if e.do(t, anna, "GET", f.URL+"/index", nil, &st); st.Status != "done" || !st.HasText || !st.UsedOCR {
		t.Fatalf("index status after extraction = %+v", st)
	}

	var res []searchResultResponse
	if code := e.do(t, anna, "GET", "/api/search?q="+url.QueryEscape("berleti dij"), nil, &res); code != 200 {
		t.Fatalf("search = %d", code)
	}
	if len(res) != 1 || len(res[0].Files) != 1 {
		t.Fatalf("results = %+v, want the page with its file", res)
	}
	hit := res[0].Files[0]
	if hit.URL != f.URL || hit.PageNo != 3 || !hit.Previewable || !hit.UsedOCR || hit.Snippet == "" {
		t.Fatalf("file hit = %+v", hit)
	}

	// Bob, outside Anna's space: no hit, no status.
	bob := e.client()
	if code := e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "display_name": "Bob", "password": "hosszujelszo"}, nil); code != 201 {
		t.Fatalf("register bob = %d", code)
	}
	res = nil
	e.do(t, bob, "GET", "/api/search?q="+url.QueryEscape("berleti dij"), nil, &res)
	if len(res) != 0 {
		t.Fatalf("bob finds anna's file: %+v", res)
	}
	if code := e.do(t, bob, "GET", f.URL+"/index", nil, nil); code != 403 && code != 404 {
		t.Fatalf("bob reads anna's file status = %d", code)
	}
}
