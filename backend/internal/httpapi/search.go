package httpapi

import (
	"net/http"
	"unicode/utf8"

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
}

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
	results, err := a.search.Search(r.Context(), userID, q, search.DefaultLimit)
	if err != nil {
		internalError(w, err)
		return
	}

	out := make([]searchResultResponse, 0, len(results))
	for _, res := range results {
		out = append(out, searchResultResponse{
			PageID: res.PageID, SpaceID: res.SpaceID, ParentPageID: res.ParentPageID,
			Kind: res.Kind, Icon: res.Icon, Title: res.Title,
			TitleMarked: res.TitleMarked, Snippet: res.Snippet,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
