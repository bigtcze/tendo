package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMCPToolCannotReadCredentialHeaders registers a tool that records the
// request headers the SDK hands it. The bearer token, Cookie and
// Proxy-Authorization must all be gone before the SDK handler runs.
func TestMCPToolCannotReadCredentialHeaders(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g-headers")}}
	var seen http.Header
	tool := &mcp.Tool{Name: "list_households", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
	e := newTestEndpoint(t, verifier, MCPTool{Tool: tool, Scope: identity.MCPScopeRead, Install: func(s *mcp.Server) {
		mcp.AddTool(s, tool, func(_ context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, struct{}, error) {
			if req != nil && req.Extra != nil {
				seen = req.Extra.Header.Clone()
			}
			return nil, struct{}{}, nil
		})
	}})
	w := serveMCP(e, http.MethodPost, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_households","arguments":{}}}`, map[string]string{
		"Authorization":        "Bearer " + testMCPToken,
		"Proxy-Authorization":  "Basic c2VjcmV0",
		"Cookie":               "tendo_session=sessionsecret",
		"Accept":               "application/json, text/event-stream",
		"Content-Type":         "application/json",
		"MCP-Protocol-Version": mcpTestNegotiated,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("tools/call status=%d body=%s", w.Code, w.Body)
	}
	if seen == nil {
		t.Fatal("tool handler did not run")
	}
	for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if v := seen.Values(name); len(v) != 0 {
			t.Fatalf("tool read %s=%q; credentials must be stripped before the SDK", name, v)
		}
	}
}

// TestMCPVerifierDisabledAnswersNotFound covers a service that reports enabled
// but whose verification then says the feature is off (a flag flipped between
// the two calls). The client must see the disabled 404, not a 401 or a 503.
func TestMCPVerifierDisabledAnswersNotFound(t *testing.T) {
	e := newTestEndpoint(t, &fakeMCPVerifier{enabled: true, err: identity.ErrMCPDisabled})
	w := serveMCP(e, http.MethodPost, "", map[string]string{"Authorization": "Bearer " + testMCPToken})
	if w.Code != http.StatusNotFound || w.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status=%d type=%q body=%s; want the disabled 404 problem", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
}

// TestMCPOversizedBodyWithoutTokenIsUnauthorized proves authentication runs
// before the body is read: an oversized body with no token is a 401, never a
// 413 that reveals the body limit to an unauthenticated caller.
func TestMCPOversizedBodyWithoutTokenIsUnauthorized(t *testing.T) {
	e := newTestEndpoint(t, &fakeMCPVerifier{enabled: true})
	big := strings.Repeat("a", mcpEndpointBodyLimit+1)
	w := serveMCP(e, http.MethodPost, big, nil)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("oversized body without token: status=%d; want 401", w.Code)
	}
}

// TestMCPSubscriptionsListenAnswersPromptly sends subscriptions/listen. The SDK
// frames a finite answer as server-sent events (an acknowledgment and a result,
// then the stream ends). The request must complete within 2s, so it never holds
// a connection open, and the result for id 7 must be present.
func TestMCPSubscriptionsListenAnswersPromptly(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g-listen")}}
	e := newTestEndpoint(t, verifier)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- serveMCP(e, http.MethodPost,
			`{"jsonrpc":"2.0","id":7,"method":"subscriptions/listen","params":{"notifications":{"toolsListChanged":true}}}`,
			map[string]string{
				"Authorization":        "Bearer " + testMCPToken,
				"Accept":               "application/json, text/event-stream",
				"Content-Type":         "application/json",
				"MCP-Protocol-Version": mcpTestNegotiated,
			})
	}()
	select {
	case w := <-done:
		if w.Code != http.StatusOK {
			t.Fatalf("subscriptions/listen status=%d body=%s", w.Code, w.Body)
		}
		if !strings.Contains(w.Body.String(), `"id":7,"result"`) {
			t.Fatalf("subscriptions/listen gave no result for id 7: %s", w.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriptions/listen did not answer within 2s")
	}
}

// TestMCPStripsClientSessionHeaderBeforeTheSDK records the request the SDK
// receives. A client-chosen Mcp-Session-Id must be gone by then, so the SDK
// can never bind the request to a session the client named.
func TestMCPStripsClientSessionHeaderBeforeTheSDK(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g-session")}}
	e := newTestEndpoint(t, verifier)
	inner := e.guard
	var seen []string
	var present bool
	e.guard = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Values("Mcp-Session-Id")
		_, present = r.Header["Mcp-Session-Id"]
		inner.ServeHTTP(w, r)
	})
	w := serveMCP(e, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, map[string]string{
		"Authorization":        "Bearer " + testMCPToken,
		"Mcp-Session-Id":       "client-chosen",
		"Accept":               "application/json, text/event-stream",
		"Content-Type":         "application/json",
		"MCP-Protocol-Version": mcpTestNegotiated,
	})
	if present || len(seen) != 0 {
		t.Fatalf("SDK received Mcp-Session-Id=%q; the endpoint must strip it", seen)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("tools/list status=%d body=%s", w.Code, w.Body)
	}
	if got := w.Header().Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("response echoed a session id: %q", got)
	}
}
