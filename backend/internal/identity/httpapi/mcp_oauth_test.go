package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

type fakeMCPService struct {
	enabled bool
	err     error
	calls   int
	got     identity.MCPClientRegistration
	// installationOff models the installation switch read as off (B1b2);
	// availErr models a failed or missing switch read, which fails closed.
	installationOff bool
	availErr        error
}

func (f *fakeMCPService) Enabled() bool { return f.enabled }

// Available is TENDO_MCP_ENABLED and the installation switch (B1b2).
func (f *fakeMCPService) Available(context.Context) (bool, error) {
	if f.availErr != nil {
		return false, f.availErr
	}
	return f.enabled && !f.installationOff, nil
}

func (f *fakeMCPService) RegisterClient(_ context.Context, reg identity.MCPClientRegistration) (identity.MCPRegisteredClient, error) {
	f.calls++
	f.got = reg
	if f.err != nil {
		return identity.MCPRegisteredClient{}, f.err
	}
	return identity.MCPRegisteredClient{
		ID: "client-1", Name: "MCP client", RedirectURIs: reg.RedirectURIs,
		IssuedAt: time.Unix(1790000000, 0).UTC(),
	}, nil
}

func (f *fakeMCPService) BeginAuthorization(context.Context, identity.MCPAuthorizationParams) (identity.MCPPendingAuthorization, error) {
	return identity.MCPPendingAuthorization{}, errors.New("not used by registration tests")
}

func (f *fakeMCPService) GetAuthorizationRequest(context.Context, string, string) (identity.MCPAuthorizationRequestView, error) {
	return identity.MCPAuthorizationRequestView{}, errors.New("not used by registration tests")
}

func (f *fakeMCPService) DecideAuthorization(context.Context, identity.MCPDecision) (identity.MCPDecisionResult, error) {
	return identity.MCPDecisionResult{}, errors.New("not used by registration tests")
}

func (f *fakeMCPService) ExchangeCode(context.Context, identity.MCPCodeExchange) (identity.MCPTokens, error) {
	return identity.MCPTokens{}, errors.New("not used by registration tests")
}

func (f *fakeMCPService) Refresh(context.Context, identity.MCPRefresh) (identity.MCPTokens, error) {
	return identity.MCPTokens{}, errors.New("not used by registration tests")
}

func (f *fakeMCPService) RevokeToken(context.Context, identity.MCPTokenRevocation) error {
	return errors.New("not used by registration tests")
}

func (f *fakeMCPService) Resource() string { return mcpPublicURL + "/mcp" }

const mcpPublicURL = "https://tendo.example"

func newMCPRouter(t *testing.T, service mcpRegistrar, publicURL string) http.Handler {
	t.Helper()
	routes := chi.NewRouter()
	NewMCP(service, NewSession(&fakeSessionService{}, publicURL), publicURL).Register(routes)
	return httpx.NewAppWithRoutes(nil, time.Second, make(chan struct{}), httpx.OriginPolicy{PublicURL: mcpPublicURL, CrossOrigin: MCPCrossOriginRoutes()}, func(r chi.Router) { r.Mount("/", routes) })
}

func mcpServe(h http.Handler, method, target string, body string, header http.Header, remote string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, mcpPublicURL+target, strings.NewReader(body))
	r.RemoteAddr = remote
	for k, v := range header {
		r.Header[k] = v
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func jsonHeader() http.Header { return http.Header{"Content-Type": {"application/json"}} }

const validRegistration = `{"redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"client_name":"Claude","scope":"claudeai"}`

func TestRegisterReturnsRFCClientShape(t *testing.T) {
	svc := &fakeMCPService{enabled: true}
	w := mcpServe(newMCPRouter(t, svc, mcpPublicURL), http.MethodPost, "/oauth/register", validRegistration, jsonHeader(), "192.0.2.1:1234")
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"client_id":                  "client-1",
		"client_id_issued_at":        float64(1790000000),
		"client_name":                "MCP client",
		"redirect_uris":              []any{"https://claude.ai/api/mcp/auth_callback"},
		"grant_types":                []any{"authorization_code", "refresh_token"},
		"response_types":             []any{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      "tendo:read tendo:write",
	}
	for k, v := range want {
		if !jsonEqual(got[k], v) {
			t.Errorf("%s=%v want %v", k, got[k], v)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("headers=%v", w.Header())
	}
	if svc.calls != 1 {
		t.Errorf("RegisterClient calls=%d", svc.calls)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestRegisterErrorsUseRFCShape(t *testing.T) {
	cases := []struct {
		name    string
		header  http.Header
		body    string
		err     error
		status  int
		errCode string
	}{
		{name: "invalid redirect", header: jsonHeader(), body: validRegistration, err: identity.ErrMCPInvalidRedirectURI, status: 400, errCode: "invalid_redirect_uri"},
		{name: "invalid metadata", header: jsonHeader(), body: validRegistration, err: identity.ErrMCPInvalidClientMetadata, status: 400, errCode: "invalid_client_metadata"},
		{name: "cap full", header: jsonHeader(), body: validRegistration, err: identity.ErrMCPRegistrationFull, status: 503, errCode: "temporarily_unavailable"},
		{name: "wrong media type", header: http.Header{"Content-Type": {"text/plain"}}, body: validRegistration, status: 415, errCode: "invalid_client_metadata"},
		{name: "missing media type", header: http.Header{}, body: validRegistration, status: 415, errCode: "invalid_client_metadata"},
		{name: "body too large", header: jsonHeader(), body: `{"redirect_uris":["https://a.example/` + strings.Repeat("a", 17<<10) + `"]}`, status: 413, errCode: "invalid_client_metadata"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeMCPService{enabled: true, err: tc.err}
			w := mcpServe(newMCPRouter(t, svc, mcpPublicURL), http.MethodPost, "/oauth/register", tc.body, tc.header, "192.0.2.2:1")
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["error"] != tc.errCode || body["error_description"] == "" {
				t.Fatalf("body=%s, want RFC error %q", w.Body, tc.errCode)
			}
			if w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("headers=%v", w.Header())
			}
			if tc.status == 503 && w.Header().Get("Retry-After") != "" {
				t.Errorf("503 must not carry Retry-After")
			}
		})
	}
}

func TestRegisterJSONParameterAccepted(t *testing.T) {
	svc := &fakeMCPService{enabled: true}
	h := http.Header{"Content-Type": {"application/json; charset=utf-8"}}
	if w := mcpServe(newMCPRouter(t, svc, mcpPublicURL), http.MethodPost, "/oauth/register", validRegistration, h, "192.0.2.3:1"); w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

func TestRegisterRateLimitsPerIPAndGlobally(t *testing.T) {
	svc := &fakeMCPService{enabled: true}
	h := newMCPRouter(t, svc, mcpPublicURL)
	for i := 0; i < mcpRegisterPerIPPerMinute; i++ {
		if w := mcpServe(h, http.MethodPost, "/oauth/register", validRegistration, jsonHeader(), "198.51.100.7:1"); w.Code != http.StatusCreated {
			t.Fatalf("attempt %d status=%d", i, w.Code)
		}
	}
	w := mcpServe(h, http.MethodPost, "/oauth/register", validRegistration, jsonHeader(), "198.51.100.7:1")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("per-ip limit status=%d retry=%q", w.Code, w.Header().Get("Retry-After"))
	}
	// A browser must be able to read the 429 and its Retry-After.
	if w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("Access-Control-Expose-Headers") != "Retry-After" {
		t.Fatalf("429 CORS headers = %v; want Allow-Origin * and Expose-Headers Retry-After", w.Header())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["error"] != "temporarily_unavailable" {
		t.Fatalf("429 body=%s", w.Body)
	}
	// A different client still gets through the per-IP bucket, until the
	// global bucket (60/min, separate from the per-IP bucket) is exhausted.
	for i := mcpRegisterPerIPPerMinute; i < mcpRegisterGlobalPerMinute; i++ {
		ip := "203.0.113." + itoa(i%250+1) + ":1"
		if w := mcpServe(h, http.MethodPost, "/oauth/register", validRegistration, jsonHeader(), ip); w.Code != http.StatusCreated {
			t.Fatalf("global attempt %d status=%d", i, w.Code)
		}
	}
	w = mcpServe(h, http.MethodPost, "/oauth/register", validRegistration, jsonHeader(), "192.0.2.250:1")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("global limit status=%d", w.Code)
	}
}

func itoa(i int) string {
	b := []byte{}
	if i == 0 {
		return "0"
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestRegisterIgnoresSessionCookieAndSetsNone(t *testing.T) {
	svc := &fakeMCPService{enabled: true}
	h := http.Header{"Content-Type": {"application/json"}, "Cookie": {"tendo_session=" + fakeToken}}
	w := mcpServe(newMCPRouter(t, svc, mcpPublicURL), http.MethodPost, "/oauth/register", validRegistration, h, "192.0.2.4:1")
	if w.Code != http.StatusCreated || len(w.Result().Cookies()) != 0 || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("status=%d cookies=%v set-cookie=%q", w.Code, w.Result().Cookies(), w.Header().Get("Set-Cookie"))
	}
}

func TestRegisterPassesRegistrationThroughUnchanged(t *testing.T) {
	svc := &fakeMCPService{enabled: true}
	mcpServe(newMCPRouter(t, svc, mcpPublicURL), http.MethodPost, "/oauth/register", validRegistration, jsonHeader(), "192.0.2.5:1")
	if svc.got.RedirectURIs[0] != "https://claude.ai/api/mcp/auth_callback" || svc.got.Name == nil || *svc.got.Name != "Claude" {
		t.Fatalf("decoded=%+v", svc.got)
	}
}

func TestMetadataDocumentsExactJSON(t *testing.T) {
	svc := &fakeMCPService{enabled: true}
	h := newMCPRouter(t, svc, mcpPublicURL+"/")
	w := mcpServe(h, http.MethodGet, "/.well-known/oauth-protected-resource/mcp", "", nil, "192.0.2.6:1")
	wantPRM := `{"authorization_servers":["https://tendo.example"],"bearer_methods_supported":["header"],"resource":"https://tendo.example/mcp","resource_name":"Tendo","scopes_supported":["tendo:read","tendo:write"]}`
	assertJSONBody(t, w, http.StatusOK, wantPRM)

	w = mcpServe(h, http.MethodGet, "/.well-known/oauth-authorization-server", "", nil, "192.0.2.6:1")
	wantAS := `{"issuer":"https://tendo.example","authorization_endpoint":"https://tendo.example/oauth/authorize","token_endpoint":"https://tendo.example/oauth/token","registration_endpoint":"https://tendo.example/oauth/register","revocation_endpoint":"https://tendo.example/oauth/revoke","scopes_supported":["tendo:read","tendo:write"],"response_types_supported":["code"],"response_modes_supported":["query"],"grant_types_supported":["authorization_code","refresh_token"],"token_endpoint_auth_methods_supported":["none"],"revocation_endpoint_auth_methods_supported":["none"],"code_challenge_methods_supported":["S256"],"authorization_response_iss_parameter_supported":true}`
	assertJSONBody(t, w, http.StatusOK, wantAS)
	if w.Header().Get("Access-Control-Expose-Headers") != "Retry-After" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Errorf("cors headers=%v", w.Header())
	}
}

func assertJSONBody(t *testing.T, w *httptest.ResponseRecorder, status int, want string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var got, exp any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &exp); err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(got, exp) {
		t.Fatalf("body=%s\nwant=%s", w.Body, want)
	}
}

func TestRootProtectedResourceIsNotServed(t *testing.T) {
	h := newMCPRouter(t, &fakeMCPService{enabled: true}, mcpPublicURL)
	if w := mcpServe(h, http.MethodGet, "/.well-known/oauth-protected-resource", "", nil, "192.0.2.7:1"); w.Code != http.StatusNotFound {
		t.Fatalf("root PRM status=%d", w.Code)
	}
}

func TestPreflightAnswersOnEachPath(t *testing.T) {
	h := newMCPRouter(t, &fakeMCPService{enabled: true}, mcpPublicURL)
	for path, method := range map[string]string{
		"/oauth/register": "POST",
		"/.well-known/oauth-protected-resource/mcp": "GET",
		"/.well-known/oauth-authorization-server":   "GET",
	} {
		w := mcpServe(h, http.MethodOptions, path, "", http.Header{"Origin": {"https://foreign.example"}, "Access-Control-Request-Method": {method}}, "192.0.2.8:1")
		if w.Code != http.StatusNoContent {
			t.Fatalf("%s preflight status=%d", path, w.Code)
		}
		if w.Header().Get("Access-Control-Allow-Methods") != method+", OPTIONS" || w.Header().Get("Access-Control-Allow-Headers") != "Content-Type" ||
			w.Header().Get("Access-Control-Max-Age") != "600" || w.Header().Get("Access-Control-Allow-Origin") != "*" || w.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatalf("%s headers=%v", path, w.Header())
		}
	}
}

func TestDisabledServiceAnswersUnknownReservedPath404(t *testing.T) {
	h := newMCPRouter(t, &fakeMCPService{enabled: false}, mcpPublicURL)
	unknown := mcpServe(h, http.MethodGet, "/oauth/xyz", "", nil, "192.0.2.9:1")
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown reserved path status=%d", unknown.Code)
	}
	cases := []struct{ method, path string }{
		{http.MethodPost, "/oauth/register"},
		{http.MethodOptions, "/oauth/register"},
		{http.MethodGet, "/.well-known/oauth-protected-resource/mcp"},
		{http.MethodOptions, "/.well-known/oauth-protected-resource/mcp"},
		{http.MethodGet, "/.well-known/oauth-authorization-server"},
		{http.MethodOptions, "/.well-known/oauth-authorization-server"},
	}
	for _, tc := range cases {
		w := mcpServe(h, tc.method, tc.path, validRegistration, jsonHeader(), "192.0.2.9:1")
		if w.Code != unknown.Code || w.Body.String() != unknown.Body.String() || w.Header().Get("Content-Type") != unknown.Header().Get("Content-Type") {
			t.Errorf("%s %s: status=%d body=%s, want %d %s", tc.method, tc.path, w.Code, w.Body, unknown.Code, unknown.Body)
		}
	}
}

func TestMCPCrossOriginRoutesExactList(t *testing.T) {
	got := MCPCrossOriginRoutes()
	want := []httpx.CrossOriginRoute{
		{Method: http.MethodPost, Path: "/oauth/register", Mode: httpx.CrossOriginAny},
		{Method: http.MethodGet, Path: "/.well-known/oauth-protected-resource/mcp", Mode: httpx.CrossOriginAny},
		{Method: http.MethodGet, Path: "/.well-known/oauth-authorization-server", Mode: httpx.CrossOriginAny},
		{Method: http.MethodPost, Path: "/oauth/token", Mode: httpx.CrossOriginAny},
		{Method: http.MethodPost, Path: "/oauth/revoke", Mode: httpx.CrossOriginAny},
		{Method: http.MethodPost, Path: "/mcp", Mode: httpx.CrossOriginNoneOrPublic},
	}
	if len(got) != len(want) {
		t.Fatalf("routes=%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("route %d=%+v want %+v", i, got[i], want[i])
		}
	}
}

func TestRegisterFailureDoesNotExposeErrorText(t *testing.T) {
	svc := &fakeMCPService{enabled: true, err: errors.New("pq: secret driver text")}
	w := mcpServe(newMCPRouter(t, svc, mcpPublicURL), http.MethodPost, "/oauth/register", validRegistration, jsonHeader(), "192.0.2.10:1")
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}
