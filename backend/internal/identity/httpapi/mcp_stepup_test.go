package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// Step-up tests (#457 slice B3). The front answers 403 insufficient_scope only
// for a tools/call request naming a registered tool whose scope the policy
// allows and the grant lacks; everything else reaches the scope middleware.

const stepUpResource = `resource_metadata="https://tendo.test/.well-known/oauth-protected-resource/mcp"`

func stepUpEndpoint(t *testing.T, access identity.MCPAccess) *MCPEndpoint {
	t.Helper()
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: access}}
	return newTestEndpoint(t, verifier,
		noopTool("read_tool", identity.MCPScopeRead),
		writeNoopTool("write_tool", identity.MCPScopeWrite))
}

func writeNoopTool(name, scope string) MCPTool {
	tool := noopTool(name, scope)
	tool.Tool.Annotations.ReadOnlyHint = false
	return tool
}

func access(scopes, allowed []string) identity.MCPAccess {
	return identity.MCPAccess{GrantID: "g1", UserID: testMCPUser, ClientID: "client", Scopes: scopes, Allowed: allowed}
}

func postMCP(e *MCPEndpoint, body string) *httptest.ResponseRecorder {
	return serveMCP(e, http.MethodPost, body, map[string]string{
		"Authorization": "Bearer " + testMCPToken, "MCP-Protocol-Version": mcpTestNegotiated,
		"Accept": "application/json, text/event-stream", "Content-Type": "application/json",
	})
}

func callBody(params string) string {
	return `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":` + params + `}`
}

// isToolRefusal reports whether the answer is the scope middleware's tool error.
func isToolRefusal(w *httptest.ResponseRecorder) bool {
	if w.Code != http.StatusOK {
		return false
	}
	var reply struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil || !reply.Result.IsError || len(reply.Result.Content) != 1 {
		return false
	}
	return reply.Result.Content[0].Text == "the token does not grant the scope this tool requires"
}

func TestMCPStepUpNamesTheHeldAndTheMissingAllowedScope(t *testing.T) {
	read, write := identity.MCPScopeRead, identity.MCPScopeWrite
	both := []string{read, write}
	cases := map[string]struct {
		held []string
		tool string
		want string
	}{
		"write-only grant calls a read tool": {[]string{write}, "read_tool", "tendo:read tendo:write"},
		"read-only grant calls a write tool": {[]string{read}, "write_tool", "tendo:read tendo:write"},
	}
	for name, tc := range cases {
		e := stepUpEndpoint(t, access(tc.held, both))
		w := postMCP(e, callBody(`{"name":"`+tc.tool+`","arguments":{}}`))
		want := `Bearer error="insufficient_scope", scope="` + tc.want + `", ` + stepUpResource
		if w.Code != http.StatusForbidden || w.Header().Get("WWW-Authenticate") != want {
			t.Fatalf("%s: status=%d challenge=%q body=%s", name, w.Code, w.Header().Get("WWW-Authenticate"), w.Body)
		}
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if body["error"] != "insufficient_scope" || strings.Contains(w.Body.String(), tc.tool) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: body=%s cache=%q", name, w.Body, w.Header().Get("Cache-Control"))
		}
	}
}

// A scope the policy does not allow is never challenged: the call reaches the
// middleware and gets the fixed tool error result.
func TestMCPStepUpIsNeverSentForAScopeThePolicyRefuses(t *testing.T) {
	read, write := identity.MCPScopeRead, identity.MCPScopeWrite
	cases := map[string]struct {
		held, allowed []string
		tool          string
	}{
		"read-only policy, write tool": {[]string{read}, []string{read}, "write_tool"},
		"write-only policy, read tool": {[]string{write}, []string{write}, "read_tool"},
		"empty allowed (defence)":      {[]string{read}, nil, "write_tool"},
	}
	for name, tc := range cases {
		e := stepUpEndpoint(t, access(tc.held, tc.allowed))
		w := postMCP(e, callBody(`{"name":"`+tc.tool+`","arguments":{}}`))
		if !isToolRefusal(w) {
			t.Fatalf("%s: status=%d body=%s; want the tool error result", name, w.Code, w.Body)
		}
	}
}

// Only an exact tools/call request with an id and a registered "name" is
// challenged. Variants pass through and are never executed without the scope.
func TestMCPStepUpOnlyForAnExactToolsCallRequest(t *testing.T) {
	read, write := identity.MCPScopeRead, identity.MCPScopeWrite
	e := stepUpEndpoint(t, access([]string{read}, []string{read, write}))
	refused := map[string]string{
		"unknown tool":       callBody(`{"name":"nope","arguments":{}}`),
		"name in other case": callBody(`{"Name":"write_tool","arguments":{}}`),
		"name not a string":  callBody(`{"name":7,"arguments":{}}`),
		"params not object":  callBody(`["write_tool"]`),
	}
	for name, body := range refused {
		w := postMCP(e, body)
		if w.Code == http.StatusForbidden {
			t.Fatalf("%s: challenged; want pass-through", name)
		}
		if !isToolRefusal(w) && !isRPCError(w) {
			t.Fatalf("%s: status=%d body=%s; want the scope tool error or a JSON-RPC error, never a run", name, w.Code, w.Body)
		}
	}
	// A duplicated name may be read either way; whichever, the call must never
	// run the write tool without tendo:write.
	dup := postMCP(e, callBody(`{"name":"read_tool","name":"write_tool","arguments":{}}`))
	if dup.Code != http.StatusForbidden && !isToolRefusal(dup) {
		t.Fatalf("duplicate name keys: status=%d body=%s; want a challenge or the tool error", dup.Code, dup.Body)
	}
	notification := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"write_tool","arguments":{}}}`
	if w := postMCP(e, notification); w.Code == http.StatusForbidden {
		t.Fatalf("notification challenged: %d %s", w.Code, w.Body)
	}
	list := `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`
	if w := postMCP(e, list); w.Code != http.StatusOK {
		t.Fatalf("tools/list: %d %s", w.Code, w.Body)
	}
	if w := postMCP(e, callBody(`{"name":"read_tool","arguments":{}}`)); w.Code != http.StatusOK || isToolRefusal(w) {
		t.Fatalf("held scope: %d %s", w.Code, w.Body)
	}
}

// isRPCError reports whether the answer is a JSON-RPC error (no result), as the
// SDK sends for an unknown tool or malformed params.
func isRPCError(w *httptest.ResponseRecorder) bool {
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(w.Body.Bytes(), &reply) != nil {
		return false
	}
	return reply.Error != nil && reply.Error.Code != 0 && len(reply.Result) == 0
}

// The step-up comes after the body checks: an oversize body, a batch or a
// missing MCP-Protocol-Version is refused as such, never challenged.
func TestMCPStepUpComesAfterTheBodyChecks(t *testing.T) {
	e := stepUpEndpoint(t, access([]string{identity.MCPScopeRead}, []string{identity.MCPScopeRead, identity.MCPScopeWrite}))
	call := callBody(`{"name":"write_tool","arguments":{}}`)
	oversize := callBody(`{"name":"write_tool","arguments":{"pad":"` + strings.Repeat("x", mcpEndpointBodyLimit) + `"}}`)
	if w := postMCP(e, oversize); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize: status=%d; want 413", w.Code)
	}
	if w := postMCP(e, "["+call+"]"); w.Code != http.StatusBadRequest {
		t.Fatalf("batch: status=%d; want 400", w.Code)
	}
	w := serveMCP(e, http.MethodPost, call, map[string]string{
		"Authorization": "Bearer " + testMCPToken, "Accept": "application/json, text/event-stream", "Content-Type": "application/json",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no protocol version: status=%d; want 400", w.Code)
	}
}

// stepUpScope names the held scopes the policy still allows plus the missing
// one, sorted, whatever the scope names are. A held scope the policy no longer
// allows is not requested again.
func TestMCPStepUpScopeIsComputedFromGrantAndPolicy(t *testing.T) {
	e := &MCPEndpoint{required: map[string]string{"a_tool": "tendo:alpha", "z_tool": "tendo:zulu"}}
	cases := map[string]struct {
		held, allowed []string
		tool, want    string
		challenge     bool
	}{
		"missing sorts first":        {[]string{"tendo:zulu"}, []string{"tendo:alpha", "tendo:zulu"}, "a_tool", "tendo:alpha tendo:zulu", true},
		"missing sorts last":         {[]string{"tendo:alpha"}, []string{"tendo:alpha", "tendo:zulu"}, "z_tool", "tendo:alpha tendo:zulu", true},
		"held but no longer allowed": {[]string{"tendo:beta"}, []string{"tendo:alpha"}, "a_tool", "tendo:alpha", true},
		"nothing held":               {nil, []string{"tendo:zulu"}, "z_tool", "tendo:zulu", true},
		"held already":               {[]string{"tendo:zulu"}, []string{"tendo:zulu"}, "z_tool", "", false},
		"not allowed":                {[]string{"tendo:alpha"}, []string{"tendo:alpha"}, "z_tool", "", false},
		"unregistered tool":          {nil, []string{"tendo:alpha"}, "b_tool", "", false},
	}
	for name, tc := range cases {
		got, challenge := e.stepUpScope([]byte(callBody(`{"name":"`+tc.tool+`"}`)), access(tc.held, tc.allowed))
		if got != tc.want || challenge != tc.challenge {
			t.Fatalf("%s: scope=%q challenge=%v; want %q %v", name, got, challenge, tc.want, tc.challenge)
		}
	}
}

// The step-up comes after the rate limits: a limited grant is 429, not 403.
func TestMCPStepUpComesAfterTheRateLimits(t *testing.T) {
	e := stepUpEndpoint(t, access([]string{identity.MCPScopeRead}, []string{identity.MCPScopeRead, identity.MCPScopeWrite}))
	body := callBody(`{"name":"write_tool","arguments":{}}`)
	var last *httptest.ResponseRecorder
	for range mcpEndpointGrantPerMin + 1 {
		last = postMCP(e, body)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d; want 429 before the step-up", last.Code)
	}
}

// A grant holding only tendo:write can list tools once write tools exist.
func TestMCPWriteOnlyGrantIsAdmittedWhenWriteToolsExist(t *testing.T) {
	e := stepUpEndpoint(t, access([]string{identity.MCPScopeWrite}, []string{identity.MCPScopeWrite}))
	if w := postMCP(e, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`); w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}
