package httpapi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/ids"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	mcpServerName    = "tendo"
	mcpServerVersion = "dev"
	// mcpBuildVerifyTimeout bounds the in-memory tools/list check at build time.
	mcpBuildVerifyTimeout = 10 * time.Second
)

// MCPTool is one tool the endpoint serves. Install registers exactly the tool
// described by Tool through mcp.AddTool, so the transport and the feature
// module meet only at this neutral type. Scope is the single scope a caller
// must hold to call the tool.
type MCPTool struct {
	Tool    *mcp.Tool
	Scope   string
	Install func(*mcp.Server)
}

// buildMCPServer builds the one shared MCP server. It validates every tool
// before any is installed, and returns each tool's required scope by name. It
// is the only place tools are registered; the server is never mutated
// afterwards.
func buildMCPServer(tools []MCPTool) (*mcp.Server, map[string]string, error) {
	required := make(map[string]string, len(tools))
	for _, tool := range tools {
		if tool.Tool == nil || tool.Tool.Name == "" || tool.Install == nil {
			return nil, nil, errors.New("mcp tool is incomplete")
		}
		if tool.Scope != identity.MCPScopeRead && tool.Scope != identity.MCPScopeWrite {
			return nil, nil, fmt.Errorf("mcp tool %q has no valid scope", tool.Tool.Name)
		}
		if _, dup := required[tool.Tool.Name]; dup {
			return nil, nil, fmt.Errorf("mcp tool %q is registered twice", tool.Tool.Name)
		}
		required[tool.Tool.Name] = tool.Scope
	}
	server := mcp.NewServer(&mcp.Implementation{Name: mcpServerName, Version: mcpServerVersion}, &mcp.ServerOptions{
		Capabilities:              &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{ListChanged: false}},
		SupportedProtocolVersions: mcpProtocolVersions,
	})
	for _, tool := range tools {
		tool.Install(server)
	}
	if err := verifyListedTools(server, required); err != nil {
		return nil, nil, err
	}
	server.AddReceivingMiddleware(requireToolScope(required))
	return server, required, nil
}

// verifyListedTools connects an in-memory client and lists the server's tools.
// Construction fails unless the listed names equal the declared set exactly, so
// an Install that registers an extra tool can never be served.
func verifyListedTools(server *mcp.Server, required map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), mcpBuildVerifyTimeout)
	defer cancel()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	session, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return errors.New("mcp server could not start for verification")
	}
	defer func() { _ = session.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "tendo-build-check", Version: "1"}, nil)
	peer, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return errors.New("mcp verification client could not connect")
	}
	defer func() { _ = peer.Close() }()
	listed := map[string]bool{}
	for tool, err := range peer.Tools(ctx, nil) {
		if err != nil {
			return errors.New("mcp tools/list failed during verification")
		}
		if listed[tool.Name] {
			return fmt.Errorf("mcp tool %q is listed twice", tool.Name)
		}
		listed[tool.Name] = true
	}
	if len(listed) != len(required) {
		return fmt.Errorf("mcp tools/list has %d tools, declared %d", len(listed), len(required))
	}
	for name := range required {
		if !listed[name] {
			return fmt.Errorf("mcp tool %q is declared but not listed", name)
		}
	}
	return nil
}

// mcpProtocolVersions are the pinned MCP protocol revisions. 2025-03-26 is
// absent: it allows JSON-RPC batching, which this endpoint refuses.
var mcpProtocolVersions = []string{"2026-07-28", "2025-11-25", "2025-06-18"}

// requireToolScope is the one scope gate for tools. It runs before a tool handler
// and refuses a tools/call whose caller does not hold the tool's exact scope.
// The refusal is a tool error result, so the handler never runs. A missing
// principal refuses too (fail closed). Unknown tools go to the SDK, which answers
// its own error.
func requireToolScope(required map[string]string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			// Fail closed: a tools/call whose params cannot be read is refused,
			// never passed through to a handler without a scope decision.
			params, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if !ok || params == nil {
				return refusedCall(), nil
			}
			// Fail closed for a name the build did not declare: only the declared
			// tools are reachable, so an unknown name is refused before the SDK.
			scope, known := required[params.Name]
			if !known {
				return refusedCall(), nil
			}
			if !holdsScope(req.GetExtra(), scope) {
				return refusedCall(), nil
			}
			return next(ctx, method, req)
		}
	}
}

// refusedCall is the tool error result for a call the scope gate refuses. The
// text is fixed and names no tool, scope or principal.
func refusedCall() *mcp.CallToolResult {
	refused := &mcp.CallToolResult{}
	refused.SetError(errors.New("the token does not grant the scope this tool requires"))
	return refused
}

// holdsScope reports whether the verified token carries the exact scope.
// tendo:write never implies tendo:read.
func holdsScope(extra *mcp.RequestExtra, scope string) bool {
	return extra != nil && extra.TokenInfo != nil && slices.Contains(extra.TokenInfo.Scopes, scope)
}

// MCPUserID is the one fail-closed principal helper for MCP tools. It returns the
// canonical user ID only when the request carries a verified token with a
// canonical user, a grant, and the given scope. Tools use it and never read the
// token themselves.
func MCPUserID(req *mcp.CallToolRequest, scope string) (string, bool) {
	if req == nil || scope == "" {
		return "", false
	}
	extra := req.GetExtra()
	if !holdsScope(extra, scope) {
		return "", false
	}
	grant, _ := extra.TokenInfo.Extra["grant_id"].(string)
	userID := extra.TokenInfo.UserID
	if grant == "" || !ids.IsCanonicalUUID(userID) {
		return "", false
	}
	return userID, true
}
