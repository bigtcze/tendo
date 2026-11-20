package httpapi

import (
	"context"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPBuildFailsWhenInstallRegistersAnExtraTool declares one tool but
// installs two. The build must refuse: the listed names differ from the
// declared set, so no server with an undeclared tool is ever served.
func TestMCPBuildFailsWhenInstallRegistersAnExtraTool(t *testing.T) {
	declared := &mcp.Tool{Name: "list_households", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
	tool := MCPTool{Tool: declared, Scope: identity.MCPScopeRead, Install: func(s *mcp.Server) {
		noop := func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, nil
		}
		mcp.AddTool(s, declared, noop)
		mcp.AddTool(s, &mcp.Tool{Name: "smuggled_tool"}, noop)
	}}
	if _, _, err := buildMCPServer([]MCPTool{tool}); err == nil {
		t.Fatal("build accepted an Install that registered an undeclared tool")
	}
}

// TestMCPBuildFailsWhenADeclaredToolIsNotInstalled declares a tool whose Install
// registers nothing. The build must refuse the mismatch.
func TestMCPBuildFailsWhenADeclaredToolIsNotInstalled(t *testing.T) {
	tool := MCPTool{Tool: &mcp.Tool{Name: "ghost"}, Scope: identity.MCPScopeRead, Install: func(*mcp.Server) {}}
	if _, _, err := buildMCPServer([]MCPTool{tool}); err == nil {
		t.Fatal("build accepted a declared tool that was never listed")
	}
}

// principalTokenInfo is a verified token with one scope, for middleware tests.
func principalTokenInfo(scope string) *auth.TokenInfo {
	return &auth.TokenInfo{Scopes: []string{scope}, UserID: testMCPUser, Extra: map[string]any{"grant_id": "g-build"}}
}

// TestMCPScopeGateRefusesUnknownToolName proves the middleware fails closed: an
// unknown tool name is refused without the next handler running.
func TestMCPScopeGateRefusesUnknownToolName(t *testing.T) {
	required := map[string]string{"list_households": identity.MCPScopeRead}
	called := false
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		called = true
		return &mcp.CallToolResult{}, nil
	}
	handler := requireToolScope(required)(next)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "not_declared"},
		Extra:  &mcp.RequestExtra{TokenInfo: principalTokenInfo(identity.MCPScopeRead)},
	}
	res, err := handler(context.Background(), "tools/call", req)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("unknown tool name reached the next handler")
	}
	if result, _ := res.(*mcp.CallToolResult); result == nil || !result.IsError {
		t.Fatalf("unknown tool name must be a tool error result, got %#v", res)
	}
}
