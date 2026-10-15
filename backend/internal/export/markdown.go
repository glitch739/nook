// Package export turns Nook content into portable files: Markdown per
// page (plus a Markdown table per database) and a full-fidelity JSON
// dump, bundled with images and attached files in one ZIP.
package export

import (
	"encoding/json"
	"fmt"
	"strings"
)

// node is the subset of the Tiptap/ProseMirror JSON the converter reads.
type node struct {
	Type    string         `json:"type"`
	Attrs   map[string]any `json:"attrs"`
	Content []node         `json:"content"`
	Text    string         `json:"text"`
	Marks   []struct {
		Type  string         `json:"type"`
		Attrs map[string]any `json:"attrs"`
	} `json:"marks"`
}

// Links rewrites a stored URL (an image's src, a file's href) for the
// output; returning "" keeps the original URL.
type Links func(url string) string

// Names gives a linked page's current title ("" when it's unknown or not
// the reader's to see: the link then keeps the name it was made with).
type Names func(pageID string) string

// Markdown converts a page's content_json to Markdown. Unknown node
// types keep their text, so nothing written in Nook is silently lost.
func Markdown(contentJSON string, links Links, names Names) (string, error) {
	var doc node
	if err := json.Unmarshal([]byte(contentJSON), &doc); err != nil {
		return "", fmt.Errorf("export: parse content: %w", err)
	}
	var b strings.Builder
	w := writer{b: &b, links: links, names: names}
	w.blocks(doc.Content, "")
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

type writer struct {
	b     *strings.Builder
	links Links
	names Names
}

func (w writer) link(url string) string {
	if w.links != nil {
		if out := w.links(url); out != "" {
			return out
		}
	}
	return url
}

func attr(n node, key string) string {
	if v, ok := n.Attrs[key]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}

func (w writer) blocks(ns []node, indent string) {
	for i, n := range ns {
		w.block(n, indent)
		if i < len(ns)-1 && indent == "" {
			w.b.WriteString("\n")
		}
	}
}

func (w writer) block(n node, indent string) {
	b := w.b
	switch n.Type {
	case "paragraph":
		b.WriteString(indent + w.inline(n.Content) + "\n")
	case "heading":
		level := 1
		fmt.Sscan(attr(n, "level"), &level)
		if level < 1 || level > 6 {
			level = 2
		}
		b.WriteString(indent + strings.Repeat("#", level) + " " + w.inline(n.Content) + "\n")
	case "bulletList", "orderedList", "taskList":
		num := 1
		fmt.Sscan(attr(n, "start"), &num)
		for _, item := range n.Content {
			marker := "- "
			switch {
			case n.Type == "orderedList":
				marker = fmt.Sprintf("%d. ", num)
				num++
			case n.Type == "taskList":
				if attr(item, "checked") == "true" {
					marker = "- [x] "
				} else {
					marker = "- [ ] "
				}
			}
			w.listItem(item, indent, marker)
		}
	case "blockquote":
		var inner strings.Builder
		writer{b: &inner, links: w.links, names: w.names}.blocks(n.Content, "")
		for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
			b.WriteString(indent + "> " + line + "\n")
		}
	case "codeBlock":
		var code strings.Builder
		for _, c := range n.Content {
			code.WriteString(c.Text)
		}
		b.WriteString(indent + "```" + attr(n, "language") + "\n")
		for _, line := range strings.Split(code.String(), "\n") {
			b.WriteString(indent + line + "\n")
		}
		b.WriteString(indent + "```\n")
	case "horizontalRule":
		b.WriteString(indent + "---\n")
	case "image":
		b.WriteString(indent + "![" + escape(attr(n, "alt")) + "](" + w.link(attr(n, "src")) + ")\n")
	case "fileAttachment":
		name := attr(n, "filename")
		b.WriteString(indent + "📎 [" + escape(name) + "](" + w.link(attr(n, "href")) + ")\n")
	case "table":
		w.table(n, indent)
	default:
		if len(n.Content) > 0 {
			w.blocks(n.Content, indent)
		} else if n.Text != "" {
			b.WriteString(indent + escape(n.Text) + "\n")
		}
	}
}

// listItem writes the item's first paragraph after the marker and the
// rest (nested lists, more paragraphs) indented under it.
func (w writer) listItem(item node, indent, marker string) {
	child := indent + strings.Repeat(" ", len([]rune(marker)))
	first := true
	for _, c := range item.Content {
		if first && c.Type == "paragraph" {
			w.b.WriteString(indent + marker + w.inline(c.Content) + "\n")
			first = false
			continue
		}
		if first {
			w.b.WriteString(indent + strings.TrimRight(marker, " ") + "\n")
			first = false
		}
		w.block(c, child)
	}
	if first {
		w.b.WriteString(indent + strings.TrimRight(marker, " ") + "\n")
	}
}

func (w writer) table(n node, indent string) {
	for r, row := range n.Content {
		cells := make([]string, 0, len(row.Content))
		for _, cell := range row.Content {
			var parts []string
			for _, p := range cell.Content {
				parts = append(parts, w.inline(p.Content))
			}
			cells = append(cells, strings.ReplaceAll(strings.Join(parts, "<br>"), "|", "\\|"))
		}
		w.b.WriteString(indent + "| " + strings.Join(cells, " | ") + " |\n")
		if r == 0 {
			w.b.WriteString(indent + "|" + strings.Repeat(" --- |", len(cells)) + "\n")
		}
	}
}

func (w writer) inline(ns []node) string {
	var b strings.Builder
	for _, n := range ns {
		switch n.Type {
		case "text":
			t := escape(n.Text)
			code := false
			for _, m := range n.Marks {
				if m.Type == "code" {
					code = true
				}
			}
			if code {
				t = "`" + n.Text + "`"
			}
			for _, m := range n.Marks {
				switch m.Type {
				case "bold":
					t = "**" + t + "**"
				case "italic":
					t = "_" + t + "_"
				case "strike":
					t = "~~" + t + "~~"
				case "link":
					if href, ok := m.Attrs["href"].(string); ok && href != "" {
						t = "[" + t + "](" + href + ")"
					}
				}
			}
			b.WriteString(t)
		case "pageMention":
			// A link to another page: its current name, else the one it
			// was linked with.
			name := attr(n, "label")
			if w.names != nil {
				if cur := w.names(attr(n, "id")); cur != "" {
					name = cur
				}
			}
			b.WriteString("@" + escape(name))
		case "hardBreak":
			b.WriteString("  \n")
		case "image":
			b.WriteString("![" + escape(attr(n, "alt")) + "](" + w.link(attr(n, "src")) + ")")
		default:
			b.WriteString(w.inline(n.Content))
		}
	}
	return b.String()
}

var escaper = strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`)

func escape(s string) string { return escaper.Replace(s) }
