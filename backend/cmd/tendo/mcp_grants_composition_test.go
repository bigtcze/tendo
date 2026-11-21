package main

import (
	"net/http"
	"strings"
	"testing"
)

// Connected apps (ADR 0023, slice A4b) in the production composition. Both
// routes answer whether or not MCP is enabled, and they never answer anonymously
// or without the canonical Origin. The production MCP wiring in these tests is
// the disabled one, which is what the deployed default is.

const mcpGrantCompositionID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"

// TestConnectedAppsRouteWhileMCPDisabled: the routes exist in the disabled
// composition and are not the unknown-path 404. Anonymous calls are 401.
func TestConnectedAppsRouteWhileMCPDisabled(t *testing.T) {
	app, cfg := newProductionTestApp(t)
	unknown := serveAt(app, http.MethodGet, cfg.PublicURL+"/oauth/xyz", cfg.PublicURL)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/mcp/grants"},
		{http.MethodDelete, "/api/v1/mcp/grants/" + mcpGrantCompositionID},
	} {
		w := serveAt(app, tc.method, cfg.PublicURL+tc.path, cfg.PublicURL)
		if w.Code == unknown.Code && w.Body.String() == unknown.Body.String() {
			t.Errorf("%s %s answered the disabled unknown-path 404; connected apps must be routed", tc.method, tc.path)
		}
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s anonymous status = %d, want 401", tc.method, tc.path, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s 401 lacks no-store", tc.method, tc.path)
		}
		if strings.Contains(w.Body.String(), "spa-fallback") {
			t.Errorf("%s %s answered the SPA", tc.method, tc.path)
		}
	}
}

// TestConnectedAppsDeleteRequiresCanonicalOrigin: a DELETE without an Origin, or
// with a foreign Origin, is 403 from the global origin policy before any handler
// or session check runs.
func TestConnectedAppsDeleteRequiresCanonicalOrigin(t *testing.T) {
	app, cfg := newProductionTestApp(t)
	target := cfg.PublicURL + "/api/v1/mcp/grants/" + mcpGrantCompositionID
	for _, origin := range []string{"", "https://evil.example"} {
		w := serveAt(app, http.MethodDelete, target, origin)
		if w.Code != http.StatusForbidden {
			t.Errorf("DELETE with origin %q status = %d, want 403", origin, w.Code)
		}
	}
}

// TestConnectedAppsNoCORSHeaders: the routes send no Access-Control-Allow-Origin,
// so a cross-origin page cannot read the responses even with a cookie.
func TestConnectedAppsNoCORSHeaders(t *testing.T) {
	app, cfg := newProductionTestApp(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/mcp/grants"},
		{http.MethodDelete, "/api/v1/mcp/grants/" + mcpGrantCompositionID},
	} {
		w := serveAt(app, tc.method, cfg.PublicURL+tc.path, cfg.PublicURL)
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s %s sends a CORS allow-origin header", tc.method, tc.path)
		}
	}
}
