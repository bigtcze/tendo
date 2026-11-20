package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// rpc posts one JSON-RPC message to the endpoint with the bearer token.
func rpc(e *MCPEndpoint, token, body string) *struct {
	Code    int
	Body    map[string]any
	Session string
} {
	w := serveMCP(e, http.MethodPost, body, map[string]string{
		"Authorization":        "Bearer " + token,
		"Accept":               "application/json, text/event-stream",
		"Content-Type":         "application/json",
		"MCP-Protocol-Version": mcpTestNegotiated,
	})
	out := &struct {
		Code    int
		Body    map[string]any
		Session string
	}{Code: w.Code, Session: w.Header().Get("Mcp-Session-Id")}
	_ = json.Unmarshal(w.Body.Bytes(), &out.Body)
	return out
}

func TestMCPSDKListsOnlyTheRegisteredToolAndCallsIt(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g1")}}
	called := false
	tool := &mcp.Tool{Name: "list_households", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
	e := newTestEndpoint(t, verifier, MCPTool{Tool: tool, Scope: identity.MCPScopeRead, Install: func(s *mcp.Server) {
		mcp.AddTool(s, tool, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
			called = true
			if _, ok := MCPUserID(req, identity.MCPScopeRead); !ok {
				t.Error("tool handler saw no principal")
			}
			return nil, struct{}{}, nil
		})
	}})

	// The SDK's initialize path negotiates only the legacy revisions (it caps at
	// 2025-11-25). A client asking for 2026-07-28 through initialize therefore
	// gets 2025-11-25, which is the SDK's documented fallback, not an error.
	for _, version := range mcpProtocolVersions {
		want := version
		if version == "2026-07-28" {
			want = "2025-11-25"
		}
		init := rpc(e, testMCPToken, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+version+`","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
		if init.Code != http.StatusOK {
			t.Fatalf("%s initialize: status=%d body=%v", version, init.Code, init.Body)
		}
		result, _ := init.Body["result"].(map[string]any)
		if result["protocolVersion"] != want {
			t.Fatalf("requested %s negotiated %v, want %s", version, result["protocolVersion"], want)
		}
		caps, _ := result["capabilities"].(map[string]any)
		if _, logging := caps["logging"]; logging {
			t.Fatalf("%s: logging capability advertised", version)
		}
		// The SDK marks listChanged omitempty, so false is serialized as an
		// absent key. The server must never advertise true.
		tools, _ := caps["tools"].(map[string]any)
		if listChanged, present := tools["listChanged"]; present && listChanged != false {
			t.Fatalf("%s: capabilities.tools.listChanged=%v, want false or absent", version, listChanged)
		}
	}

	list := rpc(e, testMCPToken, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools, _ := list.Body["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "list_households" {
		t.Fatalf("tools/list=%v, want exactly list_households", list.Body)
	}

	call := rpc(e, testMCPToken, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_households","arguments":{}}}`)
	if call.Code != http.StatusOK || !called {
		t.Fatalf("tools/call status=%d called=%v body=%v", call.Code, called, call.Body)
	}
}

func TestMCPSDKRefusesToolCallWithoutItsScope(t *testing.T) {
	// The token holds only tendo:write. The read tool must be refused by the
	// scope gate without its handler running; the write tool is admitted.
	writeOnly := identity.MCPAccess{GrantID: "g1", UserID: testMCPUser, Scopes: []string{identity.MCPScopeWrite}}
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: writeOnly}}
	readRan := false
	readTool := &mcp.Tool{Name: "list_households", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
	writeTool := &mcp.Tool{Name: "rename_household"}
	e := newTestEndpoint(t, verifier,
		MCPTool{Tool: readTool, Scope: identity.MCPScopeRead, Install: func(s *mcp.Server) {
			mcp.AddTool(s, readTool, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
				readRan = true
				return nil, struct{}{}, nil
			})
		}},
		MCPTool{Tool: writeTool, Scope: identity.MCPScopeWrite, Install: func(s *mcp.Server) {
			mcp.AddTool(s, writeTool, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
				return nil, struct{}{}, nil
			})
		}},
	)
	refused := rpc(e, testMCPToken, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_households","arguments":{}}}`)
	if readRan {
		t.Fatal("tendo:read tool ran for a tendo:write-only token")
	}
	result, _ := refused.Body["result"].(map[string]any)
	if result["isError"] != true {
		t.Fatalf("refusal must be a tool error result: %v", refused.Body)
	}
	if !strings.Contains(string(mustJSON(t, refused.Body)), "does not grant the scope") {
		t.Fatalf("refusal text=%v", refused.Body)
	}
	admitted := rpc(e, testMCPToken, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"rename_household","arguments":{}}}`)
	if admitted.Code != http.StatusOK {
		t.Fatalf("write tool for a write token: status=%d body=%v", admitted.Code, admitted.Body)
	}
}

// TestMCPPerUserLimitIsSeparateFromPerGrant gives one user many grants, so only the
// per-user bucket can trip; each grant stays under its own limit.
func TestMCPPerUserLimitIsSeparateFromPerGrant(t *testing.T) {
	access := map[string]identity.MCPAccess{}
	tokens := make([]string, 0, mcpEndpointUserPerMin+1)
	for i := range mcpEndpointUserPerMin + 1 {
		token := fmt.Sprintf("user-token-%04d", i)
		tokens = append(tokens, token)
		access[token] = identity.MCPAccess{GrantID: fmt.Sprintf("grant-%04d", i), UserID: testMCPUser, Scopes: []string{identity.MCPScopeRead}}
	}
	e := newTestEndpoint(t, &fakeMCPVerifier{enabled: true, access: access})
	var last *httptest.ResponseRecorder
	for i, token := range tokens {
		last = serveMCP(e, http.MethodPost, `{}`, map[string]string{"Authorization": "Bearer " + token})
		if i < mcpEndpointUserPerMin && last.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d limited before the per-user bound", i+1)
		}
	}
	if last.Code != http.StatusTooManyRequests || last.Header().Get("Retry-After") != "60" {
		t.Fatalf("per-user limit not enforced: status=%d retry=%q", last.Code, last.Header().Get("Retry-After"))
	}
}

// TestMCPSDKToolCallAcrossPinnedVersions calls the tool under each pinned revision
// on the same stateless endpoint, sending the protocol-version header that the
// client would send after negotiation.
func TestMCPSDKToolCallAcrossPinnedVersions(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g-version")}}
	tool := &mcp.Tool{Name: "list_households", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
	calls := 0
	e := newTestEndpoint(t, verifier, MCPTool{Tool: tool, Scope: identity.MCPScopeRead, Install: func(s *mcp.Server) {
		mcp.AddTool(s, tool, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			calls++
			return nil, struct{}{}, nil
		})
	}})
	for _, version := range mcpProtocolVersions {
		// 2026-07-28 is stateless: each request carries its version in _meta.
		body := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"list_households","arguments":{},"_meta":{` +
			`"io.modelcontextprotocol/protocolVersion":"` + version + `",` +
			`"io.modelcontextprotocol/clientInfo":{"name":"t","version":"1"},` +
			`"io.modelcontextprotocol/clientCapabilities":{}}}}`
		w := serveMCP(e, http.MethodPost, body, map[string]string{
			"Authorization":        "Bearer " + testMCPToken,
			"Accept":               "application/json, text/event-stream",
			"Content-Type":         "application/json",
			"Mcp-Protocol-Version": version,
			"Mcp-Method":           "tools/call",
			"Mcp-Name":             "list_households",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("%s: tools/call status=%d body=%s", version, w.Code, w.Body)
		}
	}
	if calls != len(mcpProtocolVersions) {
		t.Fatalf("tool handler ran %d times across %d versions", calls, len(mcpProtocolVersions))
	}
}

// TestMCPSDKNeverEchoesSessionHeader checks only the response side: the endpoint
// issues no Mcp-Session-Id, even when the client sends one. The request side is
// proven by TestMCPStripsClientSessionHeaderBeforeTheSDK.
func TestMCPSDKNeverEchoesSessionHeader(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g1")}}
	e := newTestEndpoint(t, verifier)
	w := serveMCP(e, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, map[string]string{
		"Authorization":  "Bearer " + testMCPToken,
		"Mcp-Session-Id": "client-chosen",
		"Accept":         "application/json, text/event-stream",
		"Content-Type":   "application/json",
	})
	if w.Header().Get("Mcp-Session-Id") != "" {
		t.Fatalf("stateless endpoint issued a session id: %q", w.Header().Get("Mcp-Session-Id"))
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
