package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/ids"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpTestNegotiated is the revision a client carries on every post-initialize
// request after the SDK's initialize settles on 2025-11-25.
const mcpTestNegotiated = "2025-11-25"

const (
	testMCPIssuer = "https://tendo.test"
	testMCPUser   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	testMCPToken  = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

// fakeMCPVerifier answers VerifyAccessToken from a fixed table.
type fakeMCPVerifier struct {
	enabled bool
	access  map[string]identity.MCPAccess
	err     error
	// installationOff and availErr model the B1b2 installation switch.
	installationOff bool
	availErr        error
}

// Available is TENDO_MCP_ENABLED and the installation switch (B1b2).
func (f *fakeMCPVerifier) Available(context.Context) (bool, error) {
	if f.availErr != nil {
		return false, f.availErr
	}
	return f.enabled && !f.installationOff, nil
}
func (f *fakeMCPVerifier) VerifyAccessToken(_ context.Context, token string) (identity.MCPAccess, error) {
	if f.err != nil {
		return identity.MCPAccess{}, f.err
	}
	access, ok := f.access[token]
	if !ok {
		return identity.MCPAccess{}, identity.ErrMCPInvalidToken
	}
	return access, nil
}

// noopTool is a tool whose handler is never reached by these front-check tests.
func noopTool(name, scope string) MCPTool {
	tool := &mcp.Tool{Name: name, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
	return MCPTool{Tool: tool, Scope: scope, Install: func(s *mcp.Server) {
		mcp.AddTool(s, tool, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, struct{}, error) {
			return nil, struct{}{}, nil
		})
	}}
}

func newTestEndpoint(t *testing.T, verifier *fakeMCPVerifier, tools ...MCPTool) *MCPEndpoint {
	t.Helper()
	if len(tools) == 0 {
		tools = []MCPTool{noopTool("list_households", identity.MCPScopeRead)}
	}
	endpoint, err := NewMCPEndpoint(verifier, testMCPIssuer, time.Second, tools)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func readAccess(grant string) identity.MCPAccess {
	return identity.MCPAccess{GrantID: grant, UserID: testMCPUser, ClientID: "client", Scopes: []string{identity.MCPScopeRead}}
}

func serveMCP(e *MCPEndpoint, method, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/mcp", strings.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	return w
}

func TestMCPEndpointDisabledAnswersNotFoundForEveryMethod(t *testing.T) {
	e := newTestEndpoint(t, &fakeMCPVerifier{enabled: false})
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodOptions, http.MethodPut} {
		w := serveMCP(e, method, "", map[string]string{"Authorization": "Bearer " + testMCPToken})
		if w.Code != http.StatusNotFound || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s: status=%d type=%q, want the disabled 404 problem", method, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

func TestMCPEndpointNonPostIsMethodNotAllowedWithAllow(t *testing.T) {
	e := newTestEndpoint(t, &fakeMCPVerifier{enabled: true})
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		w := serveMCP(e, method, "", nil)
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("%s: status=%d allow=%q", method, w.Code, w.Header().Get("Allow"))
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: Cache-Control=%q", method, w.Header().Get("Cache-Control"))
		}
	}
}

// TestMCPEndpointEveryResponseIsNoStore covers the front refusals and every
// response the SDK writes. The SDK sets its own Cache-Control, so the endpoint
// must force no-store on the first write it sees.
func TestMCPEndpointEveryResponseIsNoStore(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g-nostore")}}
	e := newTestEndpoint(t, verifier)
	auth := "Bearer " + testMCPToken
	sdkHeaders := map[string]string{"Authorization": auth, "Accept": "application/json, text/event-stream", "Content-Type": "application/json", "MCP-Protocol-Version": mcpTestNegotiated}
	with := func(overrides map[string]string) map[string]string {
		h := map[string]string{}
		for k, v := range sdkHeaders {
			h[k] = v
		}
		for k, v := range overrides {
			h[k] = v
		}
		return h
	}
	cases := map[string]*httptest.ResponseRecorder{
		"front 401":           serveMCP(e, http.MethodPost, "", nil),
		"front 405":           serveMCP(e, http.MethodGet, "", nil),
		"front batch 400":     serveMCP(e, http.MethodPost, "[]", map[string]string{"Authorization": auth}),
		"tools/call 200":      serveMCP(e, http.MethodPost, mcpEndpointCall(), with(nil)),
		"notification 202":    serveMCP(e, http.MethodPost, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, with(nil)),
		"subscriptions SSE":   serveMCP(e, http.MethodPost, `{"jsonrpc":"2.0","id":7,"method":"subscriptions/listen","params":{"notifications":{"toolsListChanged":true}}}`, with(nil)),
		"sdk 415 bad type":    serveMCP(e, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, with(map[string]string{"Content-Type": "text/plain"})),
		"sdk 400 bad version": serveMCP(e, http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, with(map[string]string{"MCP-Protocol-Version": "1999-01-01"})),
	}
	for name, w := range cases {
		if got := w.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s: status=%d Cache-Control=%q want no-store", name, w.Code, got)
		}
	}
}

// mcpEndpointCall is a tools/call for the fixture tool. The caller adds the
// protocol header; without it the front refuses the message.
func mcpEndpointCall() string {
	return `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_households","arguments":{}}}`
}

func TestMCPEndpointMissingBearerIsUnauthorizedWithChallenge(t *testing.T) {
	e := newTestEndpoint(t, &fakeMCPVerifier{enabled: true})
	w := serveMCP(e, http.MethodPost, "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
	want := `Bearer resource_metadata="https://tendo.test/.well-known/oauth-protected-resource/mcp", scope="tendo:read tendo:write"`
	if got := w.Header().Get("WWW-Authenticate"); got != want {
		t.Fatalf("WWW-Authenticate=%q want %q", got, want)
	}
}

func TestMCPEndpointMalformedAuthorizationIsInvalidToken(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g1")}}
	e := newTestEndpoint(t, verifier)
	cases := map[string][]string{
		"duplicate headers": {"Bearer " + testMCPToken, "Bearer " + testMCPToken},
		"basic scheme":      {"Basic " + testMCPToken},
		"two tokens":        {"Bearer " + testMCPToken + " extra"},
		"empty token":       {"Bearer"},
		"unknown token":     {"Bearer BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"},
	}
	for name, values := range cases {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		for _, v := range values {
			r.Header.Add("Authorization", v)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
			t.Fatalf("%s: status=%d challenge=%q", name, w.Code, w.Header().Get("WWW-Authenticate"))
		}
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Error != "invalid_token" {
			t.Fatalf("%s: body=%s", name, w.Body)
		}
	}
}

func TestMCPEndpointIgnoresCookieAndQueryTokens(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g1")}}
	e := newTestEndpoint(t, verifier)
	r := httptest.NewRequest(http.MethodPost, "/mcp?access_token="+testMCPToken, nil)
	r.AddCookie(&http.Cookie{Name: "tendo_session", Value: testMCPToken})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("cookie or query token authenticated: status=%d", w.Code)
	}
}

func TestMCPEndpointDatabaseFaultIsOAuthUnavailable(t *testing.T) {
	e := newTestEndpoint(t, &fakeMCPVerifier{enabled: true, err: errors.New("connect to 10.0.0.9 password=secret")})
	w := serveMCP(e, http.MethodPost, "", map[string]string{"Authorization": "Bearer " + testMCPToken})
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "10.0.0.9") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), `"error":"temporarily_unavailable"`) {
		t.Fatalf("body=%s", w.Body)
	}
}

func TestMCPEndpointRefusesBatchWithinBodyLimit(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g1")}}
	e := newTestEndpoint(t, verifier)
	for _, body := range []string{"[]", "  \n[{\"jsonrpc\":\"2.0\"}]"} {
		w := serveMCP(e, http.MethodPost, body, map[string]string{"Authorization": "Bearer " + testMCPToken})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "batch requests are not supported") {
			t.Fatalf("batch %q: status=%d body=%s", body, w.Code, w.Body)
		}
	}
}

// TestMCPEndpointRefusesMissingOrLegacyProtocolHeader pins the header rule. A
// post-initialize request without MCP-Protocol-Version would fall back to the
// SDK's 2025-03-26 revision, which allows batching; the front refuses it.
func TestMCPEndpointRefusesMissingOrLegacyProtocolHeader(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g-proto")}}
	e := newTestEndpoint(t, verifier)
	base := map[string]string{"Authorization": "Bearer " + testMCPToken, "Accept": "application/json, text/event-stream", "Content-Type": "application/json"}
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	withVersion := func(v string) map[string]string {
		h := map[string]string{}
		for k, val := range base {
			h[k] = val
		}
		if v != "" {
			h["MCP-Protocol-Version"] = v
		}
		return h
	}
	cases := map[string]map[string]string{
		"header absent": withVersion(""),
		"2025-03-26":    withVersion("2025-03-26"),
		"2024-11-05":    withVersion("2024-11-05"),
	}
	for name, headers := range cases {
		w := serveMCP(e, http.MethodPost, list, headers)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d body=%s; want 400", name, w.Code, w.Body)
		}
		if name == "header absent" && !strings.Contains(w.Body.String(), "MCP-Protocol-Version header required") {
			t.Fatalf("header absent: body=%s; want the required-header error", w.Body)
		}
	}
	// The front check must read the method exactly as the SDK does. encoding/json
	// matches keys case-insensitively and lets the last duplicate win, so these
	// bodies would look like initialize to it while the SDK runs tools/list.
	for name, body := range map[string]string{
		"capitalised key":   `{"jsonrpc":"2.0","id":3,"method":"tools/list","Method":"initialize"}`,
		"upper-case key":    `{"jsonrpc":"2.0","id":3,"method":"tools/list","METHOD":"initialize"}`,
		"tools/call hidden": `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_households","arguments":{}},"Method":"initialize"}`,
	} {
		w := serveMCP(e, http.MethodPost, body, withVersion(""))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "MCP-Protocol-Version header required") {
			t.Fatalf("%s without header: status=%d body=%s; want the required-header 400", name, w.Code, w.Body)
		}
	}
	// The pinned revision passes the header check and reaches the SDK.
	if w := serveMCP(e, http.MethodPost, list, withVersion("2025-11-25")); w.Code != http.StatusOK {
		t.Fatalf("pinned 2025-11-25 tools/list: status=%d body=%s", w.Code, w.Body)
	}
}

func TestMCPEndpointRejectsOversizedBody(t *testing.T) {
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: readAccess("g1")}}
	e := newTestEndpoint(t, verifier)
	big := strings.Repeat("a", mcpEndpointBodyLimit+1)
	w := serveMCP(e, http.MethodPost, big, map[string]string{"Authorization": "Bearer " + testMCPToken})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", w.Code)
	}
}

func TestMCPEndpointPerIPLimitReturnsRetryAfter(t *testing.T) {
	e := newTestEndpoint(t, &fakeMCPVerifier{enabled: true})
	var last *httptest.ResponseRecorder
	for range mcpEndpointIPPerMinute + 1 {
		last = serveMCP(e, http.MethodPost, "", nil)
	}
	if last.Code != http.StatusTooManyRequests || last.Header().Get("Retry-After") != "60" {
		t.Fatalf("status=%d retry=%q", last.Code, last.Header().Get("Retry-After"))
	}
}

func TestMCPEndpointPerGrantLimitIsSeparateFromPerIP(t *testing.T) {
	access := readAccess("g-limited")
	verifier := &fakeMCPVerifier{enabled: true, access: map[string]identity.MCPAccess{testMCPToken: access}}
	e := newTestEndpoint(t, verifier)
	var last *httptest.ResponseRecorder
	for range mcpEndpointGrantPerMin + 1 {
		last = serveMCP(e, http.MethodPost, `{}`, map[string]string{"Authorization": "Bearer " + testMCPToken})
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("grant limit not enforced: status=%d", last.Code)
	}
}

func TestMCPBearerHelperIsCaseInsensitiveAndExact(t *testing.T) {
	cases := []struct {
		values  []string
		present bool
		ok      bool
		token   string
	}{
		{nil, false, false, ""},
		{[]string{"bearer " + testMCPToken}, true, true, testMCPToken},
		{[]string{"BEARER " + testMCPToken}, true, true, testMCPToken},
		{[]string{"Bearer"}, true, false, ""},
		{[]string{"Bearer a b"}, true, false, ""},
		{[]string{"Bearer a", "Bearer a"}, true, false, ""},
	}
	for _, tc := range cases {
		token, present, ok := bearerFromHeader(tc.values)
		if present != tc.present || ok != tc.ok || token != tc.token {
			t.Fatalf("%q: token=%q present=%v ok=%v", tc.values, token, present, ok)
		}
	}
}

func TestMCPUserIDIsFailClosed(t *testing.T) {
	read := identity.MCPScopeRead
	cases := map[string]struct {
		req   *mcp.CallToolRequest
		scope string
		ok    bool
	}{
		"no token":                 {&mcp.CallToolRequest{}, read, false},
		"nil request":              {nil, read, false},
		"empty scope argument":     {mcpPrincipalRequest(testMCPUser, "g1", read), "", false},
		"non-canonical user":       {mcpPrincipalRequest("not-a-uuid", "g1", read), read, false},
		"braced user":              {mcpPrincipalRequest("{"+testMCPUser+"}", "g1", read), read, false},
		"missing grant_id":         {withoutGrant(mcpPrincipalRequest(testMCPUser, "g1", read)), read, false},
		"empty grant_id":           {mcpPrincipalRequest(testMCPUser, "", read), read, false},
		"wrong-typed grant_id":     {withGrant(mcpPrincipalRequest(testMCPUser, "g1", read), 42), read, false},
		"missing scope":            {mcpPrincipalRequest(testMCPUser, "g1"), read, false},
		"write-only holds no read": {mcpPrincipalRequest(testMCPUser, "g1", identity.MCPScopeWrite), read, false},
		"read scope and principal": {mcpPrincipalRequest(testMCPUser, "g1", read), read, true},
	}
	for name, tc := range cases {
		got, ok := MCPUserID(tc.req, tc.scope)
		if ok != tc.ok {
			t.Fatalf("%s: ok=%v want %v", name, ok, tc.ok)
		}
		if ok && got != testMCPUser {
			t.Fatalf("%s: user=%q want %q", name, got, testMCPUser)
		}
	}
	if !ids.IsCanonicalUUID(testMCPUser) {
		t.Fatal("fixture user must be a canonical UUID")
	}
}

func withoutGrant(req *mcp.CallToolRequest) *mcp.CallToolRequest {
	req.Extra.TokenInfo.Extra = map[string]any{}
	return req
}

func withGrant(req *mcp.CallToolRequest, grant any) *mcp.CallToolRequest {
	req.Extra.TokenInfo.Extra = map[string]any{"grant_id": grant}
	return req
}

func TestMCPBuildRejectsMalformedTools(t *testing.T) {
	cases := map[string]MCPTool{
		"no scope":      {Tool: &mcp.Tool{Name: "t"}, Install: func(*mcp.Server) {}},
		"unknown scope": {Tool: &mcp.Tool{Name: "t"}, Scope: "tendo:admin", Install: func(*mcp.Server) {}},
		"no install":    {Tool: &mcp.Tool{Name: "t"}, Scope: identity.MCPScopeRead},
	}
	for name, tool := range cases {
		if _, _, err := buildMCPServer([]MCPTool{tool}); err == nil {
			t.Fatalf("%s: build must fail", name)
		}
	}
}
