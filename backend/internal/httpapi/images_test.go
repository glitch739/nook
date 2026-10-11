package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"
)

var testPNG = append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), make([]byte, 64)...)

func uploadImage(t *testing.T, e *testEnv, c *http.Client, pageID string, data []byte) (int, imageResponse) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "shot.png")
	fw.Write(data)
	mw.Close()
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/pages/"+pageID+"/images", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out imageResponse
	if resp.StatusCode == http.StatusCreated {
		json.NewDecoder(resp.Body).Decode(&out)
	}
	return resp.StatusCode, out
}

func TestPageImages(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	anna := e.client()
	if code := e.do(t, anna, "POST", "/api/auth/register", m{"email": "anna@example.hu", "username": "anna", "display_name": "Anna", "password": "hosszujelszo"}, nil); code != 201 {
		t.Fatalf("register anna = %d", code)
	}
	var spaces []spaceResponse
	e.do(t, anna, "GET", "/api/spaces", nil, &spaces)
	var page pageResponse
	if code := e.do(t, anna, "POST", "/api/spaces/"+spaces[0].ID+"/pages", m{"title": "Képes oldal", "kind": "document"}, &page); code != 201 {
		t.Fatalf("create page = %d", code)
	}

	code, img := uploadImage(t, e, anna, page.ID, testPNG)
	if code != http.StatusCreated || img.MimeType != "image/png" || img.URL != "/api/pages/"+page.ID+"/images/"+img.ID {
		t.Fatalf("upload = %d %+v", code, img)
	}

	resp, err := anna.Get(e.srv.URL + img.URL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.Equal(data, testPNG) || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("get = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// HTML disguised as a PNG upload is refused.
	if code, _ := uploadImage(t, e, anna, page.ID, []byte("<html><script>alert(1)</script>")); code != 400 {
		t.Fatalf("non-image upload = %d", code)
	}

	// Another user can neither read nor add images on anna's page.
	bob := e.client()
	e.do(t, bob, "POST", "/api/auth/register", m{"email": "bob@example.hu", "username": "bob", "display_name": "Bob", "password": "hosszujelszo"}, nil)
	if resp, _ := bob.Get(e.srv.URL + img.URL); resp.StatusCode != 404 {
		t.Fatalf("bob read = %d", resp.StatusCode)
	}
	if code, _ := uploadImage(t, e, bob, page.ID, testPNG); code != 404 {
		t.Fatalf("bob upload = %d", code)
	}
	if resp, _ := e.client().Get(e.srv.URL + img.URL); resp.StatusCode != 401 {
		t.Fatalf("anonymous read = %d", resp.StatusCode)
	}
}

// A cover's type comes from its bytes, not the label the client sends.
func TestCoverTypeFromBytes(t *testing.T) {
	e := newTestEnv(t, RegOpen)
	anna := e.client()
	e.do(t, anna, "POST", "/api/auth/register", m{"email": "anna@example.hu", "username": "anna", "password": "hosszujelszo"}, nil)
	var spaces []spaceResponse
	e.do(t, anna, "GET", "/api/spaces", nil, &spaces)
	var page pageResponse
	e.do(t, anna, "POST", "/api/spaces/"+spaces[0].ID+"/pages", m{"title": "P"}, &page)

	upload := func(data []byte) (int, coverResponse) {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="file"; filename="c.png"`)
		h.Set("Content-Type", "image/png")
		fw, _ := mw.CreatePart(h)
		fw.Write(data)
		mw.Close()
		req, _ := http.NewRequest("PUT", e.srv.URL+"/api/pages/"+page.ID+"/cover", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		resp, err := anna.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out coverResponse
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	if code, _ := upload([]byte("<html><script>alert(1)</script>")); code != 400 {
		t.Fatalf("HTML labelled image/png = %d, want 400", code)
	}
	if code, c := upload(testPNG); code != 200 || c.MimeType != "image/png" || c.SizeBytes != int64(len(testPNG)) {
		t.Fatalf("real PNG = %d %+v", code, c)
	}
}
