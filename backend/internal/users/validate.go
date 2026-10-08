package users

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
)

var usernameRe = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

var (
	ErrInvalidUsername = errors.New("username must be 3-32 characters: a-z, 0-9, dot, underscore, hyphen")
	ErrInvalidEmail    = errors.New("invalid email address")
)

// NormalizeUsername lowercases and trims; usernames are case-insensitive.
func NormalizeUsername(u string) string { return strings.ToLower(strings.TrimSpace(u)) }

// NormalizeEmail lowercases and trims.
func NormalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// ValidateUsername expects an already-normalized username.
func ValidateUsername(u string) error {
	if !usernameRe.MatchString(u) {
		return ErrInvalidUsername
	}
	return nil
}

// ValidateEmail expects an already-normalized address. Deliberately
// loose: a bare address, at most 254 chars, with a dot in the domain.
func ValidateEmail(e string) error {
	if len(e) > 254 {
		return ErrInvalidEmail
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || a.Name != "" {
		return ErrInvalidEmail
	}
	at := strings.LastIndex(e, "@")
	if at < 1 || !strings.Contains(e[at+1:], ".") {
		return ErrInvalidEmail
	}
	return nil
}
