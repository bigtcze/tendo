package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

// Slice B1b2 of #457 (ADR 0023 "Slice B1b2"): the installation switch gates the
// same routes as TENDO_MCP_ENABLED, before any other work, and a gate fault
// fails closed per route.

const (
	mcpInstallationTestHandle = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	mcpDecisionTestPath       = "/api/v1/mcp/authorization-requests/" + mcpInstallationTestHandle + "/decision"
	mcpRequestTestPath        = "/api/v1/mcp/authorization-requests/" + mcpInstallationTestHandle
)

// installationGateRouter mounts the MCP adapter and the endpoint exactly as
// composition does, over a service whose gate answers as the caller configures.
func installationGateRouter(t *testing.T, svc mcpRegistrar, verifier mcpTokenVerifier) http.Handler {
	t.Helper()
	routes := chi.NewRouter()
	NewMCP(svc, NewSession(signedIn(), mcpPublicURL), mcpPublicURL).Register(routes)
	endpoint, err := NewMCPEndpoint(verifier, mcpPublicURL, time.Second, []MCPTool{noopTool("list_households", identity.MCPScopeRead)})
	if err != nil {
		t.Fatal(err)
	}
	endpoint.Register(routes)
	return httpx.NewAppWithRoutes(nil, time.Second, make(chan struct{}), httpx.OriginPolicy{PublicURL: mcpPublicURL, CrossOrigin: MCPCrossOriginRoutes()}, func(r chi.Router) { r.Mount("/", routes) })
}

// gateHeader carries the canonical Origin, so the origin check passes and the
// answer comes from the gate alone, as in the composition tests.
func gateHeader(extra http.Header) http.Header {
	h := http.Header{"Origin": {mcpPublicURL}}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

// installationOffGate lists every route the installation switch gates: the same
// set that TENDO_MCP_ENABLED off answers as the unknown-path 404.
var installationOffGate = []struct{ method, path string }{
	{http.MethodPost, mcpRegisterPath},
	{http.MethodOptions, mcpRegisterPath},
	{http.MethodGet, mcpProtectedResourcePath},
	{http.MethodOptions, mcpProtectedResourcePath},
	{http.MethodGet, mcpAuthorizationServerPath},
	{http.MethodOptions, mcpAuthorizationServerPath},
	{http.MethodGet, mcpAuthorizePath},
	{http.MethodGet, mcpRequestTestPath},
	{http.MethodPost, mcpDecisionTestPath},
	{http.MethodPost, mcpTokenPath},
	{http.MethodOptions, mcpTokenPath},
	{http.MethodPost, mcpRevokePath},
	{http.MethodOptions, mcpRevokePath},
	{http.MethodPost, mcpEndpointPath},
	// The non-POST method answers 405 when enabled; off it must be the 404.
	{http.MethodGet, mcpEndpointPath},
}

// TestInstallationOffAnswersUnknownPathOnEveryGatedRoute: with the installation
// switch off, every gated route answers the unknown-path 404 and never reaches
// its handler. A handler would answer differently, for example 405 on GET /mcp,
// 400 on an empty token request or 200 on a discovery document, so the 404 is
// the gate's own answer.
func TestInstallationOffAnswersUnknownPathOnEveryGatedRoute(t *testing.T) {
	svc := &fakeMCPService{enabled: true, installationOff: true}
	verifier := &fakeMCPVerifier{enabled: true, installationOff: true}
	h := installationGateRouter(t, svc, verifier)
	for _, tc := range installationOffGate {
		w := mcpServe(h, tc.method, tc.path, "", gateHeader(http.Header{"Content-Type": {"application/json"}}), "192.0.2.1:1")
		if w.Code != http.StatusNotFound || w.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s %s: status=%d type=%q; want the unknown-path 404", tc.method, tc.path, w.Code, w.Header().Get("Content-Type"))
		}
	}
	if svc.calls != 0 {
		t.Errorf("a switched-off route reached the registration service: %d calls", svc.calls)
	}
}

// TestInstallationOffGateRunsBeforeTheRateLimit: the gate runs before the
// per-IP limit, so a switched-off registration neither spends nor is limited.
func TestInstallationOffGateRunsBeforeTheRateLimit(t *testing.T) {
	h := installationGateRouter(t, &fakeMCPService{enabled: true, installationOff: true}, &fakeMCPVerifier{enabled: true})
	for i := 0; i < mcpRegisterPerIPPerMinute+5; i++ {
		w := mcpServe(h, http.MethodPost, mcpRegisterPath, validRegistration, jsonHeader(), "192.0.2.9:1")
		if w.Code != http.StatusNotFound {
			t.Fatalf("attempt %d status=%d; a switched-off route must not rate limit", i, w.Code)
		}
	}
}

// TestInstallationGateFaultFailsClosedPerRoute: a failed switch read is never
// served as on. Each route answers its own fail-closed response, and the
// response carries no driver text.
func TestInstallationGateFaultFailsClosedPerRoute(t *testing.T) {
	failure := errors.New("driver: connection refused")
	svc := &fakeMCPService{enabled: true, availErr: failure}
	h := installationGateRouter(t, svc, &fakeMCPVerifier{enabled: true, availErr: failure})

	w := mcpServe(h, http.MethodPost, mcpRegisterPath, validRegistration, gateHeader(jsonHeader()), "192.0.2.1:1")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"temporarily_unavailable"`) {
		t.Errorf("register fault: status=%d body=%s; want the OAuth 503", w.Code, w.Body)
	}
	w = mcpServe(h, http.MethodGet, mcpProtectedResourcePath, "", gateHeader(nil), "192.0.2.1:1")
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Content-Type") != "application/problem+json" || !strings.Contains(w.Body.String(), `"unavailable"`) {
		t.Errorf("discovery fault: status=%d type=%q body=%s; want the problem 503", w.Code, w.Header().Get("Content-Type"), w.Body)
	}
	w = mcpServe(h, http.MethodPost, mcpEndpointPath, "{}", gateHeader(http.Header{"Authorization": {"Bearer " + strings.Repeat("A", 43)}}), "192.0.2.1:1")
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("mcp fault: status=%d; want the 503", w.Code)
	}
	// The browser entry point fails closed with a server_error redirect that
	// carries the same no-store and no-referrer headers as every other answer.
	w = mcpServe(h, http.MethodGet, mcpAuthorizePath, "", gateHeader(nil), "192.0.2.1:1")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != mcpConnectPath+"#error=server_error" {
		t.Errorf("authorize fault: status=%d location=%q; want 303 to #error=server_error", w.Code, w.Header().Get("Location"))
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("authorize fault Cache-Control=%q, want no-store", got)
	}
	if got := w.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("authorize fault Referrer-Policy=%q, want no-referrer", got)
	}
	if strings.Contains(w.Body.String(), "driver") {
		t.Errorf("gate fault leaked driver text: %s", w.Body)
	}
	if svc.calls != 0 {
		t.Errorf("a failed gate reached the registration service: %d calls", svc.calls)
	}
}

// TestInstallationOnServesDiscovery: an installation that is on serves the
// discovery document, so the gate only narrows.
func TestInstallationOnServesDiscovery(t *testing.T) {
	h := installationGateRouter(t, &fakeMCPService{enabled: true}, &fakeMCPVerifier{enabled: true})
	w := mcpServe(h, http.MethodGet, mcpProtectedResourcePath, "", nil, "192.0.2.1:1")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

// TestInstallationPolicyRoutesRequireSessionAndAdmin: the installation policy
// and audit routes are session-only, and the instance-admin check runs before
// If-Match, the body and any lookup, like the account policy routes.
func TestInstallationPolicyRoutesRequireSessionAndAdmin(t *testing.T) {
	svc := &fakeMCPPolicyService{adminErr: identity.ErrInstanceAdminRequired}
	h := policyRouter(svc, signedIn())
	bearer := "Bearer " + strings.Repeat("A", 43)
	for _, c := range []policyCall{
		{method: http.MethodGet, target: mcpInstallationPolicyPath},
		{method: http.MethodPut, target: mcpInstallationPolicyPath, body: `{"bogus"`, ifMatch: `"1"`, contentType: policyJSON},
		{method: http.MethodGet, target: mcpPolicyEventsPath},
	} {
		c.authorization = bearer
		if w := policyServe(h, c); w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s bearer-only: status=%d cache=%q", c.method, c.target, w.Code, w.Header().Get("Cache-Control"))
		}
	}
	for _, c := range []policyCall{
		{method: http.MethodGet, target: mcpInstallationPolicyPath, cookie: sessionCookieHeader},
		{method: http.MethodPut, target: mcpInstallationPolicyPath, body: `{"bogus"`, ifMatch: `"1"`, contentType: policyJSON, cookie: sessionCookieHeader},
		{method: http.MethodGet, target: mcpPolicyEventsPath, cookie: sessionCookieHeader},
	} {
		assertProblem(t, policyServe(h, c), http.StatusForbidden, "instance_admin_required")
	}
	if svc.readCalls != 0 || svc.updCalls != 0 || svc.total() != 3 {
		t.Fatalf("a non-admin reached a command: total=%d", svc.total())
	}
}
