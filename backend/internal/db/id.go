package db

import "github.com/google/uuid"

// NewID returns a new random (v4) UUID string, used as the primary key
// for every domain table (users, spaces, pages, ...).
func NewID() string {
	return uuid.NewString()
}
