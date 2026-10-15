package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"nook/internal/activity"

	"nook/internal/databases"
	"nook/internal/db"
	"nook/internal/httpx"
	"nook/internal/pages"
	"nook/internal/permissions"
	"nook/internal/spaces"
)

// databasesAPI bundles databases.Store with pages.Store and
// permissions.Checker -- same shape as pagesAPI/spacesAPI, for the same
// reason: a property or row ID on its own doesn't prove the caller has
// access to the space it actually lives in, that has to be resolved
// and checked explicitly every time. See docs/adr/0004-database-pages.md.
type databasesAPI struct {
	databases *databases.Store
	pages     *pages.Store
	spaces    *spaces.Store
	perm      *permissions.Checker
	activity  *activity.Store
}

type propertyResponse struct {
	ID string `json:"id"`
	// PageID is the database page the column belongs to (the "Person"
	// cell uses it to ask who can be picked).
	PageID   string                   `json:"page_id"`
	Name     string                   `json:"name"`
	Type     string                   `json:"type"`
	Options  []databases.SelectOption `json:"options"`
	Position string                   `json:"position"`
}

func toPropertyResponse(p databases.Property) propertyResponse {
	return propertyResponse{ID: p.ID, PageID: p.PageID, Name: p.Name, Type: p.Type, Options: p.Options, Position: p.Position}
}

// authorizeDatabasePage loads pageID, checks it's actually a database
// page (not a document page under a mismatched endpoint), and checks
// the caller holds at least minRole in its space -- same
// 404-for-both-"doesn't exist"-and-"can't see it" convention as
// pagesAPI.authorizePage.
func (a databasesAPI) authorizeDatabasePage(w http.ResponseWriter, r *http.Request, pageID, minRole string) (pages.Page, bool) {
	page, ok := authorizePageWith(w, r, a.perm, a.pages.GetMetaByID, pageID, minRole)
	if ok && page.Kind != pages.KindDatabase {
		http.Error(w, "not found", http.StatusNotFound)
		return pages.Page{}, false
	}
	return page, ok
}

// authorizeProperty loads propertyID, then resolves and authorizes the
// database page it belongs to -- properties don't carry a space_id of
// their own, so access always goes through their owning page.
func (a databasesAPI) authorizeProperty(w http.ResponseWriter, r *http.Request, propertyID, minRole string) (databases.Property, bool) {
	prop, err := a.databases.GetProperty(r.Context(), propertyID)
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return databases.Property{}, false
	}
	if err != nil {
		internalError(w, err)
		return databases.Property{}, false
	}
	if _, ok := a.authorizeDatabasePage(w, r, prop.PageID, minRole); !ok {
		return databases.Property{}, false
	}
	return prop, true
}

// createPropertyHandler handles POST /api/pages/{pageID}/properties
// (RequireAuth-protected). Body: {"name": "...", "type": "text"|
// "select"|"date"|"person"|"number"|"checkbox"}. Requires at least
// editor on the database page's space.
func (a databasesAPI) createPropertyHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizeDatabasePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}

	var req struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	prop, err := a.databases.CreateProperty(r.Context(), page.ID, req.Name, req.Type)
	if errors.Is(err, databases.ErrInvalidType) {
		http.Error(w, "invalid property type", http.StatusBadRequest)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toPropertyResponse(prop))
}

// reorderColumnsHandler handles PUT /api/pages/{pageID}/columns/order
// (RequireAuth-protected). Body: {"column_ids": [...]} -- must be
// exactly the database page's current columns in their new order: its
// property columns, plus databases.TitleColumnID exactly once for the
// title ("Name") column, which can be dragged among the others too, not
// just fixed first. No partial moves, no injecting an id from
// elsewhere; validated here rather than in internal/databases, same
// layering every other domain package here uses. Requires at least
// editor on the database page's space.
func (a databasesAPI) reorderColumnsHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizeDatabasePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}

	var req struct {
		ColumnIDs []string `json:"column_ids"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}

	current, err := a.databases.ListProperties(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}

	propertyIDs := make([]string, 0, len(req.ColumnIDs))
	titleCount := 0
	for _, id := range req.ColumnIDs {
		if id == databases.TitleColumnID {
			titleCount++
			continue
		}
		propertyIDs = append(propertyIDs, id)
	}
	if titleCount != 1 || !isPermutationOf(propertyIDs, current) {
		http.Error(w, "column_ids must include the title column exactly once, plus exactly this database's current property columns", http.StatusBadRequest)
		return
	}

	if err := a.databases.ReorderColumns(r.Context(), page.ID, req.ColumnIDs); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isPermutationOf(ids []string, properties []databases.Property) bool {
	if len(ids) != len(properties) {
		return false
	}
	want := make(map[string]bool, len(properties))
	for _, p := range properties {
		want[p.ID] = true
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

// updatePropertyHandler handles PUT /api/properties/{propertyID}
// (RequireAuth-protected). Body: {"name": "..."?, "options": [...]?} --
// either field may be omitted; the column's type is never editable
// (see databases.Store.UpdateProperty). Requires at least editor on the
// owning database page's space.
func (a databasesAPI) updatePropertyHandler(w http.ResponseWriter, r *http.Request) {
	prop, ok := a.authorizeProperty(w, r, r.PathValue("propertyID"), spaces.RoleEditor)
	if !ok {
		return
	}

	var req struct {
		Name    *string                   `json:"name"`
		Options *[]databases.SelectOption `json:"options"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Name == nil && req.Options == nil {
		http.Error(w, "nothing to update: provide name and/or options", http.StatusBadRequest)
		return
	}

	updated, err := a.databases.UpdateProperty(r.Context(), prop.ID, req.Name, req.Options)
	if err != nil {
		internalError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toPropertyResponse(updated))
}

// deletePropertyHandler handles DELETE /api/properties/{propertyID}
// (RequireAuth-protected): removes a column and every row's value for
// it. Requires at least editor on the owning database page's space.
func (a databasesAPI) deletePropertyHandler(w http.ResponseWriter, r *http.Request) {
	prop, ok := a.authorizeProperty(w, r, r.PathValue("propertyID"), spaces.RoleEditor)
	if !ok {
		return
	}
	if err := a.databases.DeleteProperty(r.Context(), prop.ID); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type rowResponse struct {
	ID       string                     `json:"id"`
	Title    string                     `json:"title"`
	Icon     string                     `json:"icon"`
	Position string                     `json:"position"`
	Values   map[string]json.RawMessage `json:"values"`
}

type rowsResponse struct {
	Properties []propertyResponse `json:"properties"`
	Rows       []rowResponse      `json:"rows"`
	// TitleColumnPosition is the title ("Name") column's position among
	// Properties -- the frontend sorts them together (plain string
	// comparison; both use the same zero-padded scheme) to render them
	// interleaved in the right order, since the title itself isn't one
	// of the Properties entries. See databases.Store.GetTitleColumnPosition.
	TitleColumnPosition string `json:"title_column_position"`
	// BoardOrder is the board view's card order (row ids), kept apart
	// from the table's row order. See boardOrderHandler.
	BoardOrder []string `json:"board_order"`
	// BoardColumns is the board view's column order per grouping
	// property. See boardColumnsHandler.
	BoardColumns map[string][]string `json:"board_columns"`
}

// listRowsHandler handles GET /api/pages/{pageID}/rows
// (RequireAuth-protected): a database page's whole table -- its
// columns plus every row (its non-archived child pages) and their
// values, in one response shaped for a table view. Requires at least
// viewer on the database page's space.
func (a databasesAPI) listRowsHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizeDatabasePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}

	props, err := a.databases.ListProperties(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	children, err := a.pages.ListChildren(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	values, err := a.databases.ValuesForDatabase(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	titlePosition, err := a.databases.GetTitleColumnPosition(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}

	boardOrder, err := a.databases.BoardOrder(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	boardColumns, err := a.databases.BoardColumns(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}

	resp := rowsResponse{
		Properties:          make([]propertyResponse, len(props)),
		Rows:                make([]rowResponse, len(children)),
		TitleColumnPosition: titlePosition,
		BoardOrder:          boardOrder,
		BoardColumns:        boardColumns,
	}
	for i, p := range props {
		resp.Properties[i] = toPropertyResponse(p)
	}
	for i, c := range children {
		rowValues := make(map[string]json.RawMessage, len(values[c.ID]))
		for propID, valueJSON := range values[c.ID] {
			rowValues[propID] = json.RawMessage(valueJSON)
		}
		resp.Rows[i] = rowResponse{
			ID:       c.ID,
			Title:    c.Title,
			Icon:     c.Icon,
			Position: c.Position,
			Values:   rowValues,
		}
	}

	httpx.WriteJSON(w, http.StatusOK, resp)
}

// setRowPropertyHandler handles PUT
// /api/pages/{rowID}/properties/{propertyID} (RequireAuth-protected).
// Body: {"value": <anything>} -- the raw value for this cell, shaped
// per the column's type (see the migration's comment on value_json).
// Authorized through the row itself (an ordinary page, same as
// pagesAPI.authorizePage) rather than through the database page, since
// the row is what the caller actually has a page ID for; the property
// is then checked to make sure it really belongs to the row's parent,
// so a client can't write a cell under a column from an unrelated
// database it happens to know the ID of. Requires at least editor.
func (a databasesAPI) setRowPropertyHandler(w http.ResponseWriter, r *http.Request) {
	row, ok := authorizePageWith(w, r, a.perm, a.pages.GetMetaByID, r.PathValue("rowID"), spaces.RoleEditor)
	if !ok {
		return
	}

	propertyID := r.PathValue("propertyID")
	prop, err := a.databases.GetProperty(r.Context(), propertyID)
	if errors.Is(err, db.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if row.ParentPageID == nil || *row.ParentPageID != prop.PageID {
		http.Error(w, "property does not belong to this row's database", http.StatusBadRequest)
		return
	}

	var req struct {
		Value json.RawMessage `json:"value"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}

	if prop.Type == databases.TypePerson {
		ok, err := a.validPersonValue(r, row.SpaceID, req.Value)
		if err != nil {
			internalError(w, err)
			return
		}
		if !ok {
			http.Error(w, `a person value must be null or {"user_id": ...} of someone in this space`, http.StatusBadRequest)
			return
		}
	}

	if err := a.databases.SetValue(r.Context(), row.ID, prop.ID, string(req.Value)); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	recordActivity(r, a.activity, &row, activity.RowEdit, map[string]any{"property": prop.Name})
	w.WriteHeader(http.StatusNoContent)
}

// validPersonValue accepts null (cleared) or {"user_id": id} where id
// can reach the space -- so a cell can't point at an arbitrary account.
// (Older cells may still hold a plain name string; they keep displaying,
// but new values must be a real person.)
func (a databasesAPI) validPersonValue(r *http.Request, spaceID string, raw json.RawMessage) (bool, error) {
	if string(raw) == "null" || len(raw) == 0 {
		return true, nil
	}
	var v struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(raw, &v); err != nil || v.UserID == "" {
		return false, nil
	}
	people, err := a.spaces.People(r.Context(), spaceID)
	if err != nil {
		return false, err
	}
	for _, p := range people {
		if p.ID == v.UserID {
			return true, nil
		}
	}
	return false, nil
}

// GET /api/pages/{pageID}/people (viewer+ on a database page): who a
// "Person" cell can pick.
func (a databasesAPI) peopleHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizeDatabasePage(w, r, r.PathValue("pageID"), spaces.RoleViewer)
	if !ok {
		return
	}
	people, err := a.spaces.People(r.Context(), page.SpaceID)
	if err != nil {
		internalError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, people)
}

// boardOrderHandler handles PUT /api/pages/{pageID}/board-order (editor+).
// Body: {"row_ids": [...]}: the board view's card order. Unlike the
// column order this is not required to be a full permutation: rows come
// and go while someone drags, so unknown ids and repeats are dropped and
// rows left out simply follow the listed ones.
func (a databasesAPI) boardOrderHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizeDatabasePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}
	var req struct {
		RowIDs []string `json:"row_ids"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	children, err := a.pages.ListChildren(r.Context(), page.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	live := make(map[string]bool, len(children))
	for _, c := range children {
		live[c.ID] = true
	}
	ids := make([]string, 0, len(req.RowIDs))
	for _, id := range req.RowIDs {
		if live[id] {
			ids = append(ids, id)
			delete(live, id)
		}
	}
	if err := a.databases.SetBoardOrder(r.Context(), page.ID, ids); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// boardColumnsHandler handles PUT /api/pages/{pageID}/board-columns
// (editor+). Body: {"property_id": ..., "keys": [...]}: the board's
// column order when it groups by that select or person property, keys
// being option ids or user ids and "" for the "No <name>" column. Layout
// only: the property's options and the rows' values stay as they are.
// Keys aren't checked against the current options (an option can be
// added or removed meanwhile; unknown keys are simply never shown), only
// for shape: unique, short, a bounded number.
func (a databasesAPI) boardColumnsHandler(w http.ResponseWriter, r *http.Request) {
	page, ok := a.authorizeDatabasePage(w, r, r.PathValue("pageID"), spaces.RoleEditor)
	if !ok {
		return
	}
	var req struct {
		PropertyID string   `json:"property_id"`
		Keys       []string `json:"keys"`
	}
	if !httpx.DecodeJSONBody(w, r, &req) {
		return
	}
	prop, err := a.databases.GetProperty(r.Context(), req.PropertyID)
	if err != nil || prop.PageID != page.ID || (prop.Type != databases.TypeSelect && prop.Type != databases.TypePerson) {
		http.Error(w, "property_id must be a select or person column of this database", http.StatusBadRequest)
		return
	}
	if len(req.Keys) > 500 {
		http.Error(w, "too many columns", http.StatusBadRequest)
		return
	}
	seen := make(map[string]bool, len(req.Keys))
	keys := make([]string, 0, len(req.Keys))
	for _, k := range req.Keys {
		if len(k) > 100 {
			http.Error(w, "column key too long", http.StatusBadRequest)
			return
		}
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	if err := a.databases.SetBoardColumns(r.Context(), page.ID, prop.ID, keys); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
