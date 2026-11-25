package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// MaxMCPCursorLength bounds a bound cursor before it is decoded. The longest
// service cursor (the item list i2 cursor) wraps to well under this.
const MaxMCPCursorLength = 512

const mcpCursorVersion = "m1"

// BindMCPCursor wraps a service cursor so that it continues only the call that
// produced it: the same tool, the same caller and the same arguments. A service
// cursor is not always bound to its household, item or filters, so a tool
// returns only bound cursors. An empty service cursor stays empty (last page).
func BindMCPCursor(inner, tool, userID string, context ...string) string {
	if inner == "" {
		return ""
	}
	raw := mcpCursorVersion + ":" + mcpCursorFingerprint(tool, userID, context) + ":" + inner
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// OpenMCPCursor returns the service cursor inside a bound cursor. It refuses a
// cursor bound to another tool, caller or context, an oversized cursor and any
// malformed one. The check reads no data, so the outcome depends only on the
// arguments and reveals nothing about other households. An empty cursor opens
// to an empty service cursor (first page).
func OpenMCPCursor(outer, tool, userID string, context ...string) (string, bool) {
	if outer == "" {
		return "", true
	}
	if len(outer) > MaxMCPCursorLength {
		return "", false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(outer)
	if err != nil {
		return "", false
	}
	parts := strings.SplitN(string(raw), ":", 3)
	if len(parts) != 3 || parts[0] != mcpCursorVersion || parts[2] == "" ||
		parts[1] != mcpCursorFingerprint(tool, userID, context) {
		return "", false
	}
	return parts[2], true
}

func mcpCursorFingerprint(tool, userID string, context []string) string {
	fields := append([]string{"tendo.mcp.cursor.v1", tool, userID}, context...)
	encoded, _ := json.Marshal(fields)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
