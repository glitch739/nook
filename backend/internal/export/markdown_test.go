package export

import "testing"

func TestMarkdown(t *testing.T) {
	doc := `{"type":"doc","content":[
	 {"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title"}]},
	 {"type":"paragraph","content":[{"type":"text","text":"Hello "},{"type":"text","text":"bold","marks":[{"type":"bold"}]},{"type":"text","text":" and "},{"type":"text","text":"x*y","marks":[{"type":"code"}]},{"type":"text","text":" 2*3"}]},
	 {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]},
	   {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"nested"}]}]}]}]}]},
	 {"type":"taskList","content":[{"type":"taskItem","attrs":{"checked":true},"content":[{"type":"paragraph","content":[{"type":"text","text":"done"}]}]},
	   {"type":"taskItem","attrs":{"checked":false},"content":[{"type":"paragraph","content":[{"type":"text","text":"todo"}]}]}]},
	 {"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"fmt.Println(1)"}]},
	 {"type":"image","attrs":{"src":"/api/pages/p/images/i","alt":"pic"}},
	 {"type":"fileAttachment","attrs":{"href":"/api/pages/p/attachments/a","filename":"doc.pdf"}},
	 {"type":"table","content":[{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"A"}]}]},{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"B|C"}]}]}]},
	   {"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]},{"type":"tableCell","content":[{"type":"paragraph"}]}]}]}
	]}`
	got, err := Markdown(doc, func(url string) string {
		if url == "/api/pages/p/images/i" {
			return "_files/i-pic.png"
		}
		return ""
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "# Title\n\n" +
		"Hello **bold** and `x*y` 2\\*3\n\n" +
		"- one\n  - nested\n\n" +
		"- [x] done\n- [ ] todo\n\n" +
		"```go\nfmt.Println(1)\n```\n\n" +
		"![pic](_files/i-pic.png)\n\n" +
		"📎 [doc.pdf](/api/pages/p/attachments/a)\n\n" +
		"| A | B\\|C |\n| --- | --- |\n| 1 |  |\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestMarkdownPageMention(t *testing.T) {
	got, err := Markdown(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"see "},{"type":"pageMention","attrs":{"id":"x","label":"Plan_B"}}]}]}`, nil, nil)
	if err != nil || got != "see @Plan\\_B\n" {
		t.Fatalf("got %q, %v", got, err)
	}
	// The current name wins; an unknown page keeps the linked one.
	doc := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"pageMention","attrs":{"id":"x","label":"Old"}},{"type":"text","text":" "},{"type":"pageMention","attrs":{"id":"y","label":"Hidden"}}]}]}`
	names := func(id string) string { return map[string]string{"x": "New"}[id] }
	if got, _ := Markdown(doc, nil, names); got != "@New @Hidden\n" {
		t.Fatalf("got %q", got)
	}
}
