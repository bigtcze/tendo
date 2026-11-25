package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpIdempotencyPrefix marks a service idempotency key derived for an MCP grant.
// It is versioned so the derivation can change without colliding with old keys.
const mcpIdempotencyPrefix = "mcp1."

// MCPIdempotencyKey derives the service idempotency key for an MCP tool call
// from the client's key (#457 slice B3). The client key follows the HTTP
// Idempotency-Key rule (1 to 128 bytes of visible ASCII, case-sensitive). The
// derived key is a digest of the verified grant and the client key, so keys of
// one grant never collide with another grant of the same user, another user or
// a browser request, even on one item. A retry under the same grant (refresh
// rotation keeps the grant) replays; a new grant does not. It is fail closed
// exactly like MCPUserID: ok is false without a verified principal holding
// scope. The grant id never leaves this helper.
func MCPIdempotencyKey(req *mcp.CallToolRequest, scope, clientKey string) (key string, ok bool) {
	if !httpx.ValidIdempotencyKey(clientKey) {
		return "", false
	}
	if _, ok := MCPUserID(req, scope); !ok {
		return "", false
	}
	grant, _ := req.GetExtra().TokenInfo.Extra["grant_id"].(string)
	sum := sha256.Sum256([]byte("tendo-mcp-idempotency\x00" + strings.ToLower(grant) + "\x00" + clientKey))
	return mcpIdempotencyPrefix + base64.RawURLEncoding.EncodeToString(sum[:]), true
}
