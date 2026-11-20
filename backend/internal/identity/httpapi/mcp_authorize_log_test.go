package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

// TestMCPRequestLogsCarryNoSecrets runs the consent and token requests through
// the real request logger and asserts that no handle, binding, code, token or
// query value reaches the log. Only route templates may appear.
func TestMCPRequestLogsCarryNoSecrets(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	flow := &fakeMCPFlow{
		fakeMCPService: fakeMCPService{enabled: true},
		begin:          identity.MCPPendingAuthorization{Handle: testHandle, BrowserBinding: testBinding},
		decision:       identity.MCPDecisionResult{RedirectURI: registeredRedirect, Code: "SECRET-CODE", State: "s"},
		tokens:         identity.MCPTokens{AccessToken: "SECRET-ACCESS", RefreshToken: "SECRET-REFRESH", Scopes: []string{"tendo:read"}, AccessExpiresAt: time.Now().Add(time.Hour)},
	}
	routes := chi.NewRouter()
	sessions := signedIn()
	NewMCP(flow, NewSession(sessions, mcpPublicURL), mcpPublicURL).Register(routes)
	app := httpx.NewAppWithLogger(nil, time.Second, make(chan struct{}), httpx.OriginPolicy{PublicURL: mcpPublicURL, CrossOrigin: MCPCrossOriginRoutes()}, func(r chi.Router) { r.Mount("/", routes) }, logger)

	mcpServe(app, http.MethodGet, authorizeTarget("&state=SECRET-STATE"), "", nil, "192.0.2.90:1")
	mcpServe(app, http.MethodGet, "/api/v1/mcp/authorization-requests/"+testHandle, "", http.Header{"Cookie": {sessionCookieHeader + "; " + bindingCookieName + "=" + testBinding}}, "192.0.2.90:1")
	mcpServe(app, http.MethodPost, decisionPath(), `{"decision":"approve"}`, decisionHeader(), "192.0.2.90:1")
	mcpServe(app, http.MethodPost, "/oauth/token", validCodeForm(), formHeader(formHeaderValue), "192.0.2.90:1")
	mcpServe(app, http.MethodPost, "/oauth/revoke", "token=SECRET-REFRESH&client_id=client-1", formHeader(formHeaderValue), "192.0.2.90:1")

	out := logs.String()
	if out == "" {
		t.Fatal("no request log was written")
	}
	for _, secret := range []string{testHandle, testBinding, "SECRET-CODE", "SECRET-ACCESS", "SECRET-REFRESH", "SECRET-STATE", fakeToken, "client-1", "code_verifier"} {
		if strings.Contains(out, secret) {
			t.Fatalf("log leaks %q:\n%s", secret, out)
		}
	}
	for _, route := range []string{"/oauth/authorize", "/api/v1/mcp/authorization-requests/{requestHandle}", "/oauth/token", "/oauth/revoke"} {
		if !strings.Contains(out, "route="+route) {
			t.Errorf("log lacks route template %q:\n%s", route, out)
		}
	}
}
