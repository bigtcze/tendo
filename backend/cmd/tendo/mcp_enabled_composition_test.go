package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newEnabledMCPApp builds the production handler with TENDO_MCP_ENABLED on. The
// database is unreachable, so any request that reaches token verification fails
// closed with 503 and no driver text.
func newEnabledMCPApp(t *testing.T) (http.Handler, config.Config) {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), "postgres://tendo@127.0.0.1:1/tendo?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cfg := config.Config{
		PublicURL:        "http://127.0.0.1",
		SetupToken:       strings.Repeat("A", 43) + "=",
		OIDCIssuer:       "http://127.0.0.1:1",
		OIDCClientID:     "client",
		OIDCClientSecret: "secret",
		OIDCDisplayName:  "Contract IdP",
		MCPEnabled:       true,
	}
	app, err := productionApp(cfg, pool, fakeSPA, make(chan struct{}))
	if err != nil {
		t.Fatal(err)
	}
	return app, cfg
}

// serveMCPRequest sends one request with an explicit Host and optional Origin,
// Authorization and body, through the production handler.
func serveMCPRequest(h http.Handler, method, target, host, origin, auth, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	if host != "" {
		r.Host = host
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const mcpBodyPing = `{"jsonrpc":"2.0","id":1,"method":"ping"}`

// TestEnabledMCPEndpointChallengesWithoutAToken pins the 401 challenge for a
// request with no usable credential: no header, a cookie only, and a token in
// the query string. None is ever read as a bearer token.
func TestEnabledMCPEndpointChallengesWithoutAToken(t *testing.T) {
	app, cfg := newEnabledMCPApp(t)
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	cases := map[string]*httptest.ResponseRecorder{
		"no header": serveMCPRequest(app, http.MethodPost, cfg.PublicURL+"/mcp", "", cfg.PublicURL, "", mcpBodyPing),
	}
	cookie := httptest.NewRequest(http.MethodPost, cfg.PublicURL+"/mcp", strings.NewReader(mcpBodyPing))
	cookie.AddCookie(&http.Cookie{Name: "tendo_session", Value: token})
	cookieW := httptest.NewRecorder()
	app.ServeHTTP(cookieW, cookie)
	cases["cookie only"] = cookieW
	cases["query token"] = serveMCPRequest(app, http.MethodPost, cfg.PublicURL+"/mcp?access_token="+token, "", cfg.PublicURL, "", mcpBodyPing)

	for name, w := range cases {
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status=%d body=%s; want 401", name, w.Code, w.Body)
		}
		want := `resource_metadata="` + cfg.PublicURL + `/.well-known/oauth-protected-resource/mcp"`
		if !strings.Contains(w.Header().Get("WWW-Authenticate"), want) {
			t.Fatalf("%s: WWW-Authenticate=%q", name, w.Header().Get("WWW-Authenticate"))
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: Cache-Control=%q", name, w.Header().Get("Cache-Control"))
		}
	}
}

// TestEnabledMCPEndpointVerifiesWithoutLeakingDriverText pins that a well-formed
// bearer token reaches verification, and that the unreachable database yields the
// OAuth 503 with no driver text.
func TestEnabledMCPEndpointVerifiesWithoutLeakingDriverText(t *testing.T) {
	app, cfg := newEnabledMCPApp(t)
	const token = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	w := serveMCPRequest(app, http.MethodPost, cfg.PublicURL+"/mcp", "", cfg.PublicURL, "Bearer "+token, mcpBodyPing)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"error":"temporarily_unavailable"`) {
		t.Fatalf("status=%d body=%s; want the OAuth 503", w.Code, w.Body)
	}
	if strings.Contains(strings.ToLower(w.Body.String()), "connect") || strings.Contains(w.Body.String(), "127.0.0.1") {
		t.Fatalf("driver text leaked: %s", w.Body)
	}
}

// TestEnabledMCPEndpointRefusesBrowserOriginsAndWrongHost pins the origin policy
// for POST /mcp: an absent Origin passes, a foreign, null or duplicated Origin
// is refused with 403, and a wrong Host is refused with 421 before routing.
func TestEnabledMCPEndpointRefusesBrowserOriginsAndWrongHost(t *testing.T) {
	app, cfg := newEnabledMCPApp(t)
	target := cfg.PublicURL + "/mcp"
	for _, origin := range []string{"https://foreign.example", "null"} {
		if w := serveMCPRequest(app, http.MethodPost, target, "", origin, "", mcpBodyPing); w.Code != http.StatusForbidden {
			t.Fatalf("Origin %q: status=%d want 403", origin, w.Code)
		}
	}
	if w := serveMCPRequest(app, http.MethodPost, target, "tendo.lan", cfg.PublicURL, "", mcpBodyPing); w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("wrong Host: status=%d want 421", w.Code)
	}
}

// TestEnabledMCPEndpointDuplicateOriginIsRefusedAndPublicOriginPasses sends two
// Origin headers, both public. A duplicated Origin is ambiguous and must be a
// 403. A single public Origin passes the origin check and reaches the auth
// front (401 without a token).
func TestEnabledMCPEndpointDuplicateOriginIsRefusedAndPublicOriginPasses(t *testing.T) {
	app, cfg := newEnabledMCPApp(t)
	target := cfg.PublicURL + "/mcp"

	dup := httptest.NewRequest(http.MethodPost, target, strings.NewReader(mcpBodyPing))
	dup.Header.Add("Origin", cfg.PublicURL)
	dup.Header.Add("Origin", cfg.PublicURL)
	dup.Header.Set("Content-Type", "application/json")
	dupW := httptest.NewRecorder()
	app.ServeHTTP(dupW, dup)
	if dupW.Code != http.StatusForbidden {
		t.Fatalf("duplicated public Origin: status=%d body=%s; want 403", dupW.Code, dupW.Body)
	}

	single := serveMCPRequest(app, http.MethodPost, target, "", cfg.PublicURL, "", mcpBodyPing)
	if single.Code != http.StatusUnauthorized {
		t.Fatalf("single public Origin: status=%d body=%s; want 401 from the auth front", single.Code, single.Body)
	}
}

// TestEnabledMCPEndpointNonPostIsNotAllowed pins GET and DELETE with the public
// Origin: 405 with Allow: POST, never the SPA and never a 404.
func TestEnabledMCPEndpointNonPostIsNotAllowed(t *testing.T) {
	app, cfg := newEnabledMCPApp(t)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		w := serveMCPRequest(app, method, cfg.PublicURL+"/mcp", "", cfg.PublicURL, "", "")
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost {
			t.Fatalf("%s: status=%d allow=%q", method, w.Code, w.Header().Get("Allow"))
		}
		if strings.Contains(w.Body.String(), "spa-fallback") {
			t.Fatalf("%s answered the SPA", method)
		}
	}
}

// TestEnabledMCPEndpointAuthenticatesBeforeBatchRefusal pins the order: a batch
// sent without a token answers 401, so the batch refusal (400) is reached only
// by an authenticated request and never reveals the endpoint to an anonymous
// caller.
func TestEnabledMCPEndpointAuthenticatesBeforeBatchRefusal(t *testing.T) {
	app, cfg := newEnabledMCPApp(t)
	w := serveMCPRequest(app, http.MethodPost, cfg.PublicURL+"/mcp", "", cfg.PublicURL, "", "[]")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("batch without token: status=%d want 401 before batch refusal", w.Code)
	}
}

// TestEnabledMCPEndpointUnsafeMethodsWithoutOriginAreForbidden pins that a
// state-changing method on /mcp without any Origin is refused by the browser
// Origin check. Only POST /mcp is exempt for native clients that send no Origin.
func TestEnabledMCPEndpointUnsafeMethodsWithoutOriginAreForbidden(t *testing.T) {
	app, cfg := newEnabledMCPApp(t)
	for _, method := range []string{http.MethodDelete, http.MethodPut} {
		if w := serveMCPRequest(app, method, cfg.PublicURL+"/mcp", "", "", "", ""); w.Code != http.StatusForbidden {
			t.Fatalf("%s without Origin: status=%d want 403", method, w.Code)
		}
	}
}
