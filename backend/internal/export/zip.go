package export

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"nook/internal/assets"
	"nook/internal/databases"
	"nook/internal/groups"
	"nook/internal/pages"
	"nook/internal/spaces"
)

// Exporter reads everything one user can see and writes it out.
type Exporter struct {
	Pages     *pages.Store
	Spaces    *spaces.Store
	Groups    *groups.Store
	Databases *databases.Store
	Assets    *assets.Store
}

var assetURL = regexp.MustCompile(`^/api/pages/([^/]+)/(images|attachments)/([^/]+)$`)

// ---- JSON dump shapes (full fidelity: raw content and cell values) ----

type jsonExport struct {
	Format     string      `json:"format"`
	ExportedAt string      `json:"exported_at"`
	Spaces     []jsonSpace `json:"spaces"`
}

type jsonSpace struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Icon  string     `json:"icon"`
	Group string     `json:"group"`
	Pages []jsonPage `json:"pages"`
}

type jsonPage struct {
	ID         string                     `json:"id"`
	ParentID   *string                    `json:"parent_id"`
	Title      string                     `json:"title"`
	Icon       string                     `json:"icon"`
	Kind       string                     `json:"kind"`
	CreatedAt  string                     `json:"created_at"`
	UpdatedAt  string                     `json:"updated_at"`
	Content    json.RawMessage            `json:"content"`
	Properties []databases.Property       `json:"properties,omitempty"`
	Values     map[string]json.RawMessage `json:"values,omitempty"`
}

// Zip writes one ZIP: a folder per group and space, one Markdown file
// per page (sub-pages in a folder named after their parent), images and
// attached files under each folder's _files/, and nook-export.json with
// everything in full fidelity.
func (e Exporter) Zip(ctx context.Context, w io.Writer, userID string, now time.Time) error {
	zw := zip.NewWriter(w)
	root := "nook-export-" + now.Format("2006-01-02")
	names := newNamer()
	groupName := map[string]string{}
	dump := jsonExport{Format: "nook-export/1", ExportedAt: now.UTC().Format(time.RFC3339), Spaces: []jsonSpace{}}

	userSpaces, err := e.Spaces.ListForUser(ctx, userID)
	if err != nil {
		return err
	}
	type spaceData struct {
		space spaces.Space
		dir   string
		pages []pages.Page
	}
	var all []spaceData
	visible := map[string]bool{}
	for _, us := range userSpaces {
		gname, ok := groupName[us.GroupID]
		if !ok && us.GroupID != "" {
			if g, err := e.Groups.GetByID(ctx, us.GroupID); err == nil {
				gname = g.Name
			}
			groupName[us.GroupID] = gname
		}
		ps, err := e.Pages.ListForSpace(ctx, us.ID)
		if err != nil {
			return err
		}
		for _, p := range ps {
			visible[p.ID] = true
		}
		dir := names.child(names.child(root, orDefault(gname, "No group")), orDefault(us.Name, "Space"))
		all = append(all, spaceData{space: us.Space, dir: dir, pages: ps})
	}

	written := map[string]bool{}
	for _, sd := range all {
		js := jsonSpace{ID: sd.space.ID, Name: sd.space.Name, Icon: sd.space.Icon, Group: groupName[sd.space.GroupID], Pages: []jsonPage{}}
		children := map[string][]pages.Page{}
		var tops []pages.Page
		for _, p := range sd.pages {
			if p.ParentPageID == nil || !visible[*p.ParentPageID] {
				tops = append(tops, p)
			} else {
				children[*p.ParentPageID] = append(children[*p.ParentPageID], p)
			}
		}
		var walk func(p pages.Page, dir string) error
		walk = func(p pages.Page, dir string) error {
			name := names.child(dir, orDefault(p.Title, "Untitled"))
			links := func(raw string) string {
				m := assetURL.FindStringSubmatch(raw)
				if m == nil || !visible[m[1]] {
					return ""
				}
				rel, err := e.addAsset(ctx, zw, written, dir, m[1], m[2], m[3])
				if err != nil {
					return ""
				}
				return rel
			}
			md, err := e.pageMarkdown(ctx, p, children[p.ID], links)
			if err != nil {
				return err
			}
			f, err := zw.Create(name + ".md")
			if err != nil {
				return err
			}
			if _, err := io.WriteString(f, md); err != nil {
				return err
			}
			jp := jsonPage{ID: p.ID, ParentID: p.ParentPageID, Title: p.Title, Icon: p.Icon, Kind: p.Kind,
				CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Content: json.RawMessage(orDefault(p.ContentJSON, "null"))}
			if p.Kind == pages.KindDatabase {
				jp.Properties, _ = e.Databases.ListProperties(ctx, p.ID)
				if vals, err := e.Databases.ValuesForDatabase(ctx, p.ID); err == nil {
					jp.Values = map[string]json.RawMessage{}
					for rowID, cells := range vals {
						b, _ := json.Marshal(rawCells(cells))
						jp.Values[rowID] = b
					}
				}
			}
			js.Pages = append(js.Pages, jp)
			for _, c := range children[p.ID] {
				if err := walk(c, name); err != nil {
					return err
				}
			}
			return nil
		}
		for _, p := range tops {
			if err := walk(p, sd.dir); err != nil {
				return err
			}
		}
		dump.Spaces = append(dump.Spaces, js)
	}

	f, err := zw.Create(root + "/nook-export.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(dump); err != nil {
		return err
	}
	return zw.Close()
}

func rawCells(cells map[string]string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(cells))
	for k, v := range cells {
		out[k] = json.RawMessage(v)
	}
	return out
}

// addAsset copies one image or attached file into dir/_files/ (once)
// and returns the Markdown-relative link to it.
func (e Exporter) addAsset(ctx context.Context, zw *zip.Writer, written map[string]bool, dir, pageID, kind, id string) (string, error) {
	var (
		rc       io.ReadCloser
		filename string
	)
	if kind == "images" {
		f, img, err := e.Assets.OpenImage(ctx, pageID, id)
		if err != nil {
			return "", err
		}
		rc, filename = f, img.Filename
	} else {
		f, att, err := e.Assets.OpenAttachment(ctx, pageID, id)
		if err != nil {
			return "", err
		}
		rc, filename = f, att.Filename
	}
	defer rc.Close()
	file := id[:8] + "-" + safeName(orDefault(filename, "file"))
	full := dir + "/_files/" + file
	if !written[full] {
		w, err := zw.Create(full)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(w, rc); err != nil {
			return "", err
		}
		written[full] = true
	}
	return "_files/" + url.PathEscape(file), nil
}

// pageMarkdown is one page as Markdown: a title heading, a table of rows
// for a database page, then the page's own text.
func (e Exporter) pageMarkdown(ctx context.Context, p pages.Page, rows []pages.Page, links Links) (string, error) {
	var b strings.Builder
	b.WriteString("# " + escape(orDefault(p.Title, "Untitled")) + "\n\n")
	if p.Kind == pages.KindDatabase {
		table, err := e.databaseTable(ctx, p, rows)
		if err != nil {
			return "", err
		}
		b.WriteString(table + "\n")
	}
	if p.ContentJSON != "" {
		body, err := Markdown(p.ContentJSON, links)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(body) != "" {
			b.WriteString(body)
		}
	}
	return b.String(), nil
}

func (e Exporter) databaseTable(ctx context.Context, db pages.Page, rows []pages.Page) (string, error) {
	props, err := e.Databases.ListProperties(ctx, db.ID)
	if err != nil {
		return "", err
	}
	vals, err := e.Databases.ValuesForDatabase(ctx, db.ID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	head := []string{"Name"}
	for _, p := range props {
		head = append(head, cell(p.Name))
	}
	b.WriteString("| " + strings.Join(head, " | ") + " |\n|" + strings.Repeat(" --- |", len(head)) + "\n")
	for _, r := range rows {
		line := []string{cell(orDefault(r.Title, "Untitled"))}
		for _, p := range props {
			line = append(line, cell(formatValue(p, vals[r.ID][p.ID])))
		}
		b.WriteString("| " + strings.Join(line, " | ") + " |\n")
	}
	return b.String(), nil
}

// formatValue renders one cell for the Markdown table.
func formatValue(p databases.Property, raw string) string {
	if raw == "" || raw == "null" {
		return ""
	}
	label := func(id string) string {
		for _, o := range p.Options {
			if o.ID == id {
				return o.Label
			}
		}
		return ""
	}
	switch p.Type {
	case databases.TypeSelect:
		var v struct {
			OptionID string `json:"option_id"`
		}
		if json.Unmarshal([]byte(raw), &v) == nil {
			return label(v.OptionID)
		}
	case databases.TypePerson:
		var v struct {
			UserID string `json:"user_id"`
		}
		if json.Unmarshal([]byte(raw), &v) == nil && v.UserID != "" {
			return label(v.UserID)
		}
	case "checkbox":
		if raw == "true" {
			return "[x]"
		}
		return "[ ]"
	}
	var s any
	if json.Unmarshal([]byte(raw), &s) == nil {
		switch t := s.(type) {
		case string:
			return t
		case float64:
			return fmt.Sprint(t)
		}
	}
	return raw
}

func cell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// safeName makes a title usable as a file or folder name on any OS.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 32, strings.ContainsRune(`/\:*?"<>|`, r):
			return '-'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.Trim(s, ". ")
	if s == "" {
		s = "Untitled"
	}
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}

// namer hands out unique child paths: two pages called "Notes" in the
// same folder become "Notes" and "Notes (2)".
type namer struct{ used map[string]bool }

func newNamer() namer { return namer{used: map[string]bool{}} }

func (n namer) child(dir, name string) string {
	base := safeName(name)
	candidate := path.Join(dir, base)
	for i := 2; n.used[strings.ToLower(candidate)]; i++ {
		candidate = path.Join(dir, fmt.Sprintf("%s (%d)", base, i))
	}
	n.used[strings.ToLower(candidate)] = true
	return candidate
}

// PageMarkdown is a single page as Markdown (for "Export as Markdown"):
// links stay app URLs, which work while signed in.
func (e Exporter) PageMarkdown(ctx context.Context, p pages.Page) (string, error) {
	var rows []pages.Page
	if p.Kind == pages.KindDatabase {
		all, err := e.Pages.ListForSpace(ctx, p.SpaceID)
		if err != nil {
			return "", err
		}
		for _, r := range all {
			if r.ParentPageID != nil && *r.ParentPageID == p.ID {
				rows = append(rows, r)
			}
		}
	}
	return e.pageMarkdown(ctx, p, rows, nil)
}
