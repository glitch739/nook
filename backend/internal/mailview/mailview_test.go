package mailview

import (
	"strings"
	"testing"
)

// A typical Outlook mail: encoded Hungarian headers, an HTML body in
// iso-8859-2 quoted-printable, a cid: screenshot and a real attachment.
const outlookMail = "From: =?utf-8?B?U3ppbMOhZ3lpIEfDoWJvcg==?= <gabor@example.hu>\r\n" +
	"To: Anna <anna@example.hu>, =?utf-8?Q?B=C3=A9la?= <bela@example.hu>\r\n" +
	"Subject: =?utf-8?B?UkU6IE51dCBhbGFwYW55YWcgw6FyYXrDs3MgdMOhYmxh?=\r\n" +
	"Date: Mon, 31 Aug 2026 12:43:45 +0000\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=\"MIX\"\r\n" +
	"\r\n" +
	"--MIX\r\n" +
	"Content-Type: multipart/related; boundary=\"REL\"\r\n" +
	"\r\n" +
	"--REL\r\n" +
	"Content-Type: multipart/alternative; boundary=\"ALT\"\r\n" +
	"\r\n" +
	"--ALT\r\n" +
	"Content-Type: text/plain; charset=\"iso-8859-2\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"Sziasztok! =C1rv=EDzt=FBr=F5\r\n" +
	"--ALT\r\n" +
	"Content-Type: text/html; charset=\"iso-8859-2\"\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"<html><head><style>td > p {color:red}</style></head><body><p>Sziasztok! =C1rv=EDzt=FBr=F5</p>" +
	"<img src=3D\"cid:image001.png@01DC\"></body></html>\r\n" +
	"--ALT--\r\n" +
	"--REL\r\n" +
	"Content-Type: image/png; name=\"image001.png\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"Content-ID: <image001.png@01DC>\r\n" +
	"\r\n" +
	"iVBORw0KGgo=\r\n" +
	"--REL--\r\n" +
	"--MIX\r\n" +
	"Content-Type: application/pdf; name=\"=?utf-8?Q?=C3=A1raj=C3=A1nlat.pdf?=\"\r\n" +
	"Content-Disposition: attachment; filename=\"=?utf-8?Q?=C3=A1raj=C3=A1nlat.pdf?=\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"JVBERi0xLjQ=\r\n" +
	"--MIX--\r\n"

func TestParse_OutlookMail(t *testing.T) {
	m, err := Parse(strings.NewReader(outlookMail))
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "RE: Nut alapanyag árazós tábla" {
		t.Errorf("Subject = %q", m.Subject)
	}
	if m.From != "Szilágyi Gábor <gabor@example.hu>" {
		t.Errorf("From = %q", m.From)
	}
	if m.To != "Anna <anna@example.hu>, Béla <bela@example.hu>" {
		t.Errorf("To = %q", m.To)
	}
	if m.Date.IsZero() {
		t.Error("Date not parsed")
	}
	body := string(m.Body)
	if !strings.Contains(body, "Sziasztok! Árvíztűrő") {
		t.Errorf("body not decoded from iso-8859-2: %s", body)
	}
	if !strings.Contains(body, `src="data:image/png;base64,iVBORw0KGgo="`) {
		t.Errorf("cid image not inlined: %s", body)
	}
	if !strings.Contains(body, "td > p {color:red}") {
		t.Errorf("CSS mangled: %s", body)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].Name != "árajánlat.pdf" {
		t.Errorf("Attachments = %+v, want árajánlat.pdf only (the cid image is not one)", m.Attachments)
	}
}

func TestParse_PlainTextOnly(t *testing.T) {
	m, err := Parse(strings.NewReader("From: a@example.hu\r\nSubject: Teszt\r\n\r\n<b>nem HTML</b> & sima szöveg\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(m.Body), "&lt;b&gt;nem HTML&lt;/b&gt; &amp; sima szöveg") {
		t.Errorf("plain text not escaped into <pre>: %s", m.Body)
	}
}

func TestParse_NotMail(t *testing.T) {
	if _, err := Parse(strings.NewReader("\xd0\xcf\x11\xe0 binary outlook msg")); err == nil {
		t.Error("binary data parsed as mail")
	}
}

func TestSanitize_RemovesActiveContent(t *testing.T) {
	in := `<html><head><meta http-equiv="refresh" content="0;url=https://evil.example">` +
		`<base href="https://evil.example/"><link rel="stylesheet" href="https://evil.example/x.css">` +
		`<script>alert(1)</script></head><body onload="alert(2)">` +
		`<!--[if mso]><script>alert(3)</script><![endif]-->` +
		`<p onclick="alert(4)" style="color:blue">ok <a href="javascript:alert(5)">js</a> <a href="https://ubm.hu">link</a></p>` +
		`<img src="x" onerror="alert(6)"><img src="https://tracker.example/p.gif">` +
		`<iframe src="https://evil.example">inner</iframe><object data="x.swf">obj</object>` +
		`<form action="https://evil.example/steal"><input name="pw" value="kept"></form>` +
		`<svg><script>alert(7)</script></svg><div style="width:expression(alert(8))">css</div>` +
		`<o:p>office</o:p><p>vége</p></body></html>`
	out := string(Sanitize(in, nil))

	for _, bad := range []string{"alert", "evil.example", "javascript:", "refresh", "<iframe", "<object", "<form",
		"<script", "<svg", "onload", "onclick", "onerror", "expression(", "<o:p", "inner", "obj"} {
		if strings.Contains(out, bad) {
			t.Errorf("output still contains %q:\n%s", bad, out)
		}
	}
	for _, good := range []string{`<p style="color:blue">ok`, `<a href="https://ubm.hu">link</a>`, `value="kept"`,
		"vége", `<base target="_blank">`, "https://tracker.example/p.gif"} {
		if !strings.Contains(out, good) {
			t.Errorf("output lost %q:\n%s", good, out)
		}
	}
}
