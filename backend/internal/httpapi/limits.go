package httpapi

import (
	"net/http"
	"time"

	"nook/internal/assets"
)

// maxBody caps a request body at n bytes, returning a handler that
// responds with an error (via the wrapped handler's own body-too-large
// handling — http.MaxBytesReader makes the *next* Body.Read call return
// an error, it doesn't respond on its own) if the client sends more.
// Sized per-route rather than one global limit, since a login body and a
// future page-content body have very different legitimate sizes.
func maxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

// Body size limits, one per route that accepts a body. Generous for what
// each endpoint legitimately needs, nowhere near generous enough for
// someone to use it as a memory-exhaustion vector.
const (
	loginBodyLimit  = 4 << 10  // {username, password} JSON
	logoutBodyLimit = 1 << 10  // no body is ever read, but cap it anyway
	spaceBodyLimit  = 16 << 10 // {name, type, group_id} JSON
	groupBodyLimit  = 16 << 10 // {name}/{icon}/{role}/{group_ids} JSON
	// A member invite carries a username/password/display_name on top
	// of the plain group bodies above -- still nowhere near large
	// enough to matter as a memory-exhaustion vector, just its own
	// named constant so the shape it's sized for is clear at the call
	// site.
	inviteBodyLimit = 16 << 10

	pageCreateBodyLimit = 16 << 10 // {parent_page_id, title} -- no content yet
	// A page's content_json is a full Tiptap/ProseMirror document, which
	// can legitimately get large (long pages, embedded structure) --
	// generous on purpose, still nowhere near "someone can exhaust
	// memory with it."
	pageUpdateBodyLimit = 2 << 20
	// {parent_page_id, page_ids: [...]} for a reorder can legitimately
	// list many sibling ids; {space_id, parent_page_id} for a move is
	// tiny -- shared limit sized for the larger of the two.
	pageOrderBodyLimit = 64 << 10

	// {row_ids: [...]}: the board's card order lists every row of the
	// database (~40 bytes per id), so 64 KiB stopped at ~1600 rows; 1 MiB
	// holds ~25,000, beyond what a board view is usable for.
	boardOrderBodyLimit = 1 << 20

	propertyBodyLimit = 16 << 10 // {name, type} or {name?, options?} JSON
	rowValueBodyLimit = 4 << 10  // {value: <one cell>} -- always small

	// assets.MaxCoverSize is the *file's* cap; a bit of headroom on top
	// for the multipart boundary/headers around it.
	coverUploadBodyLimit   = assets.MaxCoverSize + (64 << 10)
	coverPositionBodyLimit = 1 << 10 // {position_y: <number>} -- always tiny
	// Same shape as the cover limit: the image cap plus multipart
	// framing headroom.
	imageUploadBodyLimit = assets.MaxImageSize + (64 << 10)
	// Same for attached files.
	attachmentUploadBodyLimit = assets.MaxAttachmentSize + (64 << 10)
)

// uploadTimeout bounds a whole upload request (cover, image, attachment),
// which runs outside the general RequestTimeout: 25 MB over a slow
// mobile uplink can take several minutes.
const uploadTimeout = 10 * time.Minute
