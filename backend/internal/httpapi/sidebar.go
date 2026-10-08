package httpapi

import (
	"net/http"

	"nook/internal/auth"
	"nook/internal/groups"
	"nook/internal/httpx"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/spaces"
)

// sidebarAPI serves the whole sidebar in one response. It replaces the
// frontend's 2 + one-per-space requests (groups, spaces, then each
// space's page tree), which ran again on every live "tree" event;
// over a proxy each request costs a full round trip, most noticeably
// on phones. The database work is three queries plus one cheap access
// check per space, whatever the number of spaces.
type sidebarAPI struct {
	groups *groups.Store
	spaces *spaces.Store
	pages  *pages.Store
	perm   *permissions.Checker
}

type sidebarSpaceResponse struct {
	spaceResponse
	Pages []*treeNodeResponse `json:"pages"`
}

type sidebarResponse struct {
	Groups []groupResponse        `json:"groups"`
	Spaces []sidebarSpaceResponse `json:"spaces"`
}

// GET /api/sidebar (signed in): the caller's groups (their order), their
// spaces (their order) and each space's page tree. A space is included
// only if the caller passes the same viewer check as GET
// /api/spaces/{id}/pages, so this never shows more than those did.
func (a sidebarAPI) sidebarHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := auth.UserIDFromContext(ctx)

	gs, err := a.groups.ListForUser(ctx, userID)
	if err != nil {
		internalError(w, err)
		return
	}
	ss, err := a.spaces.ListForUser(ctx, userID)
	if err != nil {
		internalError(w, err)
		return
	}

	resp := sidebarResponse{
		Groups: make([]groupResponse, len(gs)),
		Spaces: make([]sidebarSpaceResponse, 0, len(ss)),
	}
	for i, ug := range gs {
		resp.Groups[i] = toGroupResponse(ug.Group)
		resp.Groups[i].Position = ug.Position
	}

	ids := make([]string, 0, len(ss))
	index := make(map[string]int, len(ss))
	for _, us := range ss {
		ok, err := a.perm.CanAccessSpace(ctx, userID, us.ID, spaces.RoleViewer)
		if err != nil {
			internalError(w, err)
			return
		}
		if !ok {
			continue
		}
		sr := toSpaceResponse(us.Space)
		sr.Position = us.Position
		index[us.ID] = len(resp.Spaces)
		resp.Spaces = append(resp.Spaces, sidebarSpaceResponse{spaceResponse: sr, Pages: []*treeNodeResponse{}})
		ids = append(ids, us.ID)
	}

	all, err := a.pages.ListTreeForSpaces(ctx, ids)
	if err != nil {
		internalError(w, err)
		return
	}
	bySpace := make(map[string][]pages.Page, len(ids))
	for _, p := range all {
		bySpace[p.SpaceID] = append(bySpace[p.SpaceID], p)
	}
	for spaceID, list := range bySpace {
		tree := pages.BuildTree(list)
		nodes := make([]*treeNodeResponse, len(tree))
		for i, n := range tree {
			nodes[i] = toTreeResponse(n)
		}
		resp.Spaces[index[spaceID]].Pages = nodes
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}
