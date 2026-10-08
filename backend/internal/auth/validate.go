package auth

import "errors"

const (
	MinPasswordLength = 8
	MaxPasswordLength = 256
)

var ErrPasswordLength = errors.New("password must be 8-256 characters")

// ValidatePassword is the one password policy (ADR 0005 §8): length
// only, no composition rules (NIST 800-63B).
func ValidatePassword(p string) error {
	if n := len([]rune(p)); n < MinPasswordLength || len(p) > MaxPasswordLength {
		return ErrPasswordLength
	}
	return nil
}
