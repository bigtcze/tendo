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

var errInvalidPasswordHash = errors.New("invalid password hash")
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
		return false, errInvalidPasswordHash
	}
	actual := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyBytes)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func parsePasswordHash(encoded string) ([]byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return nil, nil, errInvalidPasswordHash
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return nil, nil, errInvalidPasswordHash
	}
	values := make(map[string]uint64, 3)
	for _, param := range params {
		key, value, ok := strings.Cut(param, "=")
		if !ok || key == "" || value == "" {
			return nil, nil, errInvalidPasswordHash
		}
		if _, exists := values[key]; exists {
			return nil, nil, errInvalidPasswordHash
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, nil, errInvalidPasswordHash
		}
		values[key] = n
	}
	if len(values) != 3 || values["m"] != argonMemory || values["t"] != argonIterations || values["p"] != argonParallelism {
		return nil, nil, errInvalidPasswordHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != argonSaltBytes || base64.RawStdEncoding.EncodeToString(salt) != parts[4] {
		return nil, nil, errInvalidPasswordHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) != argonKeyBytes || base64.RawStdEncoding.EncodeToString(key) != parts[5] {
		return nil, nil, errInvalidPasswordHash
	}
	return salt, key, nil
}
