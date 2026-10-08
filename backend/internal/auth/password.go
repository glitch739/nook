package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemoryKiB   = 64 * 1024
	argonIterations  = 3
	argonParallelism = 2
	argonSaltLen     = 16
	argonKeyLen      = 32
)

// HashPassword derives a salted argon2id hash of password and returns it
// as a single encoded string safe to store in users.password_hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)

	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		argonIterations,
		argonMemoryKiB,
		argonParallelism,
		argonKeyLen,
	)

	encodedSalt := base64.RawStdEncoding.EncodeToString(salt)
	encodedHash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		"argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemoryKiB,
		argonIterations,
		argonParallelism,
		encodedSalt,
		encodedHash,
	), nil
}

// VerifyPassword reports whether password matches the previously-hashed
// encoded string produced by HashPassword.
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 {
		return false, nil
	}

	if parts[0] != "argon2id" {
		return false, nil
	}

	if parts[1] != "v=19" {
		return false, nil
	}

	memory, iterations, parallelism, err := parseArgonParams(parts[2])
	if err != nil {
		return false, nil
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false, nil
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, nil
	}

	if len(salt) == 0 || len(expectedHash) == 0 {
		return false, nil
	}

	derivedHash := argon2.IDKey(
		[]byte(password),
		salt,
		iterations,
		memory,
		parallelism,
		uint32(len(expectedHash)),
	)

	if len(derivedHash) != len(expectedHash) {
		return false, nil
	}

	return subtle.ConstantTimeCompare(derivedHash, expectedHash) == 1, nil
}

func parseArgonParams(encoded string) (memory, iterations uint32, parallelism uint8, err error) {
	params := strings.Split(encoded, ",")
	if len(params) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid argon2 parameters")
	}

	var (
		memorySet      bool
		iterationsSet  bool
		parallelismSet bool
	)

	for _, param := range params {
		parts := strings.SplitN(param, "=", 2)
		if len(parts) != 2 {
			return 0, 0, 0, fmt.Errorf("invalid argon2 parameter")
		}

		switch parts[0] {
		case "m":
			if memorySet {
				return 0, 0, 0, fmt.Errorf("duplicate memory parameter")
			}

			value, err := strconv.ParseUint(parts[1], 10, 32)
			if err != nil || value == 0 {
				return 0, 0, 0, fmt.Errorf("invalid memory parameter")
			}

			memory = uint32(value)
			memorySet = true

		case "t":
			if iterationsSet {
				return 0, 0, 0, fmt.Errorf("duplicate iterations parameter")
			}

			value, err := strconv.ParseUint(parts[1], 10, 32)
			if err != nil || value == 0 {
				return 0, 0, 0, fmt.Errorf("invalid iterations parameter")
			}

			iterations = uint32(value)
			iterationsSet = true

		case "p":
			if parallelismSet {
				return 0, 0, 0, fmt.Errorf("duplicate parallelism parameter")
			}

			value, err := strconv.ParseUint(parts[1], 10, 8)
			if err != nil || value == 0 {
				return 0, 0, 0, fmt.Errorf("invalid parallelism parameter")
			}

			parallelism = uint8(value)
			parallelismSet = true

		default:
			return 0, 0, 0, fmt.Errorf("unknown argon2 parameter")
		}
	}

	if !memorySet || !iterationsSet || !parallelismSet {
		return 0, 0, 0, fmt.Errorf("missing argon2 parameter")
	}

	return memory, iterations, parallelism, nil
}
