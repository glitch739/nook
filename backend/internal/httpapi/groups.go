package httpapi

import (
	"errors"
	"net/http"

	"nook/internal/auth"
	"nook/internal/db"
	"nook/internal/groups"
	"nook/internal/httpx"
	"nook/internal/permissions"
	"nook/internal/spaces"
	"nook/internal/users"
)

// groupRoleRank mirrors permissions' own unexported roleRank -- needed
// here too, specifically to enforce "you can't invite/promote someone
// to a role higher than your own" (privilege escalation), which is a
// group-membership-management concern this package owns, not something
// permissions.Checker itself needs to know about.
var groupRoleRank = map[string]int{
	groups.RoleViewer: 1,
	groups.RoleEditor: 2,
	groups.RoleAdmin:  3,
	groups.RoleOwner:  4,
}

// groupsAPI bundles every store the group endpoints touch. spaces and
// users are here (not just groups+perm, unlike spacesAPI) because
// inviting a brand-new person needs to create their account (users)
// and their own personal space (spaces) in the same request, matching
// the one existing user-creation path's own behavior (cmd/nook/main.go's
// bootstrapAdmin -- see its comment on why every account gets one).
type groupsAPI struct {
	groups *groups.Store
	spaces *spaces.Store
	users  *users.Store
	perm   *permissions.Checker
}

type groupResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Position string `json:"position,omitempty"`
}

func toGroupResponse(g groups.Group) groupResponse {
	return groupResponse{ID: g.ID, Name: g.Name, Icon: g.Icon}
}

type memberResponse struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

func toMemberResponse(m groups.Member) memberResponse {
	return memberResponse{UserID: m.UserID, Username: m.Username, DisplayName: m.DisplayName, Role: m.Role}
}

func isValidGroupRole(role string) bool {
	_, ok := groupRoleRank[role]
	return ok
}

// authorizeGroup loads groupID and checks the caller holds at least
// minRole there -- same 404-for-both-cases anti-enumeration convention
// as spacesAPI.authorizeSpace/pagesAPI.authorizePage.
func (a groupsAPI) authorizeGroup(w http.ResponseWriter, r *http.Request, groupID, minRole string) (groups.Group, bool) {
	g, err := a.groups.GetByID(r.Context(), groupID)
	if errors.Is(err, db.ErrNotFound) || g.ArchivedAt != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return groups.Group{}, false
	}
	if err != nil {
		internalError(w, err)
		return groups.Group{}, false
	}

	userID := auth.UserIDFromContext(r.Context())
	ok, err := a.perm.CanAccessGroup(r.Context(), userID, groupID, minRole)
	if err != nil {
		internalError(w, err)
		return groups.Group{}, false
	}
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return groups.Group{}, false
	}
	return g, true
}

// createGroupHandler handles POST /api/groups (RequireAuth-protected).
// Body: {"name": "..."}. The caller becomes the group's owner.
func (a groupsAPI) createGroupHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	userID := auth.UserIDFromContext(r.Context())
	g, err := a.groups.Create(r.Context(), req.Name, userID)
	if err != nil {
		internalError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toGroupResponse(g))
}

// listGroupsHandler handles GET /api/groups (RequireAuth-protected):
// every group the caller is a member of, in their own chosen order.
func (a groupsAPI) listGroupsHandler(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	list, err := a.groups.ListForUser(r.Context(), userID)
	if err != nil {
		internalError(w, err)
		return
	}

	resp := make([]groupResponse, len(list))
	for i, ug := range list {
		resp[i] = toGroupResponse(ug.Group)
		resp[i].Position = ug.Position
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

// updateGroupHandler handles PUT /api/groups/{groupID}
// (RequireAuth-protected). Body: {"name": "...", "icon": "..."} -- any
// subset. Requires admin+, same bar as renaming a space does.
func (a groupsAPI) updateGroupHandler(w http.ResponseWriter, r *http.Request) {
	g, ok := a.authorizeGroup(w, r, r.PathValue("groupID"), groups.RoleAdmin)
	if !ok {
		return
	}

	var req struct {
		Name *string `json:"name"`
		Icon *string `json:"icon"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Name == nil && req.Icon == nil {
		http.Error(w, "nothing to update: provide name and/or icon", http.StatusBadRequest)
		return
	}
	if req.Name != nil && *req.Name == "" {
		http.Error(w, "name cannot be empty", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		updated, err := a.groups.UpdateName(r.Context(), g.ID, *req.Name)
		if err != nil {
			internalError(w, err)
			return
		}
		g = updated
	}
	if req.Icon != nil {
		updated, err := a.groups.UpdateIcon(r.Context(), g.ID, *req.Icon)
		if err != nil {
			internalError(w, err)
			return
		}
		g = updated
	}

	httpx.WriteJSON(w, http.StatusOK, toGroupResponse(g))
}

// archiveGroupHandler handles DELETE /api/groups/{groupID}
// (RequireAuth-protected). Requires owner, same bar as archiving a
// space -- this removes everyone else's access to the whole group, not
// just the caller's own view of it. Refuses with 409 (not the usual
// 500) when the group still has spaces in it (groups.ErrGroupHasSpaces,
// see Store.Archive's own comment on why) -- a real, actionable
// "empty it out first" response, not a generic failure.
func (a groupsAPI) archiveGroupHandler(w http.ResponseWriter, r *http.Request) {
	g, ok := a.authorizeGroup(w, r, r.PathValue("groupID"), groups.RoleOwner)
	if !ok {
		return
	}
	err := a.groups.Archive(r.Context(), g.ID)
	if errors.Is(err, groups.ErrGroupHasSpaces) {
		http.Error(w, "cannot archive a group that still has spaces in it", http.StatusConflict)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reorderGroupsHandler handles PUT /api/groups/order
// (RequireAuth-protected). Body: {"group_ids": [...]} -- must be
// exactly a permutation of the caller's own current groups, same shape
// as reorderSpacesHandler.
func (a groupsAPI) reorderGroupsHandler(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())

	var req struct {
		GroupIDs []string `json:"group_ids"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}

	current, err := a.groups.ListForUser(r.Context(), userID)
	if err != nil {
		internalError(w, err)
		return
	}
	if !isPermutationOfGroups(req.GroupIDs, current) {
		http.Error(w, "group_ids must be exactly this user's current groups", http.StatusBadRequest)
		return
	}

	if err := a.groups.ReorderForUser(r.Context(), userID, req.GroupIDs); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isPermutationOfGroups(ids []string, current []groups.UserGroup) bool {
	if len(ids) != len(current) {
		return false
	}
	want := make(map[string]bool, len(current))
	for _, g := range current {
		want[g.ID] = true
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !want[id] || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}

// listMembersHandler handles GET /api/groups/{groupID}/members
// (RequireAuth-protected). Any member (viewer+) can see who else is in
// the group; only admin+ can change that list (invite/remove/re-role).
func (a groupsAPI) listMembersHandler(w http.ResponseWriter, r *http.Request) {
	g, ok := a.authorizeGroup(w, r, r.PathValue("groupID"), groups.RoleViewer)
	if !ok {
		return
	}

	members, err := a.groups.ListMembers(r.Context(), g.ID)
	if err != nil {
		internalError(w, err)
		return
	}

	resp := make([]memberResponse, len(members))
	for i, m := range members {
		resp[i] = toMemberResponse(m)
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

// inviteMemberHandler handles POST /api/groups/{groupID}/members
// (RequireAuth-protected). Body: {"username", "role"}. Adds an
// *existing* account to the group (ADR 0005 §5: people sign up
// themselves; group admins no longer create accounts). Requires admin+
// in the group, and the requested role can never rank higher than the
// caller's own -- otherwise a bare admin could mint a new owner (or a
// peer admin), privilege escalation through the invite path.
func (a groupsAPI) inviteMemberHandler(w http.ResponseWriter, r *http.Request) {
	g, ok := a.authorizeGroup(w, r, r.PathValue("groupID"), groups.RoleAdmin)
	if !ok {
		return
	}

	var req struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if !isValidGroupRole(req.Role) {
		http.Error(w, "role must be one of viewer, editor, admin, owner", http.StatusBadRequest)
		return
	}

	callerID := auth.UserIDFromContext(r.Context())
	callerRole, err := a.groups.GetMemberRole(r.Context(), g.ID, callerID)
	if err != nil {
		internalError(w, err)
		return
	}
	if groupRoleRank[req.Role] > groupRoleRank[callerRole] {
		http.Error(w, "cannot grant a role higher than your own", http.StatusForbidden)
		return
	}

	user, err := a.users.GetByUsername(r.Context(), req.Username)
	if errors.Is(err, db.ErrNotFound) || (err == nil && user.Status != users.StatusActive) {
		http.Error(w, "no such user", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if _, err := a.groups.GetMemberRole(r.Context(), g.ID, user.ID); err == nil {
		http.Error(w, "already a member", http.StatusConflict)
		return
	}

	if err := a.groups.AddMember(r.Context(), g.ID, user.ID, req.Role); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, memberResponse{
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Role:        req.Role,
	})
}

// updateMemberRoleHandler handles PUT
// /api/groups/{groupID}/members/{userID} (RequireAuth-protected).
// Body: {"role": "..."}. Requires admin+, same "can't grant/hold a role
// higher than your own" cap as inviteMemberHandler.
func (a groupsAPI) updateMemberRoleHandler(w http.ResponseWriter, r *http.Request) {
	g, ok := a.authorizeGroup(w, r, r.PathValue("groupID"), groups.RoleAdmin)
	if !ok {
		return
	}
	targetID := r.PathValue("userID")

	var req struct {
		Role string `json:"role"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if !isValidGroupRole(req.Role) {
		http.Error(w, "role must be one of viewer, editor, admin, owner", http.StatusBadRequest)
		return
	}

	callerID := auth.UserIDFromContext(r.Context())
	callerRole, err := a.groups.GetMemberRole(r.Context(), g.ID, callerID)
	if err != nil {
		internalError(w, err)
		return
	}
	if groupRoleRank[req.Role] > groupRoleRank[callerRole] {
		http.Error(w, "cannot grant a role higher than your own", http.StatusForbidden)
		return
	}
	// Nor change the role of someone above you (an admin demoting an
	// owner): only an equal or higher role may touch a member's role.
	if ok := a.outranks(w, r, g.ID, callerRole, targetID); !ok {
		return
	}

	err = a.groups.UpdateMemberRole(r.Context(), g.ID, targetID, req.Role)
	if errors.Is(err, groups.ErrLastOwner) {
		http.Error(w, "cannot demote the group's last owner", http.StatusConflict)
		return
	}
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// removeMemberHandler handles DELETE
// /api/groups/{groupID}/members/{userID} (RequireAuth-protected).
// Any member can remove *themselves* (leave); removing anyone else
// needs admin+. Either way, ErrLastOwner blocks removing the group's
// only remaining owner.
func (a groupsAPI) removeMemberHandler(w http.ResponseWriter, r *http.Request) {
	// authorizeGroup can't be used as-is here -- self-removal needs to
	// work even for a plain viewer, who'd fail an admin+ check. Load
	// the group and confirm the caller is *some* member first (viewer
	// is the floor for even knowing the group exists), then decide
	// admin-or-self below.
	g, ok := a.authorizeGroup(w, r, r.PathValue("groupID"), groups.RoleViewer)
	if !ok {
		return
	}
	targetID := r.PathValue("userID")
	callerID := auth.UserIDFromContext(r.Context())

	if callerID != targetID {
		isAdmin, err := a.perm.CanAccessGroup(r.Context(), callerID, g.ID, groups.RoleAdmin)
		if err != nil {
			internalError(w, err)
			return
		}
		if !isAdmin {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		callerRole, err := a.groups.GetMemberRole(r.Context(), g.ID, callerID)
		if err != nil {
			internalError(w, err)
			return
		}
		if ok := a.outranks(w, r, g.ID, callerRole, targetID); !ok {
			return
		}
	}

	err := a.groups.RemoveMember(r.Context(), g.ID, targetID)
	if errors.Is(err, groups.ErrLastOwner) {
		http.Error(w, "cannot remove the group's last owner", http.StatusConflict)
		return
	}
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// outranks reports (and answers 403/404 itself when not) whether a
// caller with callerRole may act on targetID's membership: the target's
// role must not be higher than the caller's.
func (a groupsAPI) outranks(w http.ResponseWriter, r *http.Request, groupID, callerRole, targetID string) bool {
	targetRole, err := a.groups.GetMemberRole(r.Context(), groupID, targetID)
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return false
	}
	if err != nil {
		internalError(w, err)
		return false
	}
	if groupRoleRank[targetRole] > groupRoleRank[callerRole] {
		http.Error(w, "cannot change a member whose role is higher than yours", http.StatusForbidden)
		return false
	}
	return true
}
