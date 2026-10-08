package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestExport(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	alice, bob := e.client(), e.client()
	e.do(t, alice, "POST", "/api/auth/register", m{"email": "alice@example.hu", "username": "alice", "password": "hosszujelszo"}, nil)
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "password": "hosszujelszo"}, nil)
	var g, sp, pg, child, dbp, row, prop idResp
	e.do(t, alice, "POST", "/api/groups", m{"name": "Work"}, &g)
	e.do(t, alice, "POST", "/api/spaces", m{"name": "Notes", "type": "private", "group_id": g.ID}, &sp)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Plan", "kind": "document"}, &pg)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Step 1", "parent_page_id": pg.ID, "kind": "document"}, &child)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Tasks", "kind": "database"}, &dbp)
	e.do(t, alice, "POST", "/api/spaces/"+sp.ID+"/pages", m{"title": "Buy milk", "parent_page_id": dbp.ID, "kind": "document"}, &row)
	e.do(t, alice, "POST", "/api/pages/"+dbp.ID+"/properties", m{"name": "Status", "type": "select"}, &prop)
	e.do(t, alice, "PUT", "/api/properties/"+prop.ID, m{"options": []m{{"id": "o1", "label": "Done", "color": "#4ade80"}}}, nil)
	e.do(t, alice, "PUT", "/api/pages/"+row.ID+"/properties/"+prop.ID, m{"value": m{"option_id": "o1"}}, nil)

	// An attached file referenced from the page text.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "spec sheet.pdf")
	fw.Write([]byte("%PDF-1.4 spec"))
	mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/pages/"+pg.ID+"/attachments", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res, _ := alice.Do(req)
	var att struct{ URL string }
	json.NewDecoder(res.Body).Decode(&att)
	res.Body.Close()
	content := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"See the spec"}]},{"type":"fileAttachment","attrs":{"href":"` + att.URL + `","filename":"spec sheet.pdf","size":13}}]}`
	if code := e.do(t, alice, "PUT", "/api/pages/"+pg.ID, m{"content_json": content}, nil); code != 200 {
		t.Fatalf("set content = %d", code)
	}

	get := func(c *http.Client, path string) (*http.Response, []byte) {
		r, err := c.Get(e.srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r, b
	}

	r, body := get(alice, "/api/export.zip")
	if r.StatusCode != 200 || !strings.Contains(r.Header.Get("Content-Disposition"), "nook-export-") {
		t.Fatalf("export.zip = %d", r.StatusCode)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		parts := strings.SplitN(f.Name, "/", 2)
		files[parts[1]] = string(b)
	}
	plan := files["Work/Notes/Plan.md"]
	if !strings.Contains(plan, "# Plan") || !strings.Contains(plan, "See the spec") || !strings.Contains(plan, "](_files/") {
		t.Fatalf("Plan.md = %q (files: %v)", plan, keysOf(files))
	}
	if _, ok := files["Work/Notes/Plan/Step 1.md"]; !ok {
		t.Fatalf("sub-page missing: %v", keysOf(files))
	}
	foundPDF := false
	for name, data := range files {
		if strings.HasPrefix(name, "Work/Notes/_files/") && strings.HasSuffix(name, "spec sheet.pdf") && data == "%PDF-1.4 spec" {
			foundPDF = true
		}
	}
	if !foundPDF {
		t.Fatalf("attached file not in zip: %v", keysOf(files))
	}
	if tasks := files["Work/Notes/Tasks.md"]; !strings.Contains(tasks, "| Buy milk | Done |") {
		t.Fatalf("Tasks.md = %q", tasks)
	}
	var dump struct {
		Spaces []struct {
			Pages []struct {
				Title   string          `json:"title"`
				Content json.RawMessage `json:"content"`
			} `json:"pages"`
		} `json:"spaces"`
	}
	if err := json.Unmarshal([]byte(files["nook-export.json"]), &dump); err != nil || len(dump.Spaces) == 0 {
		t.Fatalf("json dump: %v", err)
	}

	// Bob's export doesn't contain Alice's space.
	_, bobZip := get(bob, "/api/export.zip")
	if bytes.Contains(bobZip, []byte("Plan.md")) {
		t.Fatal("bob's export contains alice's pages")
	}

	// Single page.
	r, md := get(alice, "/api/pages/"+pg.ID+"/export.md")
	if r.StatusCode != 200 || !strings.HasPrefix(string(md), "# Plan") {
		t.Fatalf("export.md = %d %q", r.StatusCode, md)
	}
	if r2, _ := get(bob, "/api/pages/"+pg.ID+"/export.md"); r2.StatusCode == 200 {
		t.Fatal("bob exported alice's page")
	}
}

func keysOf(mp map[string]string) []string {
	out := []string{}
	for k := range mp {
		out = append(out, k)
	}
	return out
}
