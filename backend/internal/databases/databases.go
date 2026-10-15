// Package databases manages the columns ("properties") and cell values
// of database pages -- see docs/adr/0004-database-pages.md. A database
// page's rows are themselves ordinary internal/pages rows (that
// package owns their creation, deletion, icons, and rich-text body);
// this package only owns the structured schema layered on top of them.
package databases

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"nook/internal/db"
)

// ErrInvalidType is returned by CreateProperty when propType isn't one
// of the Type* constants -- a client (400) error, distinct from
// db.ErrNotFound and from a genuine internal error, so httpapi can tell
// the three apart.
var ErrInvalidType = errors.New("databases: invalid property type")

// Property types. A column's type never changes after creation --
// retyping a column with live values would make its existing
// value_json meaningless, and nothing in the UI offers it.
const (
	TypeText     = "text"
	TypeSelect   = "select"
	TypeDate     = "date"
	TypePerson   = "person"
	TypeNumber   = "number"
	TypeCheckbox = "checkbox"
)

// TitleColumnID is the sentinel column id representing a database
// page's title ("Name") column in ReorderColumns' input -- it isn't a
// real database_properties row (the title lives on the page itself),
// but it can still be dragged and reordered among the real columns, so
// it needs an id to appear in that ordering alongside them. Never
// collides with a real property id (those are db.NewID() UUIDs).
const TitleColumnID = "title"

var validTypes = map[string]bool{
	TypeText:     true,
	TypeSelect:   true,
	TypeDate:     true,
	TypePerson:   true,
	TypeNumber:   true,
	TypeCheckbox: true,
}

// SelectOption is one user-defined value of a 'select' column (e.g. a
// Status column's "Done"/"In progress"/... values), each with its own
// color -- freely added, renamed, recolored, or removed; never a fixed
// enum baked into the schema.
type SelectOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Color string `json:"color"`
}

// Property is one column of a database page.
type Property struct {
	ID        string
	PageID    string
	Name      string
	Type      string
	Options   []SelectOption
	Position  string
	CreatedAt string
}

// positionWidth matches internal/pages' own positionWidth -- same
// zero-padded-counter scheme, same "revisit when real reordering
// exists" deferral (see that package's comment on the constant).
const positionWidth = 6

type Store struct {
	db *sql.DB
}

func NewStore(conn *sql.DB) *Store {
	return &Store{db: conn}
}

// CreateProperty adds a new column to a database page, appended after
// its existing columns. propType must be one of the Type* constants.
func (s *Store) CreateProperty(ctx context.Context, pageID, name, propType string) (Property, error) {
	if !validTypes[propType] {
		return Property{}, fmt.Errorf("databases: create property: %w: %q", ErrInvalidType, propType)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Property{}, fmt.Errorf("databases: begin create property transaction: %w", err)
	}
	defer tx.Rollback()

	position, err := nextPropertyPosition(ctx, tx, pageID)
	if err != nil {
		return Property{}, fmt.Errorf("databases: compute position: %w", err)
	}

	id := db.NewID()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO database_properties (id, page_id, name, type, position)
		VALUES (?, ?, ?, ?, ?);
	`, id, pageID, name, propType, position)
	if err != nil {
		return Property{}, fmt.Errorf("databases: create property: %w", err)
	}

	prop, err := scanProperty(tx.QueryRowContext(ctx, selectPropertyByID, id))
	if err != nil {
		return Property{}, fmt.Errorf("databases: read created property: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Property{}, fmt.Errorf("databases: commit create property transaction: %w", err)
	}
	return prop, nil
}

// nextPropertyPosition returns a position that sorts after every
// existing column of pageID -- mirrors internal/pages' nextPosition.
func nextPropertyPosition(ctx context.Context, tx *sql.Tx, pageID string) (string, error) {
	var maxPosition sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT MAX(position) FROM database_properties WHERE page_id = ?;
	`, pageID).Scan(&maxPosition)
	if err != nil {
		return "", err
	}

	next := 1
	if maxPosition.Valid {
		var current int
		if _, err := fmt.Sscanf(maxPosition.String, "%d", &current); err == nil {
			next = current + 1
		}
	}
	return fmt.Sprintf("%0*d", positionWidth, next), nil
}

const selectPropertyByID = `
	SELECT id, page_id, name, type, options_json, position, created_at
	FROM database_properties WHERE id = ?;
`

// GetProperty looks up one column by ID. Returns an error wrapping
// db.ErrNotFound if no such property exists.
func (s *Store) GetProperty(ctx context.Context, id string) (Property, error) {
	return scanProperty(s.db.QueryRowContext(ctx, selectPropertyByID, id))
}

// ListProperties returns every column of a database page, in display
// order.
func (s *Store) ListProperties(ctx context.Context, pageID string) ([]Property, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, page_id, name, type, options_json, position, created_at
		FROM database_properties
		WHERE page_id = ?
		ORDER BY position;
	`, pageID)
	if err != nil {
		return nil, fmt.Errorf("databases: list properties: %w", err)
	}
	defer rows.Close()

	list := make([]Property, 0)
	for rows.Next() {
		p, err := scanPropertyRow(rows)
		if err != nil {
			return nil, fmt.Errorf("databases: scan property: %w", err)
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("databases: list properties rows: %w", err)
	}
	return list, nil
}

// ReorderColumns updates every column's position to match columnIDs'
// order -- the *whole* set of pageID's columns (its property columns
// plus TitleColumnID, exactly once) in the new desired sequence, not a
// single move. Columns are always few enough per database that simply
// re-stamping every position on a reorder is simpler than, and just as
// correct as, real fractional indexing -- unlike internal/pages'
// row/page ordering, where fractional indexing is deliberately
// deferred to Phase 4 instead, since a page can have far more siblings
// than a database ever realistically has columns. Each property UPDATE
// is scoped to pageID too (not just the property id), so an id from an
// unrelated database can't sneak a position change in here; validating
// that columnIDs is exactly this page's current column set (no more,
// no less, title included once) is the caller's job (httpapi), same
// layering as internal/pages leaving content_json validation to
// nothing at all (ADR 0002) -- this store only owns the mechanics of
// applying an already-trusted order.
func (s *Store) ReorderColumns(ctx context.Context, pageID string, columnIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("databases: begin reorder transaction: %w", err)
	}
	defer tx.Rollback()

	for i, id := range columnIDs {
		position := fmt.Sprintf("%0*d", positionWidth, i+1)
		if id == TitleColumnID {
			if err := setTitleColumnPosition(ctx, tx, pageID, position); err != nil {
				return err
			}
			continue
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE database_properties SET position = ? WHERE id = ? AND page_id = ?;
		`, position, id, pageID)
		if err != nil {
			return fmt.Errorf("databases: reorder property: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("databases: reorder property: %w", db.ErrNotFound)
		}
	}

	return tx.Commit()
}

// GetTitleColumnPosition returns pageID's title column's position
// among its other columns -- '000000' (sorts before the first real
// property, whose positions start at '000001') if no explicit position
// has ever been set, matching every database's original fixed
// title-always-first behavior.
func (s *Store) GetTitleColumnPosition(ctx context.Context, pageID string) (string, error) {
	var pos string
	err := s.db.QueryRowContext(ctx, `
		SELECT title_column_position FROM database_settings WHERE page_id = ?;
	`, pageID).Scan(&pos)
	if err == sql.ErrNoRows {
		return "000000", nil
	}
	if err != nil {
		return "", fmt.Errorf("databases: get title column position: %w", err)
	}
	return pos, nil
}

func setTitleColumnPosition(ctx context.Context, tx *sql.Tx, pageID, position string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO database_settings (page_id, title_column_position)
		VALUES (?, ?)
		ON CONFLICT (page_id) DO UPDATE SET title_column_position = excluded.title_column_position;
	`, pageID, position)
	if err != nil {
		return fmt.Errorf("databases: set title column position: %w", err)
	}
	return nil
}

// UpdateProperty renames a column and/or replaces its options list (for
// a 'select' column -- add/rename/recolor/remove a status value). Both
// arguments are optional (nil leaves that field unchanged), matching
// the same partial-update shape pages.Store.UpdateTitle/UpdateIcon's
// HTTP handlers already use.
func (s *Store) UpdateProperty(ctx context.Context, id string, name *string, options *[]SelectOption) (Property, error) {
	if name == nil && options == nil {
		return s.GetProperty(ctx, id)
	}

	var optionsJSON *string
	if options != nil {
		encoded, err := json.Marshal(*options)
		if err != nil {
			return Property{}, fmt.Errorf("databases: marshal options: %w", err)
		}
		s := string(encoded)
		optionsJSON = &s
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE database_properties
		SET name = COALESCE(?, name), options_json = COALESCE(?, options_json)
		WHERE id = ?;
	`, name, optionsJSON, id)
	if err != nil {
		return Property{}, fmt.Errorf("databases: update property: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Property{}, fmt.Errorf("databases: update property: %w", db.ErrNotFound)
	}
	return s.GetProperty(ctx, id)
}

// DeleteProperty removes a column. Every row's value for it goes with
// it (ON DELETE CASCADE via database_property_values.property_id).
func (s *Store) DeleteProperty(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM database_properties WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("databases: delete property: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("databases: delete property: %w", db.ErrNotFound)
	}
	return nil
}

// SetValue sets one row's value for one column -- an upsert, since a
// row may never have touched this column before. valueJSON's shape
// depends on the column's type (see the migration's comment); this
// package doesn't validate it against the column's declared type --
// same "opaque to the store" treatment internal/pages gives
// content_json (ADR 0002) and icon.
func (s *Store) SetValue(ctx context.Context, rowPageID, propertyID, valueJSON string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO database_property_values (page_id, property_id, value_json)
		VALUES (?, ?, ?)
		ON CONFLICT (page_id, property_id) DO UPDATE SET value_json = excluded.value_json;
	`, rowPageID, propertyID, valueJSON)
	if err != nil {
		return fmt.Errorf("databases: set value: %w", err)
	}
	return nil
}

// ValuesForDatabase returns every row's values for a database page, as
// rowPageID -> propertyID -> value_json -- one query for a whole table
// view rather than N+1 round trips per row.
func (s *Store) ValuesForDatabase(ctx context.Context, databasePageID string) (map[string]map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.page_id, v.property_id, v.value_json
		FROM database_property_values AS v
		JOIN pages AS p ON p.id = v.page_id
		WHERE p.parent_page_id = ?;
	`, databasePageID)
	if err != nil {
		return nil, fmt.Errorf("databases: values for database: %w", err)
	}
	defer rows.Close()

	values := make(map[string]map[string]string)
	for rows.Next() {
		var rowPageID, propertyID, valueJSON string
		if err := rows.Scan(&rowPageID, &propertyID, &valueJSON); err != nil {
			return nil, fmt.Errorf("databases: scan value: %w", err)
		}
		if values[rowPageID] == nil {
			values[rowPageID] = make(map[string]string)
		}
		values[rowPageID][propertyID] = valueJSON
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("databases: values rows: %w", err)
	}
	return values, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProperty(row *sql.Row) (Property, error) {
	return scanPropertyLike(row)
}

func scanPropertyRow(rows *sql.Rows) (Property, error) {
	return scanPropertyLike(rows)
}

func scanPropertyLike(s rowScanner) (Property, error) {
	var p Property
	var optionsJSON string
	err := s.Scan(&p.ID, &p.PageID, &p.Name, &p.Type, &optionsJSON, &p.Position, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return Property{}, fmt.Errorf("databases: %w", db.ErrNotFound)
	}
	if err != nil {
		return Property{}, err
	}
	p.Options = make([]SelectOption, 0)
	if err := json.Unmarshal([]byte(optionsJSON), &p.Options); err != nil {
		return Property{}, fmt.Errorf("databases: unmarshal options: %w", err)
	}
	return p, nil
}

// BoardOrder returns the database's board card order (row ids), empty
// when never set.
func (s *Store) BoardOrder(ctx context.Context, pageID string) ([]string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT board_order FROM database_settings WHERE page_id = ?;`, pageID).Scan(&raw)
	if err == sql.ErrNoRows {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("databases: get board order: %w", err)
	}
	ids := []string{}
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return []string{}, nil // unreadable: as if never set
	}
	return ids, nil
}

// SetBoardOrder stores the board card order (validated by the caller).
func (s *Store) SetBoardOrder(ctx context.Context, pageID string, rowIDs []string) error {
	raw, err := json.Marshal(rowIDs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO database_settings (page_id, board_order) VALUES (?, ?)
		ON CONFLICT (page_id) DO UPDATE SET board_order = excluded.board_order;`, pageID, string(raw))
	if err != nil {
		return fmt.Errorf("databases: set board order: %w", err)
	}
	return nil
}

// BoardColumns returns the board's column order per grouping property
// (property id -> column keys, "" for "no value"), empty when never set.
func (s *Store) BoardColumns(ctx context.Context, pageID string) (map[string][]string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT board_columns FROM database_settings WHERE page_id = ?;`, pageID).Scan(&raw)
	if err == sql.ErrNoRows {
		return map[string][]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("databases: get board columns: %w", err)
	}
	cols := map[string][]string{}
	if err := json.Unmarshal([]byte(raw), &cols); err != nil {
		return map[string][]string{}, nil // unreadable: as if never set
	}
	return cols, nil
}

// SetBoardColumns stores the column order for one grouping property
// (validated by the caller), keeping the other properties' orders.
func (s *Store) SetBoardColumns(ctx context.Context, pageID, propertyID string, keys []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("databases: begin board columns: %w", err)
	}
	defer tx.Rollback()
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT board_columns FROM database_settings WHERE page_id = ?;`, pageID).Scan(&raw)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("databases: get board columns: %w", err)
	}
	cols := map[string][]string{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &cols) // unreadable: start over
	}
	cols[propertyID] = keys
	out, err := json.Marshal(cols)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO database_settings (page_id, board_columns) VALUES (?, ?)
		ON CONFLICT (page_id) DO UPDATE SET board_columns = excluded.board_columns;`, pageID, string(out)); err != nil {
		return fmt.Errorf("databases: set board columns: %w", err)
	}
	return tx.Commit()
}
