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
	"strings"
	"unicode"
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
}

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

// Search returns the live pages userID can access whose title or body
// matches text, best match first (title hits weigh ten times a body
// hit). Access mirrors permissions.Checker.CanAccessSpace: a direct
// space membership, or membership of the space's non-archived group.
func (s *Store) Search(ctx context.Context, userID, text string, limit int) ([]Result, error) {
	match := BuildMatchQuery(text)
	if match == "" {
		return []Result{}, nil
	}
	if limit <= 0 {
		limit = DefaultLimit
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.space_id, p.parent_page_id, p.kind, p.icon, p.title,
		       highlight(search_index, 0, char(2), char(3)),
		       snippet(search_index, 1, char(2), char(3), '…', 12)
		FROM search_index
		JOIN search_docs AS d ON d.doc_id = search_index.rowid
		JOIN pages AS p ON p.id = d.page_id
		JOIN spaces AS sp ON sp.id = p.space_id
		WHERE search_index MATCH ?
		  AND p.archived_at IS NULL
		  AND sp.archived_at IS NULL
		  AND (
		      EXISTS (SELECT 1 FROM space_members AS sm
		              WHERE sm.space_id = sp.id AND sm.user_id = ?)
		   OR EXISTS (SELECT 1 FROM group_members AS gm
		              JOIN groups AS g ON g.id = gm.group_id
		              WHERE gm.group_id = sp.group_id AND gm.user_id = ? AND g.archived_at IS NULL)
		  )
		ORDER BY bm25(search_index, 10.0, 1.0)
		LIMIT ?;
	`, match, userID, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	defer rows.Close()

	results := make([]Result, 0)
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.PageID, &r.SpaceID, &r.ParentPageID, &r.Kind, &r.Icon,
			&r.Title, &r.TitleMarked, &r.Snippet); err != nil {
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
