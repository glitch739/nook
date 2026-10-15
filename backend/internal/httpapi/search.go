package httpapi

import (
	"net/http"
	"unicode/utf8"

	"nook/internal/assets"
	"nook/internal/auth"
	"nook/internal/httpx"
	"nook/internal/search"
)

// maxSearchQueryRunes bounds ?q= -- long enough for any real search,
// short enough that a pasted novel can't become an expensive query.
const maxSearchQueryRunes = 200

type searchAPI struct {
	search *search.Store
}

// searchResultResponse is one page hit. title_marked and snippet carry
// the \u0002/\u0003 highlight markers (see search.MarkStart); the
// frontend splits on them instead of rendering any server-built HTML.
type searchResultResponse struct {
	PageID       string  `json:"page_id"`
	SpaceID      string  `json:"space_id"`
	ParentPageID *string `json:"parent_page_id"`
	Kind         string  `json:"kind"`
	Icon         string  `json:"icon"`
	Title        string  `json:"title"`
	TitleMarked  string  `json:"title_marked"`
	Snippet      string  `json:"snippet"`
	// Files: matching attachments of the page (ADR 0008), best first.
	Files []searchFileResponse `json:"files"`
}

// searchFileResponse is one attachment hit under a page. page_no is the
// PDF page of the match (0: none); url is the file's own URL, the same
// the editor's file block uses.
type searchFileResponse struct {
	FileID      string `json:"file_id"`
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	NameMarked  string `json:"name_marked"`
	Previewable bool   `json:"previewable"`
	PageNo      int    `json:"page_no"`
	Part        string `json:"part"`
	Snippet     string `json:"snippet"`
	UsedOCR     bool   `json:"used_ocr"`
}

// collectionSearchLimit: a collection is meant to hold many pages, so
// its own filter returns more than the sidebar's global search does.
const collectionSearchLimit = 200

// searchHandler handles GET /api/search?q=... (RequireAuth-protected):
// full-text search over every live page the caller can open. Access
// filtering happens inside the query itself (search.Store.Search), so a
// page the caller can't see is never even counted toward the limit.
func (a searchAPI) searchHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if utf8.RuneCountInString(q) > maxSearchQueryRunes {
		http.Error(w, "query too long", http.StatusBadRequest)
		return
	}

	userID := auth.UserIDFromContext(r.Context())
	// ?parent= limits the search to one collection's own pages (its
	// list filter); access is still checked per page inside the query.
	limit := search.DefaultLimit
	parent := r.URL.Query().Get("parent")
	if parent != "" {
		limit = collectionSearchLimit
	}
	results, err := a.search.SearchIn(r.Context(), userID, q, parent, limit)
	if err != nil {
		internalError(w, err)
		return
	}

	out := make([]searchResultResponse, 0, len(results))
	for _, res := range results {
		files := make([]searchFileResponse, 0, len(res.Files))
		for _, f := range res.Files {
			files = append(files, searchFileResponse{
				FileID: f.FileID, URL: "/api/pages/" + res.PageID + "/attachments/" + f.FileID,
				Filename: f.Filename, NameMarked: f.NameMarked,
				Previewable: assets.IsPreviewable(f.MimeType),
				PageNo:      f.PageNo, Part: f.Part, Snippet: f.Snippet, UsedOCR: f.UsedOCR,
			})
		}
		out = append(out, searchResultResponse{
			PageID: res.PageID, SpaceID: res.SpaceID, ParentPageID: res.ParentPageID,
			Kind: res.Kind, Icon: res.Icon, Title: res.Title,
			TitleMarked: res.TitleMarked, Snippet: res.Snippet, Files: files,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
