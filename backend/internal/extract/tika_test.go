package extract

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rmetaScannedZip is shaped like tika-server 4.1's /rmeta/xml answer for
// a zip holding a Word file and a two-page scanned PDF (trimmed).
const rmetaScannedZip = `[
 {"Content-Type":"application/zip","tk:embedded-depth":"0",
  "tk:content":"<html xmlns=\"http://www.w3.org/1999/xhtml\"><head><meta name=\"x\" content=\"y\"/><title></title></head><body><div class=\"package-entry\"><h1>note.docx</h1></div></body></html>"},
 {"Content-Type":"application/vnd.openxmlformats-officedocument.wordprocessingml.document","tk:embedded-resource-path":"/note.docx",
  "tk:content":"<html xmlns=\"http://www.w3.org/1999/xhtml\"><head><title>t</title></head><body><p>Hálózati topológia:&nbsp;a core switch</p><p>neve swcore02.</p></body></html>"},
 {"Content-Type":"application/pdf","tk:embedded-resource-path":"/scanned.pdf","pdf:ocr-page-count":"2",
  "tk:content":"<html xmlns=\"http://www.w3.org/1999/xhtml\"><head></head><body><div class=\"page\"><div class=\"ocr\">Üzemeltetési kézikönyv\nSzerver: srvdb01</div>\n</div>\n<div class=\"page\"><div class=\"ocr\">Árvíztűrő   tükörfúrógép</div></div></body></html>"}
]`

func TestParseRmeta_PagesPartsAndOCR(t *testing.T) {
	res, err := parseRmeta([]byte(rmetaScannedZip))
	if err != nil {
		t.Fatalf("parseRmeta: %v", err)
	}
	if !res.UsedOCR {
		t.Error("UsedOCR = false, want true (pdf:ocr-page-count)")
	}
	want := []Chunk{
		{PageNo: 0, Part: "", Text: "note.docx"},
		{PageNo: 0, Part: "/note.docx", Text: "Hálózati topológia: a core switch\nneve swcore02."},
		{PageNo: 1, Part: "/scanned.pdf", OCR: true, Text: "Üzemeltetési kézikönyv\nSzerver: srvdb01"},
		{PageNo: 2, Part: "/scanned.pdf", OCR: true, Text: "Árvíztűrő tükörfúrógép"},
	}
	if len(res.Chunks) != len(want) {
		t.Fatalf("chunks = %+v, want %+v", res.Chunks, want)
	}
	for i := range want {
		if res.Chunks[i] != want[i] {
			t.Errorf("chunk %d = %+v, want %+v", i, res.Chunks[i], want[i])
		}
	}
}

func TestParseRmeta_Tika3KeysAndProblem(t *testing.T) {
	res, err := parseRmeta([]byte(`[{"X-TIKA:content":"<html><body><p>régi kulcs</p></body></html>",
		"X-TIKA:EXCEPTION:embedded_exception":"org.apache.tika.exception.EncryptedDocumentException: bad\n\tat x"}]`))
	if err != nil {
		t.Fatalf("parseRmeta: %v", err)
	}
	if len(res.Chunks) != 1 || res.Chunks[0].Text != "régi kulcs" {
		t.Fatalf("chunks = %+v", res.Chunks)
	}
	if res.Problem != "password protected" {
		t.Errorf("Problem = %q, want password protected", res.Problem)
	}
}

func TestSplitPages_SkipsHeadAndScripts(t *testing.T) {
	got := splitPages(`<html><head><title>Cím</title><style>p{}</style></head><body><script>alert(1)</script><table><tr><td>a1</td><td>b1</td></tr></table></body></html>`)
	if len(got) != 1 || got[0].Text != "a1\nb1" {
		t.Fatalf("splitPages = %+v, want one chunk \"a1\\nb1\"", got)
	}
}

func TestTika_StatusMapping(t *testing.T) {
	cases := []struct {
		status  int
		body    string
		wantErr error
	}{
		{http.StatusUnprocessableEntity, "org.apache.tika.exception.EncryptedDocumentException", ErrEncrypted},
		{http.StatusUnprocessableEntity, "corrupt", ErrUnprocessable},
		{http.StatusUnsupportedMediaType, "", ErrUnprocessable},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut || r.URL.Path != "/rmeta/xml" {
				t.Errorf("request = %s %s", r.Method, r.URL.Path)
			}
			if !strings.Contains(r.Header.Get("Content-Disposition"), "doc.pdf") {
				t.Errorf("Content-Disposition = %q, want the file name", r.Header.Get("Content-Disposition"))
			}
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}))
		_, err := (&Tika{BaseURL: srv.URL}).Extract(context.Background(), strings.NewReader("x"), "doc.pdf")
		srv.Close()
		if !errors.Is(err, c.wantErr) {
			t.Errorf("HTTP %d %q: err = %v, want %v", c.status, c.body, err, c.wantErr)
		}
	}

	// A server error is worth retrying: not ErrUnprocessable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := (&Tika{BaseURL: srv.URL}).Extract(context.Background(), strings.NewReader("x"), "doc.pdf")
	if err == nil || errors.Is(err, ErrUnprocessable) {
		t.Errorf("HTTP 500: err = %v, want a retryable error", err)
	}
}

func TestCapText_CutsAtRuneBoundary(t *testing.T) {
	got := capText([]Chunk{{Text: "ab"}, {Text: "őő"}, {Text: "never"}}, 5)
	if len(got) != 2 || got[1].Text != "ő" {
		t.Fatalf("capText = %+v, want [ab ő]", got)
	}
}

func TestParseRmeta_MailHeaderIsSearchable(t *testing.T) {
	res, err := parseRmeta([]byte(`[{"Content-Type":"message/rfc822","dc:title":"RE: Nut alapanyag árazós tábla",
		"message:from":"Szilágyi Gábor <gabor@example.hu>","message:to":["Anna <anna@example.hu>","Béla <bela@example.hu>"],
		"tk:content":"<html><body><p>Sziasztok!</p></body></html>"},
		{"Content-Type":"image/png","tk:embedded-resource-path":"/image001.png","tk:content":"<html><body></body></html>"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chunks) != 2 {
		t.Fatalf("chunks = %+v, want header + body (the empty image adds nothing)", res.Chunks)
	}
	want := "Subject: RE: Nut alapanyag árazós tábla\nFrom: Szilágyi Gábor <gabor@example.hu>\nTo: Anna <anna@example.hu>, Béla <bela@example.hu>"
	if res.Chunks[0].Text != want {
		t.Errorf("header chunk = %q, want %q", res.Chunks[0].Text, want)
	}
	if res.Chunks[1].Text != "Sziasztok!" {
		t.Errorf("body chunk = %q", res.Chunks[1].Text)
	}
}
