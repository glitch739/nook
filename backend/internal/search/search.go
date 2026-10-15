// Package search answers the sidebar's full-text page search.
//
// The index itself (search_index, an FTS5 table) is maintained entirely
// by SQLite triggers -- see migrations/0014_search.sql and ADR 0007.
// This package only turns a user's typed text into a safe FTS5 query
// and runs it, restricted to pages the user can actually open.
package search

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Highlight markers wrapped around matched terms in Result.TitleMarked
// and Result.Snippet. Not HTML: two ASCII control characters that never
// appear in typed text, so the frontend can split on them and render matches as React
// elements without ever injecting markup.
const (
	MarkStart = "\x02"
	MarkEnd   = "\x03"
)

// MaxQueryTerms caps how many words one query may contain; anything past
// it is ignored rather than rejected (a pasted paragraph still searches
// for its first few words).
const MaxQueryTerms = 8

// DefaultLimit is how many results Search returns.
const DefaultLimit = 30

type Result struct {
	PageID       string
	SpaceID      string
	ParentPageID *string
	Kind         string
	Icon         string
	Title        string
	// TitleMarked is Title with MarkStart/MarkEnd around matched terms.
	TitleMarked string
	// Snippet is a short excerpt of the body around the best match, with
	// the same markers; empty when only the title matched.
	Snippet string
	// Files are the page's attachments that matched (best first, at most
	// MaxFilesPerPage). A page can be a result through its files alone.
	Files []FileHit

	// score: bm25 of the best match on the page or its files; lower is better.
	score float64
}

// FileHit is one attachment whose name or extracted text matched.
type FileHit struct {
	FileID   string
	Filename string
	MimeType string
	// NameMarked is Filename with MarkStart/MarkEnd around matched terms.
	NameMarked string
	// PageNo is the PDF page of the best match, 0 when the format has no
	// pages or only the name matched.
	PageNo int
	// Part is the embedded document the match is in ("/notes.docx"
	// inside a zip), "" for the file itself.
	Part    string
	Snippet string
	// UsedOCR: the matched text was read off a scan or photo, so it may
	// contain misread characters.
	UsedOCR bool

	score float64
}

// MaxFilesPerPage caps the file hits listed under one page result.
const MaxFilesPerPage = 3

type Store struct {
	db *sql.DB
}

func NewStore(conn *sql.DB) *Store {
	return &Store{db: conn}
}

// BuildMatchQuery turns free text into an FTS5 MATCH expression: every
// run of letters/digits becomes a quoted prefix term ("word"*), and the
// terms are ANDed together (FTS5's implicit operator). Everything else
// -- quotes, operators, column filters -- is treated as a separator, so
// user input can never be interpreted as FTS5 syntax. Returns "" when
// the text contains no searchable term.
func BuildMatchQuery(text string) string {
	terms := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(terms) > MaxQueryTerms {
		terms = terms[:MaxQueryTerms]
	}
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		parts = append(parts, `"`+t+`"*`)
	}
	return strings.Join(parts, " ")
}

// BuildSubstringQuery is BuildMatchQuery for the trigram indexes
// (migration 0023): every term of 3+ characters becomes a quoted
// substring ("db01" matches inside "srvdb01"). Trigrams can't match
// anything shorter, so 1-2 character terms come back separately, as a
// word-prefix query the caller checks against the word index -- every
// term still has to match, as with BuildMatchQuery. substring is "" when
// no term is long enough; then there is nothing a substring search
// could add.
func BuildSubstringQuery(text string) (substring, short string) {
	terms := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(terms) > MaxQueryTerms {
		terms = terms[:MaxQueryTerms]
	}
	var long, shortParts []string
	for _, t := range terms {
		if utf8.RuneCountInString(t) >= 3 {
			long = append(long, `"`+t+`"`)
		} else {
			shortParts = append(shortParts, `"`+t+`"*`)
		}
	}
	return strings.Join(long, " "), strings.Join(shortParts, " ")
}

// index names one kind of FTS5 index for pages and for attachments.
// Table names, never user input.
type index struct {
	pages, files string
	// snippetTokens: excerpt length in the index's tokens -- words for
	// the word index, characters (trigrams) for the trigram one.
	snippetTokens int
}

var (
	wordIndex    = index{pages: "search_index", files: "file_search_index", snippetTokens: 12}
	trigramIndex = index{pages: "search_trigram", files: "file_search_trigram", snippetTokens: 64}
)

// substringPenalty ranks every hit found only inside a word below every
// whole-word / prefix hit (bm25 scores are small negative numbers).
const substringPenalty = 1e6

// accessFilter is the one definition of "pages userID may see" for
// both queries below, mirroring permissions.Checker.CanAccessSpace: a
// live page in a live space, reached through a direct space membership
// or a membership of the space's non-archived group. No admin bypass:
// an instance admin who is not a member of a restricted group does not
// see it here either. Expects aliases p (pages) and sp (spaces) and two
// userID arguments.
const accessFilter = `
		  p.archived_at IS NULL
		  AND sp.archived_at IS NULL
		  AND (
		      EXISTS (SELECT 1 FROM space_members AS sm
		              WHERE sm.space_id = sp.id AND sm.user_id = ?)
		   OR EXISTS (SELECT 1 FROM group_members AS gm
		              JOIN groups AS g ON g.id = gm.group_id
		              WHERE gm.group_id = sp.group_id AND gm.user_id = ? AND g.archived_at IS NULL)
		  )`

// Search returns the live pages userID can access whose title, body or
// attached files match text, best match first (a title or file-name hit
// weighs ten times a body hit). Both the page and the file query apply
// accessFilter in SQL, so nothing the user cannot open is ever read,
// counted or excerpted.
func (s *Store) Search(ctx context.Context, userID, text string, limit int) ([]Result, error) {
	return s.SearchIn(ctx, userID, text, "", limit)
}

// SearchIn is Search limited to the direct children of parentID (a
// collection's own pages) when parentID is not empty -- same ranking,
// same access filter, page and attachment hits alike.
func (s *Store) SearchIn(ctx context.Context, userID, text, parentID string, limit int) ([]Result, error) {
	match := BuildMatchQuery(text)
	if match == "" {
		return []Result{}, nil
	}
	if limit <= 0 {
		limit = DefaultLimit
	}

	results, err := s.searchPages(ctx, userID, wordIndex, match, "", parentID, limit)
	if err != nil {
		return nil, err
	}
	files, err := s.searchFiles(ctx, userID, wordIndex, match, "", parentID, limit*MaxFilesPerPage)
	if err != nil {
		return nil, err
	}

	// Then inside words ("db01" in "srvdb01"), for whatever the word
	// index did not already find; those rank after every word hit.
	if sub, short := BuildSubstringQuery(text); sub != "" {
		subPages, err := s.searchPages(ctx, userID, trigramIndex, sub, short, parentID, limit)
		if err != nil {
			return nil, err
		}
		seen := make(map[string]bool, len(results))
		for _, r := range results {
			seen[r.PageID] = true
		}
		for _, r := range subPages {
			if !seen[r.PageID] {
				r.score += substringPenalty
				results = append(results, r)
			}
		}

		subFiles, err := s.searchFiles(ctx, userID, trigramIndex, sub, short, parentID, limit*MaxFilesPerPage)
		if err != nil {
			return nil, err
		}
		seenFile := make(map[string]bool, len(files))
		for _, f := range files {
			seenFile[f.hit.FileID] = true
		}
		for _, f := range subFiles {
			if !seenFile[f.hit.FileID] {
				f.hit.score += substringPenalty
				f.page.score += substringPenalty
				files = append(files, f)
			}
		}
	}

	// Merge: file hits go under their page; a page found only through
	// its files becomes a result of its own. A page ranks by its best
	// match, whether that was in its own text or in a file.
	byPage := make(map[string]int, len(results))
	for i, r := range results {
		byPage[r.PageID] = i
	}
	for _, f := range files {
		i, ok := byPage[f.page.PageID]
		if !ok {
			results = append(results, f.page)
			i = len(results) - 1
			byPage[f.page.PageID] = i
		}
		r := &results[i]
		if len(r.Files) < MaxFilesPerPage {
			r.Files = append(r.Files, f.hit)
		}
		if f.hit.score < r.score {
			r.score = f.hit.score
		}
	}
	sort.SliceStable(results, func(a, b int) bool { return results[a].score < results[b].score })
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// searchPages runs match against idx's page index. short, when not
// empty, is a word-prefix query the page must also match in the word
// index (BuildSubstringQuery's 1-2 character terms).
func (s *Store) searchPages(ctx context.Context, userID string, idx index, match, short, parentID string, limit int) ([]Result, error) {
	args := []any{match}
	shortFilter := ""
	if short != "" {
		shortFilter = `AND d.doc_id IN (SELECT rowid FROM search_index WHERE search_index MATCH ?)`
		args = append(args, short)
	}
	if parentID != "" {
		shortFilter += ` AND p.parent_page_id = ?`
		args = append(args, parentID)
	}
	args = append(args, userID, userID, limit)
	t := idx.pages
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT p.id, p.space_id, p.parent_page_id, p.kind, p.icon, p.title,
		       highlight(%[1]s, 0, char(2), char(3)),
		       snippet(%[1]s, 1, char(2), char(3), '…', %[2]d),
		       bm25(%[1]s, 10.0, 1.0)
		FROM %[1]s
		JOIN search_docs AS d ON d.doc_id = %[1]s.rowid
		JOIN pages AS p ON p.id = d.page_id
		JOIN spaces AS sp ON sp.id = p.space_id
		WHERE %[1]s MATCH ?
		  %[3]s
		  AND `+accessFilter+`
		ORDER BY bm25(%[1]s, 10.0, 1.0)
		LIMIT ?;
	`, t, idx.snippetTokens, shortFilter), args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	defer rows.Close()

	results := make([]Result, 0)
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.PageID, &r.SpaceID, &r.ParentPageID, &r.Kind, &r.Icon,
			&r.Title, &r.TitleMarked, &r.Snippet, &r.score); err != nil {
			return nil, fmt.Errorf("search: scan: %w", err)
		}
		// snippet() still returns the body's opening words when the
		// match was only in the title -- that excerpt shows nothing
		// relevant, so drop it.
		if !strings.Contains(r.Snippet, MarkStart) {
			r.Snippet = ""
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: rows: %w", err)
	}
	return results, nil
}

type fileMatch struct {
	page Result // the file's page, as a result of its own (Title unmarked)
	hit  FileHit
}

// searchFiles finds attachments by name or extracted text (ADR 0009),
// best chunk per file. Besides accessFilter, a file only counts while
// its page still shows it: a block removed from the page without its
// own Delete leaves the file row behind, and that file must not keep
// turning up in search. (Restoring an older version that has the block
// brings it back.)
func (s *Store) searchFiles(ctx context.Context, userID string, idx index, match, short, parentID string, limit int) ([]fileMatch, error) {
	args := []any{match}
	shortFilter := ""
	if short != "" {
		shortFilter = `AND t.chunk_id IN (SELECT rowid FROM file_search_index WHERE file_search_index MATCH ?)`
		args = append(args, short)
	}
	if parentID != "" {
		shortFilter += ` AND p.parent_page_id = ?`
		args = append(args, parentID)
	}
	args = append(args, userID, userID, limit)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT page_id, space_id, parent_page_id, kind, icon, title,
		       file_id, filename, mime_type, name_marked, page_no, part, snippet, used_ocr, score
		FROM (
			SELECT *, row_number() OVER (PARTITION BY file_id ORDER BY score) AS rn
			FROM (
				SELECT p.id AS page_id, p.space_id, p.parent_page_id, p.kind, p.icon, p.title,
				       f.id AS file_id, f.filename, f.mime_type,
				       highlight(%[1]s, 0, char(2), char(3)) AS name_marked,
				       t.page_no, t.part,
				       snippet(%[1]s, 1, char(2), char(3), '…', %[2]d) AS snippet,
				       t.ocr AS used_ocr,
				       bm25(%[1]s, 10.0, 1.0) AS score
				FROM %[1]s
				JOIN file_text AS t ON t.chunk_id = %[1]s.rowid
				JOIN page_attachments AS f ON f.id = t.file_id
				JOIN pages AS p ON p.id = f.page_id
				JOIN spaces AS sp ON sp.id = p.space_id
				WHERE %[1]s MATCH ?
				  %[3]s
				  AND instr(p.content_json, f.id) > 0
				  AND `+accessFilter+`
			)
		)
		WHERE rn = 1
		ORDER BY score
		LIMIT ?;
	`, idx.files, idx.snippetTokens, shortFilter), args...)
	if err != nil {
		return nil, fmt.Errorf("search: files query: %w", err)
	}
	defer rows.Close()

	out := make([]fileMatch, 0)
	for rows.Next() {
		var m fileMatch
		var pageNo sql.NullInt64
		if err := rows.Scan(&m.page.PageID, &m.page.SpaceID, &m.page.ParentPageID, &m.page.Kind, &m.page.Icon,
			&m.page.Title, &m.hit.FileID, &m.hit.Filename, &m.hit.MimeType, &m.hit.NameMarked,
			&pageNo, &m.hit.Part, &m.hit.Snippet, &m.hit.UsedOCR, &m.hit.score); err != nil {
			return nil, fmt.Errorf("search: files scan: %w", err)
		}
		m.page.TitleMarked = m.page.Title
		m.page.score = m.hit.score
		// Same rule as for pages: an excerpt without a match in it (the
		// name matched, not the text) shows nothing useful. Nor does a
		// page number then.
		if strings.Contains(m.hit.Snippet, MarkStart) {
			m.hit.PageNo = int(pageNo.Int64)
		} else {
			m.hit.Snippet = ""
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: files rows: %w", err)
	}
	return out, nil
}
