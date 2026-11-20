package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/go-chi/chi/v5"
)

const formHeaderValue = "application/x-www-form-urlencoded"

func formHeader(contentType string) http.Header {
	return http.Header{"Content-Type": {contentType}, "Origin": {mcpPublicURL}}
}

func validCodeForm() string {
	return "grant_type=authorization_code&code=CODE-1&redirect_uri=" + "http%3A%2F%2F127.0.0.1%3A5555%2Fcb" +
		"&client_id=client-1&code_verifier=" + strings.Repeat("v", 43)
}

func tokenOK() identity.MCPTokens {
	return identity.MCPTokens{
		AccessToken: "ACCESS", RefreshToken: "REFRESH", Scopes: []string{"tendo:read", "tendo:write"},
		AccessExpiresAt: time.Now().Add(time.Hour),
	}
}

func TestTokenRejectsWrongMediaTypeAndAcceptsCharset(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokens: tokenOK()}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	if w := mcpServe(h, http.MethodPost, "/oauth/token", validCodeForm(), formHeader("application/json"), "192.0.2.50:1"); w.Code != http.StatusBadRequest {
		t.Fatalf("wrong media type status=%d", w.Code)
	}
	if w := mcpServe(h, http.MethodPost, "/oauth/token", validCodeForm(), formHeader(formHeaderValue+"; charset=utf-8"), "192.0.2.50:1"); w.Code != http.StatusOK {
		t.Fatalf("charset status=%d body=%s", w.Code, w.Body)
	}
}

func TestTokenRejectsOversizeBody(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokens: tokenOK()}
	big := validCodeForm() + "&pad=" + strings.Repeat("a", mcpTokenBodyLimit)
	w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodPost, "/oauth/token", big, formHeader(formHeaderValue), "192.0.2.51:1")
	if w.Code != http.StatusBadRequest || flow.exchangeCalls != 0 {
		t.Fatalf("status=%d exchangeCalls=%d", w.Code, flow.exchangeCalls)
	}
}

func TestTokenIgnoresURLQueryParameters(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokens: tokenOK()}
	// Params only in the URL query: the body has no grant_type.
	w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodPost, "/oauth/token?"+validCodeForm(), "", formHeader(formHeaderValue), "192.0.2.52:1")
	if w.Code != http.StatusBadRequest || flow.exchangeCalls != 0 || !strings.Contains(w.Body.String(), "invalid_request") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

func TestTokenRefusesDuplicatesAndMissingParameters(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokens: tokenOK()}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	cases := map[string]string{
		"duplicate grant_type": validCodeForm() + "&grant_type=refresh_token",
		"duplicate code":       validCodeForm() + "&code=CODE-2",
		"missing grant_type":   "code=CODE-1",
		"missing code":         "grant_type=authorization_code&redirect_uri=x&client_id=c&code_verifier=v",
		"missing verifier":     "grant_type=authorization_code&code=CODE-1&redirect_uri=x&client_id=c",
		"missing client_id":    "grant_type=refresh_token&refresh_token=R",
	}
	for name, body := range cases {
		w := mcpServe(h, http.MethodPost, "/oauth/token", body, formHeader(formHeaderValue), "192.0.2.53:1")
		if w.Code != http.StatusBadRequest || flow.exchangeCalls != 0 || flow.refreshCalls != 0 {
			t.Errorf("%s: status=%d body=%s", name, w.Code, w.Body)
		}
	}
	// A repeated resource is invalid_target, not invalid_request.
	w := mcpServe(h, http.MethodPost, "/oauth/token", validCodeForm()+"&resource=a&resource=b", formHeader(formHeaderValue), "192.0.2.53:1")
	if !strings.Contains(w.Body.String(), "invalid_target") {
		t.Fatalf("repeated resource body=%s", w.Body)
	}
}

func TestTokenUnsupportedGrantTypeAndResourceMismatch(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokens: tokenOK()}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	w := mcpServe(h, http.MethodPost, "/oauth/token", "grant_type=password&client_id=c", formHeader(formHeaderValue), "192.0.2.54:1")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "unsupported_grant_type") {
		t.Fatalf("unsupported status=%d body=%s", w.Code, w.Body)
	}
	w = mcpServe(h, http.MethodPost, "/oauth/token", validCodeForm()+"&resource=https%3A%2F%2Fother.example%2Fmcp", formHeader(formHeaderValue), "192.0.2.54:1")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_target") || flow.exchangeCalls != 0 {
		t.Fatalf("resource mismatch status=%d body=%s", w.Code, w.Body)
	}
}

func TestTokenAbsentResourceUsesConfiguredResource(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokens: tokenOK()}
	mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodPost, "/oauth/token", validCodeForm(), formHeader(formHeaderValue), "192.0.2.55:1")
	if flow.exchangeIn.Resource != mcpPublicURL+"/mcp" {
		t.Fatalf("resource=%q", flow.exchangeIn.Resource)
	}
}

func TestTokenSuccessShapeHeadersAndNoCookies(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokens: tokenOK()}
	w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodPost, "/oauth/token", validCodeForm(), formHeader(formHeaderValue), "192.0.2.56:1")
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["access_token"] != "ACCESS" || got["token_type"] != "Bearer" || got["refresh_token"] != "REFRESH" || got["scope"] != "tendo:read tendo:write" {
		t.Fatalf("body=%v", got)
	}
	if expires, ok := got["expires_in"].(float64); !ok || expires < 3500 || expires > 3600 {
		t.Fatalf("expires_in=%v", got["expires_in"])
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Pragma") != "no-cache" || w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("headers=%v", w.Header())
	}
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("token endpoint set a cookie")
	}
}

func TestTokenNeverReadsSessionCookie(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokens: tokenOK()}
	sessions := signedIn()
	h := http.Header{"Content-Type": {formHeaderValue}, "Cookie": {sessionCookieHeader}}
	mcpServe(flowRouter(t, flow, sessions, mcpPublicURL), http.MethodPost, "/oauth/token", validCodeForm(), h, "192.0.2.57:1")
	if sessions.lookupCalls != 0 {
		t.Fatalf("token endpoint read the session (%d lookups)", sessions.lookupCalls)
	}
}

func TestTokenErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{identity.ErrMCPInvalidGrant, http.StatusBadRequest, "invalid_grant"},
		{identity.ErrMCPInvalidScope, http.StatusBadRequest, "invalid_scope"},
		{identity.ErrMCPInvalidTarget, http.StatusBadRequest, "invalid_target"},
		{errors.New("pq: secret driver text"), http.StatusServiceUnavailable, "temporarily_unavailable"},
	}
	for _, tc := range cases {
		flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokenErr: tc.err}
		w := mcpServe(flowRouter(t, flow, signedIn(), mcpPublicURL), http.MethodPost, "/oauth/token", validCodeForm(), formHeader(formHeaderValue), "192.0.2.58:1")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "secret") {
			t.Fatalf("%v: status=%d body=%s", tc.err, w.Code, w.Body)
		}
	}
}

func TestTokenRefreshPassesScopesAndRejectsMissing(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokens: tokenOK()}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	w := mcpServe(h, http.MethodPost, "/oauth/token", "grant_type=refresh_token&refresh_token=R&client_id=c&scope=tendo:read", formHeader(formHeaderValue), "192.0.2.59:1")
	if w.Code != http.StatusOK || flow.refreshIn.RefreshToken != "R" || len(flow.refreshIn.Scopes) != 1 || flow.refreshIn.Scopes[0] != "tendo:read" {
		t.Fatalf("status=%d refreshIn=%+v", w.Code, flow.refreshIn)
	}
	w = mcpServe(h, http.MethodPost, "/oauth/token", "grant_type=refresh_token&client_id=c", formHeader(formHeaderValue), "192.0.2.59:1")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing refresh_token status=%d", w.Code)
	}
}

func TestTokenRateLimitedAnswersRetryAfter(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokenErr: identity.ErrMCPInvalidGrant}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	for i := 0; i < mcpTokenPerClientPerMinute; i++ {
		mcpServe(h, http.MethodPost, "/oauth/token", validCodeForm(), formHeader(formHeaderValue), "198.51.100.60:1")
	}
	w := mcpServe(h, http.MethodPost, "/oauth/token", validCodeForm(), formHeader(formHeaderValue), "198.51.100.60:1")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" || !strings.Contains(w.Body.String(), "temporarily_unavailable") {
		t.Fatalf("status=%d retry=%q body=%s", w.Code, w.Header().Get("Retry-After"), w.Body)
	}
}

// TestTokenGlobalBucketShieldsPerClientMap: once the global token bucket is
// full, a request with a fresh caller-chosen client_id is refused before the
// per-client map is touched, so no new per-client key is created.
func TestTokenGlobalBucketShieldsPerClientMap(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}, tokenErr: identity.ErrMCPInvalidGrant}
	mcp := NewMCP(flow, NewSession(signedIn(), mcpPublicURL), mcpPublicURL)
	routes := chi.NewRouter()
	mcp.Register(routes)
	// Drain the global bucket from many IPs so no single per-client bucket fills.
	for i := 0; i < mcpTokenGlobalPerMinute; i++ {
		mcpServe(routes, http.MethodPost, "/oauth/token", validCodeForm(), formHeader(formHeaderValue), "10.1.0.1:1")
	}
	before := len(mcp.tokenPerClient.attempts)
	w := mcpServe(routes, http.MethodPost, "/oauth/token", "grant_type=authorization_code&code=C&redirect_uri=r&client_id=fresh-client&code_verifier=v", formHeader(formHeaderValue), "10.2.0.2:1")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if after := len(mcp.tokenPerClient.attempts); after != before {
		t.Fatalf("per-client keys grew from %d to %d while the global bucket was full", before, after)
	}
}

func TestTokenPreflightAndCORS(t *testing.T) {
	h := flowRouter(t, &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}}, signedIn(), mcpPublicURL)
	w := mcpServe(h, http.MethodOptions, "/oauth/token", "", http.Header{"Origin": {"https://foreign.example"}, "Access-Control-Request-Method": {"POST"}}, "192.0.2.60:1")
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("Access-Control-Allow-Credentials") != "" || w.Header().Get("Access-Control-Allow-Methods") != "POST, OPTIONS" {
		t.Fatalf("preflight status=%d headers=%v", w.Code, w.Header())
	}
}

func TestRevokeSuccessMissingParametersAndFault(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	ok := mcpServe(h, http.MethodPost, "/oauth/revoke", "token=TOK&client_id=client-1&token_type_hint=refresh_token", formHeader(formHeaderValue), "192.0.2.70:1")
	if ok.Code != http.StatusOK || ok.Body.Len() != 0 || ok.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("success status=%d body=%q headers=%v", ok.Code, ok.Body, ok.Header())
	}
	if flow.revokeIn.Token != "TOK" || flow.revokeIn.ClientID != "client-1" {
		t.Fatalf("revoke input=%+v", flow.revokeIn)
	}
	for name, body := range map[string]string{"missing token": "client_id=client-1", "missing client_id": "token=TOK"} {
		if w := mcpServe(h, http.MethodPost, "/oauth/revoke", body, formHeader(formHeaderValue), "192.0.2.71:1"); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d", name, w.Code)
		}
	}
	flow.revokeErr = errors.New("pq: secret")
	w := mcpServe(h, http.MethodPost, "/oauth/revoke", "token=TOK&client_id=client-1", formHeader(formHeaderValue), "192.0.2.72:1")
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("fault status=%d body=%s", w.Code, w.Body)
	}
}

func TestRevokeRejectsDuplicateTokenAndNeverReadsSession(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}}
	sessions := signedIn()
	h := flowRouter(t, flow, sessions, mcpPublicURL)
	w := mcpServe(h, http.MethodPost, "/oauth/revoke", "token=A&token=B&client_id=c", formHeader(formHeaderValue), "192.0.2.73:1")
	if w.Code != http.StatusBadRequest || flow.revokeCalls != 0 {
		t.Fatalf("duplicate status=%d calls=%d", w.Code, flow.revokeCalls)
	}
	mcpServe(h, http.MethodPost, "/oauth/revoke", "token=A&client_id=c", http.Header{"Content-Type": {formHeaderValue}, "Cookie": {sessionCookieHeader}}, "192.0.2.73:1")
	if sessions.lookupCalls != 0 {
		t.Fatalf("revoke read the session cookie")
	}
}

func TestRevokeRateLimitedPerIP(t *testing.T) {
	flow := &fakeMCPFlow{fakeMCPService: fakeMCPService{enabled: true}}
	h := flowRouter(t, flow, signedIn(), mcpPublicURL)
	for i := 0; i < mcpRevokePerIPPerMinute; i++ {
		mcpServe(h, http.MethodPost, "/oauth/revoke", "token=A&client_id=c", formHeader(formHeaderValue), "198.51.100.74:1")
	}
	w := mcpServe(h, http.MethodPost, "/oauth/revoke", "token=A&client_id=c", formHeader(formHeaderValue), "198.51.100.74:1")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("status=%d retry=%q", w.Code, w.Header().Get("Retry-After"))
	}
}
