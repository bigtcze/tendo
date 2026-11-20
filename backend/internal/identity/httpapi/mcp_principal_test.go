package httpapi

import (
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpPrincipalRequest builds the call request the SDK produces for a verified
// token with the given user, grant and scopes. It exists so tool tests can state
// a principal without a token: tools still read the principal only through
// MCPUserID, and the architecture test forbids TokenInfo or Extra in tool code.
func mcpPrincipalRequest(userID, grantID string, scopes ...string) *mcp.CallToolRequest {
	return &mcp.CallToolRequest{
		Extra: &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{
			Scopes: scopes,
			UserID: userID,
			Extra:  map[string]any{"grant_id": grantID},
		}},
	}
}
