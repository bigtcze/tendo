package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// TestMCPInstallationAPIRoutesExistWhileMCPDisabled (ADR 0023, slice B1b2): the
// installation policy and the audit list are mounted in the disabled production
// composition. They answer an anonymous caller 401 with no-store and no CORS
// header, and never the unknown-path 404 of the gated MCP surface.
func TestMCPInstallationAPIRoutesExistWhileMCPDisabled(t *testing.T) {
	app, cfg := newProductionTestApp(t)
	unknown := serveAt(app, http.MethodGet, cfg.PublicURL+"/oauth/xyz", cfg.PublicURL)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/mcp-policy"},
		{http.MethodPut, "/api/v1/admin/mcp-policy"},
		{http.MethodGet, "/api/v1/admin/mcp-policy-events"},
	} {
		w := serveAt(app, tc.method, cfg.PublicURL+tc.path, cfg.PublicURL)
		if w.Code == unknown.Code && w.Body.String() == unknown.Body.String() {
			t.Errorf("%s %s answered the disabled unknown-path 404", tc.method, tc.path)
		}
		if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s anonymous status = %d cache = %q, want 401 no-store", tc.method, tc.path, w.Code, w.Header().Get("Cache-Control"))
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("%s %s sends a CORS allow-origin header", tc.method, tc.path)
		}
		if strings.Contains(w.Body.String(), "spa-fallback") {
			t.Errorf("%s %s answered the SPA", tc.method, tc.path)
		}
	}
}

// TestEnabledMCPGateReadsDatabaseSwitchWithoutOverride (ADR 0023, slice B1b2)
// proves the production wiring reads the installation switch from the database.
// No override is passed, the database is unreachable, and MCP is enabled. The
// gate cannot read the switch, so it fails closed: anonymous POST /mcp answers
// the OAuth 503 and a discovery document answers the problem 503. Neither leaks
// driver text, and neither is the installation-off 404 or a 401 challenge.
func TestEnabledMCPGateReadsDatabaseSwitchWithoutOverride(t *testing.T) {
	app, cfg := newEnabledMCPApp(t, nil)

	mcp := serveMCPRequest(app, http.MethodPost, cfg.PublicURL+"/mcp", "", cfg.PublicURL, "", mcpBodyPing)
	if mcp.Code != http.StatusServiceUnavailable || !strings.Contains(mcp.Body.String(), `"error":"temporarily_unavailable"`) {
		t.Fatalf("anonymous POST /mcp: status=%d body=%s; want the OAuth 503 from the database gate", mcp.Code, mcp.Body)
	}

	discovery := serveMCPRequest(app, http.MethodGet, cfg.PublicURL+"/.well-known/oauth-authorization-server", "", cfg.PublicURL, "", "")
	if discovery.Code != http.StatusServiceUnavailable || !strings.Contains(discovery.Body.String(), `"code":"unavailable"`) {
		t.Fatalf("discovery: status=%d body=%s; want the problem 503 from the database gate", discovery.Code, discovery.Body)
	}

	for name, body := range map[string]string{"mcp": mcp.Body.String(), "discovery": discovery.Body.String()} {
		if strings.Contains(body, "127.0.0.1") || strings.Contains(strings.ToLower(body), "connect") {
			t.Fatalf("%s leaks driver text: %s", name, body)
		}
	}
}

// TestInstallationOffAnswersEveryMCPPathAsTheUnknownPath (ADR 0023, slice B1b2)
// pins the installation switch off on an enabled app: every entry of the MCP
// path list answers byte for byte the unknown reserved path's 404. It must match
// in status, body, header set and header values (except the per-request
// X-Request-ID), carry no CORS header, and never answer the SPA. The list already
// covers OPTIONS, DELETE, PUT and PATCH on /mcp.
func TestInstallationOffAnswersEveryMCPPathAsTheUnknownPath(t *testing.T) {
	app, cfg := newEnabledMCPApp(t, fixedInstallationSwitch(false))
	unknown := serveAt(app, http.MethodGet, cfg.PublicURL+"/oauth/xyz", cfg.PublicURL)
	if unknown.Code != http.StatusNotFound || unknown.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("unknown reserved path status=%d type=%q", unknown.Code, unknown.Header().Get("Content-Type"))
	}
	for _, tc := range mcpDisabledPaths {
		w := serveAt(app, tc.method, cfg.PublicURL+tc.path, cfg.PublicURL)
		if w.Code != unknown.Code || w.Body.String() != unknown.Body.String() {
			t.Errorf("%s %s: status=%d body=%s; want the unknown-path 404 %s", tc.method, tc.path, w.Code, w.Body, unknown.Body)
		}
		if got, want := headerSet(w.Header()), headerSet(unknown.Header()); !slices.Equal(got, want) {
			t.Errorf("%s %s: headers=%v; want the unknown-path headers %v", tc.method, tc.path, got, want)
		}
		for _, name := range []string{"Content-Type", "Cache-Control", "Referrer-Policy", "X-Content-Type-Options"} {
			if w.Header().Get(name) != unknown.Header().Get(name) {
				t.Errorf("%s %s: %s=%q; want the unknown-path value %q", tc.method, tc.path, name, w.Header().Get(name), unknown.Header().Get(name))
			}
		}
		for name := range w.Header() {
			if strings.HasPrefix(http.CanonicalHeaderKey(name), "Access-Control-") {
				t.Errorf("%s %s sends the CORS header %s", tc.method, tc.path, name)
			}
		}
		if strings.Contains(w.Body.String(), "spa-fallback") {
			t.Errorf("%s %s answered the SPA", tc.method, tc.path)
		}
	}
}

// headerSet returns the sorted header names, without the per-request ID that
// every answer carries with its own value.
func headerSet(h http.Header) []string {
	names := make([]string, 0, len(h))
	for name := range h {
		if http.CanonicalHeaderKey(name) == "X-Request-Id" {
			continue
		}
		names = append(names, http.CanonicalHeaderKey(name))
	}
	slices.Sort(names)
	return names
}

// TestMCPInstallationPutRefusesForeignOriginBeforeHandler: the installation
// PUT needs the canonical Origin like every state-changing session route.
func TestMCPInstallationPutRefusesForeignOriginBeforeHandler(t *testing.T) {
	app, cfg := newProductionTestApp(t)
	for _, origin := range []string{"", "https://evil.example"} {
		if w := serveAt(app, http.MethodPut, cfg.PublicURL+"/api/v1/admin/mcp-policy", origin); w.Code != http.StatusForbidden {
			t.Errorf("PUT /api/v1/admin/mcp-policy with origin %q status = %d, want 403", origin, w.Code)
		}
	}
}
