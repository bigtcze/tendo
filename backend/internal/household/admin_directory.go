package household

import (
	"encoding/base64"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bigtcze/tendo/backend/internal/platform/ids"
)

// MaxDirectorySearchRunes bounds the account directory search term.
const MaxDirectorySearchRunes = 64

// ValidateDirectorySearch returns the trimmed search term or a validation
// error on field q. Empty means no filter. Control, format and separator
// characters are refused, as for item search.
func ValidateDirectorySearch(q string) (string, error) {
	if !utf8.ValidString(q) {
		return "", &ValidationError{Field: "q", Code: "invalid_format"}
	}
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > MaxDirectorySearchRunes {
		return "", &ValidationError{Field: "q", Code: "invalid_format"}
	}
	for _, r := range q {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return "", &ValidationError{Field: "q", Code: "invalid_format"}
		}
	}
	return q, nil
}

func directoryPage(search, cursor string, limit int) (DirectoryPage, error) {
	term, err := ValidateDirectorySearch(search)
	if err != nil {
		return DirectoryPage{}, err
	}
	login, userID, err := DecodeDirectoryCursor(cursor)
	if err != nil {
		return DirectoryPage{}, err
	}
	return DirectoryPage{Search: term, AfterLogin: login, AfterUserID: userID, Limit: limit + 1}, nil
}

// EncodeDirectoryCursor produces the opaque "d1" directory cursor: the last
// row's canonical user ID followed by its login.
func EncodeDirectoryCursor(login, userID string) string {
	return "d1" + base64.RawURLEncoding.EncodeToString([]byte(userID+login))
}

// DecodeDirectoryCursor validates and unwraps a directory cursor.
func DecodeDirectoryCursor(cursor string) (string, string, error) {
	if cursor == "" {
		return "", "", nil
	}
	invalid := &ValidationError{Field: "cursor", Code: "invalid_format"}
	if len(cursor) < 3 || cursor[:2] != "d1" {
		return "", "", invalid
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(cursor[2:])
	if err != nil || len(b) < 37 || !ids.IsCanonicalUUID(string(b[:36])) {
		return "", "", invalid
	}
	login := string(b[36:])
	if !utf8.ValidString(login) || utf8.RuneCountInString(login) > 64 {
		return "", "", invalid
	}
	return login, string(b[:36]), nil
}
