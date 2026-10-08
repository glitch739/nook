package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"nook/internal/db"
)

// Preferences returns the account's UI preferences: a JSON object
// whose shape the frontend owns (migration 0017).
func (s *Store) Preferences(ctx context.Context, id string) (map[string]json.RawMessage, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT preferences FROM users WHERE id = ?;`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("users: preferences: %w", db.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("users: preferences: %w", err)
	}
	prefs := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &prefs); err != nil {
		// A corrupt value shouldn't lock anyone out of their settings.
		return map[string]json.RawMessage{}, nil
	}
	return prefs, nil
}

// MergePreferences sets the given top-level keys (a JSON null removes
// one) and keeps the rest, so two devices saving different keys don't
// overwrite each other. Returns the result.
func (s *Store) MergePreferences(ctx context.Context, id string, patch map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	prefs, err := s.Preferences(ctx, id)
	if err != nil {
		return nil, err
	}
	for k, v := range patch {
		if string(v) == "null" {
			delete(prefs, k)
		} else {
			prefs[k] = v
		}
	}
	out, err := json.Marshal(prefs)
	if err != nil {
		return nil, fmt.Errorf("users: merge preferences: %w", err)
	}
	if err := s.exec(ctx, "set preferences", `UPDATE users SET preferences = ? WHERE id = ?;`, string(out), id); err != nil {
		return nil, err
	}
	return prefs, nil
}
