package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// mcpDisabledPaths are every method+path the MCP surface exposes. While MCP is
// disabled in production each one answers the unknown-path 404.
var mcpDisabledPaths = []struct{ method, path string }{
	{http.MethodPost, "/oauth/register"},
	{http.MethodOptions, "/oauth/register"},
	{http.MethodGet, "/.well-known/oauth-protected-resource/mcp"},
	{http.MethodOptions, "/.well-known/oauth-protected-resource/mcp"},
	{http.MethodGet, "/.well-known/oauth-authorization-server"},
	{http.MethodOptions, "/.well-known/oauth-authorization-server"},
	{http.MethodGet, "/mcp"},
	{http.MethodPost, "/mcp"},
	{http.MethodOptions, "/mcp"},
}

// fakeSPA stands in for the bundled web UI. Any SPA fallback is visible as its
// marker in a response body.
var fakeSPA = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte("<!doctype html>spa-fallback"))
})

func newProductionTestApp(t *testing.T) (http.Handler, config.Config) {
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
	}
	app, err := productionApp(cfg, pool, fakeSPA, make(chan struct{}))
	if err != nil {
		t.Fatal(err)
	}
	return app, cfg
}

func serveAt(h http.Handler, method, target, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestProductionCompositionAnswersMCPDisabled404 pins that, with the production
// (disabled) MCP wiring and the UI fallback present, every MCP path and
// preflight answers exactly the unknown-reserved-path 404 problem and never the
// SPA. The request carries the canonical Origin so the origin check passes and
// the response comes from routing alone.
func TestProductionCompositionAnswersMCPDisabled404(t *testing.T) {
	app, cfg := newProductionTestApp(t)
	unknown := serveAt(app, http.MethodGet, cfg.PublicURL+"/oauth/xyz", cfg.PublicURL)
	if unknown.Code != http.StatusNotFound || unknown.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("unknown reserved path status=%d type=%q", unknown.Code, unknown.Header().Get("Content-Type"))
	}
	for _, tc := range mcpDisabledPaths {
		w := serveAt(app, tc.method, cfg.PublicURL+tc.path, cfg.PublicURL)
		if w.Code != unknown.Code || w.Body.String() != unknown.Body.String() || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s %s: status=%d type=%q body=%s; want the unknown-path 404 %s",
				tc.method, tc.path, w.Code, w.Header().Get("Content-Type"), w.Body, unknown.Body)
		}
		if strings.Contains(w.Body.String(), "spa-fallback") {
			t.Errorf("%s %s answered the SPA", tc.method, tc.path)
		}
	}
}

// TestProductionOriginExemptionIsExactlyTheMCPList pins that a foreign Origin is
// skipped only on the three listed MCP endpoints. The listed ones reach routing
// (404 while disabled); an unlisted state-changing request with the same foreign
// Origin is still refused with 403 by the origin check.
func TestProductionOriginExemptionIsExactlyTheMCPList(t *testing.T) {
	app, cfg := newProductionTestApp(t)
	const foreign = "https://foreign.example"
	for _, path := range []string{"/oauth/register", "/.well-known/oauth-authorization-server"} {
		method := http.MethodPost
		if strings.HasPrefix(path, "/.well-known") {
			method = http.MethodGet
		}
		if w := serveAt(app, method, cfg.PublicURL+path, foreign); w.Code != http.StatusNotFound {
			t.Errorf("%s %s with foreign Origin: status=%d, want the exempt 404", method, path, w.Code)
		}
	}
	for _, path := range []string{"/oauth/xyz", "/api/v1/session"} {
		if w := serveAt(app, http.MethodPost, cfg.PublicURL+path, foreign); w.Code != http.StatusForbidden {
			t.Errorf("POST %s with foreign Origin: status=%d, want 403", path, w.Code)
		}
	}
}
