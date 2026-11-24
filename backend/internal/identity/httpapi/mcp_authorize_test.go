package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

// fakeMCPFlow records every call of the A3a surface. Each method answers from
// its configured fields, so a test controls exactly one outcome.
type fakeMCPFlow struct {
	fakeMCPService
	beginIn       identity.MCPAuthorizationParams
	beginCalls    int
	begin         identity.MCPPendingAuthorization
	beginErr      error
	viewCalls     int
	viewHandle    string
	viewBinding   string
	view          identity.MCPAuthorizationRequestView
	viewErr       error
	decideIn      identity.MCPDecision
	decideCalls   int
	decision      identity.MCPDecisionResult
	decideErr     error
	exchangeIn    identity.MCPCodeExchange
	exchangeCalls int
	tokens        identity.MCPTokens
	tokenErr      error
	refreshIn     identity.MCPRefresh
	refreshCalls  int
	revokeIn      identity.MCPTokenRevocation
	revokeCalls   int
	revokeErr     error
}

func (f *fakeMCPFlow) BeginAuthorization(_ context.Context, in identity.MCPAuthorizationParams) (identity.MCPPendingAuthorization, error) {
	f.beginCalls++
	f.beginIn = in
	return f.begin, f.beginErr
}

func (f *fakeMCPFlow) GetAuthorizationRequest(_ context.Context, handle, binding string) (identity.MCPAuthorizationRequestView, error) {
	f.viewCalls++
	f.viewHandle, f.viewBinding = handle, binding
	return f.view, f.viewErr
}

func (f *fakeMCPFlow) DecideAuthorization(_ context.Context, in identity.MCPDecision) (identity.MCPDecisionResult, error) {
	f.decideCalls++
	f.decideIn = in
	return f.decision, f.decideErr
}

func (f *fakeMCPFlow) ExchangeCode(_ context.Context, in identity.MCPCodeExchange) (identity.MCPTokens, error) {
	f.exchangeCalls++
	f.exchangeIn = in
	return f.tokens, f.tokenErr
}

func (f *fakeMCPFlow) Refresh(_ context.Context, in identity.MCPRefresh) (identity.MCPTokens, error) {
	f.refreshCalls++
	f.refreshIn = in
	return f.tokens, f.tokenErr
}

func (f *fakeMCPFlow) RevokeToken(_ context.Context, in identity.MCPTokenRevocation) error {
	f.revokeCalls++
	f.revokeIn = in
	return f.revokeErr
}

const (
	testHandle  = "handle-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testBinding = "binding-BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	// The HTTPS cookie names are used with mcpPublicURL, which is https.
	sessionCookieHeader = "__Host-tendo_session=" + fakeToken
	bindingCookieName   = "__Secure-tendo_mcp_consent"
	registeredRedirect  = "http://127.0.0.1:5555/cb?x=1"
)

// flowRouter mounts the real adapter with a session handler over sessions.
func flowRouter(t *testing.T, service mcpRegistrar, sessions *fakeSessionService, publicURL string) http.Handler {
	t.Helper()
	routes := chi.NewRouter()
	NewMCP(service, NewSession(sessions, publicURL), publicURL).Register(routes)
	return httpx.NewAppWithRoutes(nil, time.Second, make(chan struct{}), httpx.OriginPolicy{PublicURL: mcpPublicURL, CrossOrigin: MCPCrossOriginRoutes()}, func(r chi.Router) { r.Mount("/", routes) })
}

// signedIn is a session fake for an account that can decide consents.
func signedIn() *fakeSessionService {
	return &fakeSessionService{principal: identity.Principal{UserID: "user-1", Login: "owner"}, expires: time.Now().Add(time.Hour)}
}

func authorizeTarget(extra string) string {
	return "/oauth/authorize?response_type=code&client_id=client-1&redirect_uri=" + url.QueryEscape(registeredRedirect) +
		"&code_challenge=" + strings.Repeat("A", 43) + "&code_challenge_method=S256" + extra
}

// decodeBody decodes the JSON body of a recorded response.
func decodeBody(w *httptest.ResponseRecorder, v any) error {
	return json.Unmarshal(w.Body.Bytes(), v)
}

func connectError(t *testing.T, w interface{ Header() http.Header }) string {
	t.Helper()
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "/connect#error=") {
		t.Fatalf("Location=%q, want /connect#error=...", loc)
	}
	return strings.TrimPrefix(loc, "/connect#error=")
}

func TestAuthorizeErrorsAlwaysRedirectToConnect(t *testing.T) {
	cases := []struct {
		name   string
		target string
		err    error
		code   string
	}{
		{name: "missing response_type", target: "/oauth/authorize?client_id=c", code: "invalid_request"},
		{name: "response_type token", target: "/oauth/authorize?response_type=token", code: "unsupported_response_type"},
		{name: "malformed query", target: "/oauth/authorize?response_type=code&state=%zz", code: "invalid_request"},
		{name: "duplicate client_id", target: authorizeTarget("&client_id=other"), code: "invalid_request"},
		{name: "duplicate resource", target: authorizeTarget("&resource=a&resource=b"), code: "invalid_target"},
		{name: "valid redirect with bad code_challenge", target: authorizeTarget(""), err: identity.ErrMCPInvalidRequest, code: "invalid_request"},
		{name: "invalid scope", target: authorizeTarget("&scope=bogus"), err: identity.ErrMCPInvalidScope, code: "invalid_scope"},
		{name: "invalid target", target: authorizeTarget("&resource=x"), err: identity.ErrMCPInvalidTarget, code: "invalid_target"},
		{name: "invalid client", target: authorizeTarget(""), err: identity.ErrMCPInvalidClient, code: "invalid_client"},
		{name: "invalid redirect", target: authorizeTarget(""), err: identity.ErrMCPInvalidRedirectURI, code: "invalid_redirect_uri"},
		{name: "unexpected failure", target: authorizeTarget(""), err: errors.New("pq: secret driver text"), code: "server_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, beginErr: tc.err}
			w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodGet, tc.target, "", nil, "192.0.2.20:1")
			if w.Code != http.StatusSeeOther {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if got := connectError(t, w); got != tc.code {
				t.Fatalf("error=%q want %q", got, tc.code)
			}
			loc := w.Header().Get("Location")
			if strings.Contains(loc, "127.0.0.1") || strings.Contains(loc, "5555") || strings.Contains(loc, "secret") {
				t.Fatalf("Location leaks client redirect or driver text: %q", loc)
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("headers=%v", w.Header())
			}
		})
	}
}

func TestAuthorizeRateLimitedAnswersTemporarilyUnavailable(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	for i := 0; i < mcpAuthorizeIPPerMinute; i++ {
		mcpServe(h, http.MethodGet, authorizeTarget(""), "", nil, "198.51.100.20:1")
	}
	w := mcpServe(h, http.MethodGet, authorizeTarget(""), "", nil, "198.51.100.20:1")
	if got := connectError(t, w); got != "temporarily_unavailable" {
		t.Fatalf("error=%q", got)
	}
}

func TestAuthorizeSuccessSetsBindingCookieAndNeverReadsSession(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, begin: identity.MCPPendingAuthorization{Handle: testHandle, BrowserBinding: testBinding}}
	sessions := signedIn()
	w := mcpServe(flowRouter(t, flow, sessions, mcpPublicURL), http.MethodGet, authorizeTarget("&scope=tendo:read&unknown=1"), "", http.Header{"Cookie": {sessionCookieHeader}}, "192.0.2.21:1")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/connect#request="+testHandle {
		t.Fatalf("status=%d location=%q", w.Code, w.Header().Get("Location"))
	}
	if sessions.lookupCalls != 0 {
		t.Fatalf("authorize read the session cookie (%d lookups)", sessions.lookupCalls)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("headers=%v", w.Header())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	c := cookies[0]
	if c.Name != bindingCookieName || c.Value != testBinding || c.Path != "/api/v1/mcp/authorization-requests/"+testHandle ||
		!c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 300 {
		t.Fatalf("binding cookie=%+v", c)
	}
	if len(flow.beginIn.Scopes) != 1 || flow.beginIn.Scopes[0] != "tendo:read" || flow.beginIn.State != "" {
		t.Fatalf("begin params=%+v", flow.beginIn)
	}
}

func TestAuthorizeEmptyScopeIsAbsent(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, begin: identity.MCPPendingAuthorization{Handle: testHandle, BrowserBinding: testBinding}}
	mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodGet, authorizeTarget("&scope=+++"), "", nil, "192.0.2.22:1")
	if flow.beginIn.Scopes != nil {
		t.Fatalf("scopes=%v, want absent", flow.beginIn.Scopes)
	}
}

func TestAuthorizePlainHTTPUsesUnprefixedCookie(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, begin: identity.MCPPendingAuthorization{Handle: testHandle, BrowserBinding: testBinding}}
	w := mcpServe(flowRouter(t, flow, signedIn(), "http://127.0.0.1"), http.MethodGet, authorizeTarget(""), "", nil, "192.0.2.23:1")
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "tendo_mcp_consent" || cookies[0].Secure {
		t.Fatalf("cookies=%v", cookies)
	}
}

func TestDisabledAnswersNotFoundOnEveryConsentAndTokenRoute(t *testing.T) {
	h := flowRouter(t, &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: false}}, signedIn(), mcpPublicURL)
	unknown := mcpServe(h, http.MethodGet, "/oauth/xyz", "", nil, "192.0.2.24:1")
	cases := []struct{ method, path string }{
		{http.MethodGet, "/oauth/authorize"},
		{http.MethodGet, "/api/v1/mcp/authorization-requests/" + testHandle},
		{http.MethodPost, "/api/v1/mcp/authorization-requests/" + testHandle + "/decision"},
		{http.MethodPost, "/oauth/token"},
		{http.MethodOptions, "/oauth/token"},
		{http.MethodPost, "/oauth/revoke"},
		{http.MethodOptions, "/oauth/revoke"},
	}
	for _, tc := range cases {
		// The canonical Origin passes the origin policy, so only routing
		// decides. The session cookie and body are present: neither may be reached.
		w := mcpServe(h, tc.method, tc.path, `{"decision":"approve"}`, http.Header{"Cookie": {sessionCookieHeader}, "Content-Type": {"application/json"}, "Origin": {mcpPublicURL}}, "192.0.2.24:1")
		if w.Code != unknown.Code || w.Body.String() != unknown.Body.String() || w.Header().Get("Set-Cookie") != "" {
			t.Errorf("%s %s: status=%d body=%s, want %d %s", tc.method, tc.path, w.Code, w.Body, unknown.Code, unknown.Body)
		}
	}
}

func TestGetAuthorizationRequestRequiresSessionAndBinding(t *testing.T) {
	view := identity.MCPAuthorizationRequestView{ClientName: "Claude", RedirectURI: registeredRedirect, Scopes: []string{"tendo:read"}, ExpiresAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	path := "/api/v1/mcp/authorization-requests/" + testHandle
	bindingHeader := http.Header{"Cookie": {sessionCookieHeader + "; " + bindingCookieName + "=" + testBinding}}

	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, view: view}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	if w := mcpServe(h, http.MethodGet, path, "", http.Header{"Cookie": {bindingCookieName + "=" + testBinding}}, "192.0.2.30:1"); w.Code != http.StatusUnauthorized || flow.viewCalls != 0 {
		t.Fatalf("no session: status=%d viewCalls=%d", w.Code, flow.viewCalls)
	}
	if w := mcpServe(h, http.MethodGet, path, "", http.Header{"Cookie": {sessionCookieHeader}}, "192.0.2.30:1"); w.Code != http.StatusNotFound || flow.viewCalls != 0 {
		t.Fatalf("no binding: status=%d viewCalls=%d", w.Code, flow.viewCalls)
	}
	dup := http.Header{"Cookie": {sessionCookieHeader + "; " + bindingCookieName + "=" + testBinding + "; " + bindingCookieName + "=other"}}
	if w := mcpServe(h, http.MethodGet, path, "", dup, "192.0.2.30:1"); w.Code != http.StatusNotFound || flow.viewCalls != 0 {
		t.Fatalf("duplicate binding: status=%d viewCalls=%d", w.Code, flow.viewCalls)
	}
	w := mcpServe(h, http.MethodGet, path, "", bindingHeader, "192.0.2.30:1")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	assertJSONBody(t, w, http.StatusOK, `{"clientName":"Claude","clientNameVerified":false,"redirectUri":"http://127.0.0.1:5555/cb?x=1","scopes":["tendo:read"],"expiresAt":"2026-11-20T12:00:00Z"}`)
	if flow.viewHandle != testHandle || flow.viewBinding != testBinding {
		t.Fatalf("service got handle/binding mismatch")
	}
}

func TestGetAuthorizationRequestNotFoundAndFault(t *testing.T) {
	path := "/api/v1/mcp/authorization-requests/" + testHandle
	header := http.Header{"Cookie": {sessionCookieHeader + "; " + bindingCookieName + "=" + testBinding}}
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, viewErr: identity.ErrMCPAuthorizationRequestNotFound}
	if w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodGet, path, "", header, "192.0.2.31:1"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "authorization_request_not_found") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	flow.viewErr = errors.New("pq: secret")
	w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodGet, path, "", header, "192.0.2.31:1")
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

// decisionHeader is the signed-in POST header set with the binding cookie.
func decisionHeader() http.Header {
	return http.Header{
		"Content-Type": {"application/json"},
		"Origin":       {mcpPublicURL},
		"Cookie":       {sessionCookieHeader + "; " + bindingCookieName + "=" + testBinding},
	}
}

func decisionPath() string { return "/api/v1/mcp/authorization-requests/" + testHandle + "/decision" }

func TestDecisionRedirectPreservesQueryAndCarriesIssuer(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, decision: identity.MCPDecisionResult{RedirectURI: registeredRedirect, Code: "CODE-1", State: "st ate"}}
	w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodPost, decisionPath(), `{"decision":"approve"}`, decisionHeader(), "192.0.2.40:1")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var body struct {
		RedirectTo string `json:"redirectTo"`
	}
	if err := decodeBody(w, &body); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(body.RedirectTo)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Host != "127.0.0.1:5555" || parsed.Path != "/cb" || !strings.HasPrefix(parsed.RawQuery, "x=1&") {
		t.Fatalf("redirectTo=%q: registered query must be preserved first", body.RedirectTo)
	}
	q := parsed.Query()
	if q.Get("code") != "CODE-1" || q.Get("state") != "st ate" || q.Get("iss") != mcpPublicURL || q.Get("error") != "" {
		t.Fatalf("query=%v", q)
	}
	clearCookie := w.Result().Cookies()
	if len(clearCookie) != 1 || clearCookie[0].MaxAge != -1 || clearCookie[0].Path != "/api/v1/mcp/authorization-requests/"+testHandle {
		t.Fatalf("binding cookie not cleared: %v", clearCookie)
	}
	if flow.decideIn.Principal.UserID != "user-1" || flow.decideIn.SessionToken != fakeToken || !flow.decideIn.Approve || flow.decideIn.BrowserBinding != testBinding {
		t.Fatalf("decision input=%+v", flow.decideIn)
	}
}

func TestDecisionDenyCarriesAccessDeniedWithoutState(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, decision: identity.MCPDecisionResult{RedirectURI: registeredRedirect}}
	w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodPost, decisionPath(), `{"decision":"deny"}`, decisionHeader(), "192.0.2.41:1")
	var body struct {
		RedirectTo string `json:"redirectTo"`
	}
	if err := decodeBody(w, &body); err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(body.RedirectTo)
	q := parsed.Query()
	if q.Get("error") != "access_denied" || q.Get("code") != "" || q.Has("state") || q.Get("iss") != mcpPublicURL {
		t.Fatalf("redirectTo=%q", body.RedirectTo)
	}
}

func TestDecisionRefusesCrossOriginAndBadBodies(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	foreign := decisionHeader()
	foreign.Set("Origin", "https://foreign.example")
	if w := mcpServe(h, http.MethodPost, decisionPath(), `{"decision":"approve"}`, foreign, "192.0.2.42:1"); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin status=%d", w.Code)
	}
	bodies := map[string]string{
		"unknown field": `{"decision":"approve","extra":1}`,
		"duplicate key": `{"decision":"approve","decision":"deny"}`,
		"bad value":     `{"decision":"maybe"}`,
		"not an object": `["approve"]`,
		"wrong type":    `{"decision":true}`,
	}
	for name, body := range bodies {
		w := mcpServe(h, http.MethodPost, decisionPath(), body, decisionHeader(), "192.0.2.42:1")
		if w.Code != http.StatusBadRequest || flow.decideCalls != 0 {
			t.Errorf("%s: status=%d decideCalls=%d body=%s", name, w.Code, flow.decideCalls, w.Body)
		}
	}
	noJSON := decisionHeader()
	noJSON.Set("Content-Type", "text/plain")
	if w := mcpServe(h, http.MethodPost, decisionPath(), `{"decision":"approve"}`, noJSON, "192.0.2.42:1"); w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("media type status=%d", w.Code)
	}
}

func TestDecisionNotFoundAndUnauthenticatedOutcomes(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, decideErr: identity.ErrMCPAuthorizationRequestNotFound}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	w := mcpServe(h, http.MethodPost, decisionPath(), `{"decision":"approve"}`, decisionHeader(), "192.0.2.43:1")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "authorization_request_not_found") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if c := w.Result().Cookies(); len(c) != 1 || c[0].MaxAge != -1 {
		t.Fatalf("404 must clear the binding cookie: %v", c)
	}
	flow.decideErr = identity.ErrUnauthenticated
	if w := mcpServe(h, http.MethodPost, decisionPath(), `{"decision":"approve"}`, decisionHeader(), "192.0.2.43:1"); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", w.Code)
	}
	// The installation policy switched off after routing (#457): the disabled 404.
	flow.decideErr = identity.ErrMCPDisabled
	disabled := mcpServe(flowRouter(t, &fakeMCPFlow{}, signedIn(), mcpPublicURL), http.MethodPost, decisionPath(), `{"decision":"approve"}`, decisionHeader(), "192.0.2.43:1")
	if w := mcpServe(h, http.MethodPost, decisionPath(), `{"decision":"approve"}`, decisionHeader(), "192.0.2.43:1"); w.Code != http.StatusNotFound || disabled.Code != http.StatusNotFound || w.Body.String() != disabled.Body.String() || w.Header().Get("Content-Type") != disabled.Header().Get("Content-Type") {
		t.Fatalf("installation off status=%d body=%s want the disabled 404 %s", w.Code, w.Body, disabled.Body)
	}
	flow.decideErr = errors.New("pq: secret driver text")
	w = mcpServe(h, http.MethodPost, decisionPath(), `{"decision":"approve"}`, decisionHeader(), "192.0.2.43:1")
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("fault status=%d body=%s", w.Code, w.Body)
	}
}

// TestDecisionRequiresSessionBeforeDecision proves RequireSession is wired on
// the decision route: with a valid origin and binding cookie, a missing or
// stale session cookie is refused with 401 before the service is called. The
// production contract probe no longer reaches this route while MCP is disabled,
// so this test is the only proof.
func TestDecisionRequiresSessionBeforeDecision(t *testing.T) {
	const staleToken = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
	bindingOnly := http.Header{
		"Content-Type": {"application/json"},
		"Origin":       {mcpPublicURL},
		"Cookie":       {bindingCookieName + "=" + testBinding},
	}
	stale := http.Header{
		"Content-Type": {"application/json"},
		"Origin":       {mcpPublicURL},
		"Cookie":       {"__Host-tendo_session=" + staleToken + "; " + bindingCookieName + "=" + testBinding},
	}

	t.Run("no session cookie", func(t *testing.T) {
		flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}}
		w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodPost, decisionPath(), `{"decision":"approve"}`, bindingOnly, "192.0.2.80:1")
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "unauthenticated") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		if flow.decideCalls != 0 {
			t.Fatalf("decideCalls=%d, want 0", flow.decideCalls)
		}
	})

	t.Run("stale session cookie", func(t *testing.T) {
		flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}}
		sessions := signedIn()
		w := mcpServe(flowRouter(t, flow, sessions, mcpPublicURL), http.MethodPost, decisionPath(), `{"decision":"approve"}`, stale, "192.0.2.81:1")
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "unauthenticated") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
		var cleared bool
		for _, c := range w.Result().Cookies() {
			if c.Name == "__Host-tendo_session" && c.MaxAge == -1 {
				cleared = true
			}
		}
		if !cleared {
			t.Fatalf("stale session was not cleared: %v", w.Result().Cookies())
		}
		if flow.decideCalls != 0 {
			t.Fatalf("decideCalls=%d, want 0", flow.decideCalls)
		}
		if sessions.lookupCalls != 1 {
			t.Fatalf("lookupCalls=%d, want exactly the one session lookup", sessions.lookupCalls)
		}
	})
}

// TestDecisionRejectsMalformedHandleWithoutClearing: a malformed handle answers
// 404 without writing any Set-Cookie, because no binding Path was ever issued.
func TestDecisionRejectsMalformedHandleWithoutClearing(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, decideErr: identity.ErrMCPAuthorizationRequestNotFound}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	w := mcpServe(h, http.MethodPost, "/api/v1/mcp/authorization-requests/short/decision", `{"decision":"approve"}`, decisionHeader(), "192.0.2.82:1")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "authorization_request_not_found") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if len(w.Result().Cookies()) != 0 || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("malformed handle wrote a cookie: %v", w.Result().Cookies())
	}
}

func TestValidRequestHandle(t *testing.T) {
	ok := []string{testHandle}
	bad := []string{"", "short", testHandle + "A", testHandle[:42] + "=", testHandle[:42] + "/", testHandle[:42] + " "}
	for _, h := range ok {
		if !validRequestHandle(h) {
			t.Errorf("%q rejected", h)
		}
	}
	for _, h := range bad {
		if validRequestHandle(h) {
			t.Errorf("%q accepted", h)
		}
	}
}

func TestDecisionPerUserRateLimit(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, decideErr: identity.ErrMCPAuthorizationRequestNotFound}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	for i := 0; i < mcpDecisionPerUserPerMinute; i++ {
		mcpServe(h, http.MethodPost, decisionPath(), `{"decision":"approve"}`, decisionHeader(), "192.0.2.44:1")
	}
	w := mcpServe(h, http.MethodPost, decisionPath(), `{"decision":"approve"}`, decisionHeader(), "192.0.2.44:1")
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "rate_limited") || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}
