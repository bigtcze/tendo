package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	maxPasswordBytes = 512
	argonMemory      = 64 * 1024
	argonIterations  = 3
	argonParallelism = 1
	argonSaltBytes   = 16
	argonKeyBytes    = 32
)

// ErrInvalidPasswordHash reports a stored hash that is not a valid PHC string
// for the fixed parameters: a credential integrity fault, not a mismatch.
var ErrInvalidPasswordHash = errors.New("invalid password hash")
var ErrPasswordWorkLimit = errors.New("password work limit reached")

// HashPassword creates a versioned Argon2id PHC string using the fixed parameters
// selected for local account password storage.
func HashPassword(password string) (string, error) {
	if len(password) > maxPasswordBytes {
		return "", errors.New("password exceeds maximum length")
	}

	salt := make([]byte, argonSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", errors.New("could not generate password salt")
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyBytes)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword validates a stored Argon2id PHC string against the exact input bytes.
func VerifyPassword(encoded, password string) (bool, error) {
	if len(password) > maxPasswordBytes {
		return false, errors.New("password exceeds maximum length")
	}
	salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, ErrInvalidPasswordHash
	}
	actual := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyBytes)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func parsePasswordHash(encoded string) ([]byte, []byte, error) {
	parts, ok := passwordHashEnvelope(encoded)
	if !ok {
		return nil, nil, ErrInvalidPasswordHash
	}
	params, ok := parsePasswordParameters(parts[3])
	if !ok || !fixedPasswordParameters(params) {
		return nil, nil, ErrInvalidPasswordHash
	}
	salt, ok := decodeCanonicalHashPart(parts[4], argonSaltBytes)
	if !ok {
		return nil, nil, ErrInvalidPasswordHash
	}
	key, ok := decodeCanonicalHashPart(parts[5], argonKeyBytes)
	if !ok {
		return nil, nil, ErrInvalidPasswordHash
	}
	return salt, key, nil
}

func passwordHashEnvelope(encoded string) ([]string, bool) {
	parts := strings.Split(encoded, "$")
	return parts, len(parts) == 6 && parts[0] == "" && parts[1] == "argon2id" && parts[2] == "v=19"
}

func parsePasswordParameters(encoded string) (map[string]uint64, bool) {
	params := strings.Split(encoded, ",")
	if len(params) != 3 {
		return nil, false
	}
	values := make(map[string]uint64, len(params))
	for _, param := range params {
		key, value, ok := strings.Cut(param, "=")
		if !ok || key == "" || value == "" {
			return nil, false
		}
		if _, exists := values[key]; exists {
			return nil, false
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, false
		}
		values[key] = n
	}
	return values, true
}

func fixedPasswordParameters(values map[string]uint64) bool {
	return len(values) == 3 && values["m"] == argonMemory && values["t"] == argonIterations && values["p"] == argonParallelism
}

func decodeCanonicalHashPart(encoded string, length int) ([]byte, bool) {
	decoded, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != length || base64.RawStdEncoding.EncodeToString(decoded) != encoded {
		return nil, false
	}
	return decoded, true
}
