package httpx

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

// crossOriginPolicy is the policy used by the CrossOrigin tests. The public
// origin and the trusted proxy match mustOriginConfig.
func crossOriginPolicy(routes ...CrossOriginRoute) OriginPolicy {
	return OriginPolicy{PublicURL: "https://example.test", TrustedProxyCIDRs: []string{"192.0.2.0/24"}, CrossOrigin: routes}
}

// runOrigin sends one request through originMiddlewareConfig and reports the
// status and whether the downstream handler was reached. If the policy is
// invalid, newOriginConfig returns err and the middleware fails closed.
func runOrigin(t *testing.T, policy OriginPolicy, method, target, host string, headers http.Header) (int, bool) {
	t.Helper()
	return runOriginFrom(t, policy, "203.0.113.4:1234", method, target, host, headers)
}

// runOriginFrom is runOrigin with an explicit socket peer. A peer inside the
// trusted proxy CIDR (192.0.2.0/24) lets forwarded headers apply.
func runOriginFrom(t *testing.T, policy OriginPolicy, remote, method, target, host string, headers http.Header) (int, bool) {
	t.Helper()
	config, err := newOriginConfig(policy)
	called := false
	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	r := httptest.NewRequest(method, "https://example.test"+target, nil)
	r.Host = host
	r.TLS = &tls.ConnectionState{}
	r.RemoteAddr = remote
	for key, values := range headers {
		for _, value := range values {
			r.Header.Add(key, value)
		}
	}
	w := httptest.NewRecorder()
	originMiddlewareConfig(config, err)(probe).ServeHTTP(w, r)
	return w.Code, called
}

var foreignOrigin = http.Header{"Origin": {"https://foreign.example"}}

func TestCrossOriginAnyExemptsOnlyListedMethodAndPath(t *testing.T) {
	policy := crossOriginPolicy(
		CrossOriginRoute{Method: http.MethodPost, Path: "/oauth/register", Mode: CrossOriginAny},
		CrossOriginRoute{Method: http.MethodGet, Path: "/.well-known/oauth-authorization-server", Mode: CrossOriginAny},
		CrossOriginRoute{Method: http.MethodDelete, Path: "/mcp/grant", Mode: CrossOriginAny},
	)
	for _, tc := range []struct {
		name, method, target string
		headers              http.Header
		wantStatus           int
		wantCalled           bool
	}{
		{"listed POST foreign origin", http.MethodPost, "/oauth/register", foreignOrigin, http.StatusNoContent, true},
		{"listed POST no origin", http.MethodPost, "/oauth/register", nil, http.StatusNoContent, true},
		// Any Origin is ignored, so duplicated values on a listed route pass too.
		// CrossOriginAny skips the Origin check entirely.
		{"listed POST duplicated origin", http.MethodPost, "/oauth/register", http.Header{"Origin": {"https://foreign.example", "https://example.test"}}, http.StatusNoContent, true},
		{"listed GET foreign origin", http.MethodGet, "/.well-known/oauth-authorization-server", foreignOrigin, http.StatusNoContent, true},
		{"listed DELETE foreign origin", http.MethodDelete, "/mcp/grant", foreignOrigin, http.StatusNoContent, true},
		{"OPTIONS listed path foreign origin", http.MethodOptions, "/oauth/register", foreignOrigin, http.StatusNoContent, true},
		{"OPTIONS listed GET path foreign origin", http.MethodOptions, "/.well-known/oauth-authorization-server", foreignOrigin, http.StatusNoContent, true},
		// Same listed path, different method: no exemption.
		{"DELETE on POST-only path foreign origin", http.MethodDelete, "/oauth/register", foreignOrigin, http.StatusForbidden, false},
		{"PUT on POST-only path foreign origin", http.MethodPut, "/oauth/register", foreignOrigin, http.StatusForbidden, false},
		{"POST on GET-only path foreign origin", http.MethodPost, "/.well-known/oauth-authorization-server", foreignOrigin, http.StatusForbidden, false},
		// Unlisted routes keep the existing check.
		{"unlisted POST foreign origin", http.MethodPost, "/oauth/other", foreignOrigin, http.StatusForbidden, false},
		{"unlisted POST no origin", http.MethodPost, "/oauth/other", nil, http.StatusForbidden, false},
		{"OPTIONS unlisted path foreign origin", http.MethodOptions, "/oauth/other", foreignOrigin, http.StatusForbidden, false},
		// Path variants are not exempt: matching is exact and uncleaned.
		{"trailing slash variant", http.MethodPost, "/oauth/register/", foreignOrigin, http.StatusForbidden, false},
		{"case variant", http.MethodPost, "/OAUTH/register", foreignOrigin, http.StatusForbidden, false},
		{"double slash variant", http.MethodPost, "/oauth//register", foreignOrigin, http.StatusForbidden, false},
		{"percent-encoded slash", http.MethodPost, "/oauth%2Fregister", foreignOrigin, http.StatusForbidden, false},
		{"percent-encoded letter", http.MethodPost, "/oauth/%72egister", foreignOrigin, http.StatusForbidden, false},
		{"dot segment", http.MethodPost, "/oauth/x/../register", foreignOrigin, http.StatusForbidden, false},
		{"prefix extension", http.MethodPost, "/oauth/register/extra", foreignOrigin, http.StatusForbidden, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, called := runOrigin(t, policy, tc.method, tc.target, "example.test", tc.headers)
			if status != tc.wantStatus || called != tc.wantCalled {
				t.Fatalf("status=%d called=%v, want status=%d called=%v", status, called, tc.wantStatus, tc.wantCalled)
			}
		})
	}
}

// The canonical-authority (421) and trusted-forwarded (400) checks run before
// the Origin check, so listed routes keep them.
func TestCrossOriginListedRouteKeepsAuthorityAndForwardedChecks(t *testing.T) {
	policy := crossOriginPolicy(CrossOriginRoute{Method: http.MethodPost, Path: "/oauth/register", Mode: CrossOriginAny})
	const target = "/oauth/register"
	for _, tc := range []struct {
		name, remote, method, host string
		headers                    http.Header
		want                       int
	}{
		{"wrong host", "203.0.113.4:1234", http.MethodPost, "wrong.example", foreignOrigin, http.StatusMisdirectedRequest},
		{"wrong host OPTIONS", "203.0.113.4:1234", http.MethodOptions, "wrong.example", foreignOrigin, http.StatusMisdirectedRequest},
		{"wrong host no origin", "203.0.113.4:1234", http.MethodPost, "wrong.example", nil, http.StatusMisdirectedRequest},
		// Untrusted peer: forwarded headers are ignored, so the wrong host stays wrong.
		{"forged forwarded untrusted peer", "203.0.113.4:1234", http.MethodPost, "wrong.example", http.Header{"Forwarded": {"proto=https;host=example.test"}}, http.StatusMisdirectedRequest},
		// Trusted peer: malformed forwarding is still rejected before the Origin exemption.
		{"malformed forwarded from trusted peer", "192.0.2.4:1234", http.MethodPost, "example.test", http.Header{"Forwarded": {"bad"}, "Origin": {"https://foreign.example"}}, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, called := runOriginFrom(t, policy, tc.remote, tc.method, target, tc.host, tc.headers)
			if status != tc.want || called {
				t.Fatalf("status=%d called=%v want status=%d and not called", status, called, tc.want)
			}
		})
	}
}

func TestCrossOriginNoneOrPublic(t *testing.T) {
	policy := crossOriginPolicy(CrossOriginRoute{Method: http.MethodPost, Path: "/oauth/register", Mode: CrossOriginNoneOrPublic})
	for _, tc := range []struct {
		name, method, target string
		headers              http.Header
		wantStatus           int
		wantCalled           bool
	}{
		{"absent origin allowed", http.MethodPost, "/oauth/register", nil, http.StatusNoContent, true},
		{"public origin allowed", http.MethodPost, "/oauth/register", http.Header{"Origin": {"https://example.test"}}, http.StatusNoContent, true},
		{"foreign origin rejected", http.MethodPost, "/oauth/register", foreignOrigin, http.StatusForbidden, false},
		{"duplicated public origin rejected", http.MethodPost, "/oauth/register", http.Header{"Origin": {"https://example.test", "https://example.test"}}, http.StatusForbidden, false},
		{"null origin rejected", http.MethodPost, "/oauth/register", http.Header{"Origin": {"null"}}, http.StatusForbidden, false},
		// OPTIONS is not implied for NoneOrPublic.
		{"OPTIONS foreign origin rejected", http.MethodOptions, "/oauth/register", foreignOrigin, http.StatusForbidden, false},
		{"other method foreign origin rejected", http.MethodDelete, "/oauth/register", foreignOrigin, http.StatusForbidden, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, called := runOrigin(t, policy, tc.method, tc.target, "example.test", tc.headers)
			if status != tc.wantStatus || called != tc.wantCalled {
				t.Fatalf("status=%d called=%v, want status=%d called=%v", status, called, tc.wantStatus, tc.wantCalled)
			}
		})
	}
}

func TestCrossOriginPolicyValidation(t *testing.T) {
	valid := []CrossOriginRoute{
		{Method: http.MethodPost, Path: "/oauth/register", Mode: CrossOriginAny},
		{Method: http.MethodGet, Path: "/.well-known/oauth-authorization-server", Mode: CrossOriginAny},
		{Method: http.MethodDelete, Path: "/mcp/grant/", Mode: CrossOriginNoneOrPublic},
	}
	if _, err := newOriginConfig(crossOriginPolicy(valid...)); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	for _, tc := range []struct {
		name  string
		route CrossOriginRoute
	}{
		{"relative path", CrossOriginRoute{Method: "POST", Path: "oauth/register", Mode: CrossOriginAny}},
		{"empty path", CrossOriginRoute{Method: "POST", Path: "", Mode: CrossOriginAny}},
		{"wildcard", CrossOriginRoute{Method: "POST", Path: "/oauth/*", Mode: CrossOriginAny}},
		{"query", CrossOriginRoute{Method: "POST", Path: "/oauth/register?x=1", Mode: CrossOriginAny}},
		{"fragment", CrossOriginRoute{Method: "POST", Path: "/oauth/register#f", Mode: CrossOriginAny}},
		{"template", CrossOriginRoute{Method: "POST", Path: "/oauth/{id}", Mode: CrossOriginAny}},
		{"percent", CrossOriginRoute{Method: "POST", Path: "/oauth/%72egister", Mode: CrossOriginAny}},
		{"space", CrossOriginRoute{Method: "POST", Path: "/oauth/reg ister", Mode: CrossOriginAny}},
		{"control", CrossOriginRoute{Method: "POST", Path: "/oauth/register\n", Mode: CrossOriginAny}},
		{"method PUT", CrossOriginRoute{Method: http.MethodPut, Path: "/oauth/register", Mode: CrossOriginAny}},
		{"method PATCH", CrossOriginRoute{Method: http.MethodPatch, Path: "/oauth/register", Mode: CrossOriginAny}},
		{"method OPTIONS listed", CrossOriginRoute{Method: http.MethodOptions, Path: "/oauth/register", Mode: CrossOriginAny}},
		{"method HEAD", CrossOriginRoute{Method: http.MethodHead, Path: "/oauth/register", Mode: CrossOriginAny}},
		{"method lowercase", CrossOriginRoute{Method: "post", Path: "/oauth/register", Mode: CrossOriginAny}},
		{"empty method", CrossOriginRoute{Method: "", Path: "/oauth/register", Mode: CrossOriginAny}},
		{"zero mode", CrossOriginRoute{Method: http.MethodPost, Path: "/oauth/register"}},
		{"unknown mode", CrossOriginRoute{Method: http.MethodPost, Path: "/oauth/register", Mode: 99}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newOriginConfig(crossOriginPolicy(tc.route)); err == nil {
				t.Fatalf("policy %+v accepted", tc.route)
			}
		})
	}
	t.Run("duplicate same method and path", func(t *testing.T) {
		_, err := newOriginConfig(crossOriginPolicy(
			CrossOriginRoute{Method: http.MethodPost, Path: "/oauth/register", Mode: CrossOriginAny},
			CrossOriginRoute{Method: http.MethodPost, Path: "/oauth/register", Mode: CrossOriginNoneOrPublic},
		))
		if err == nil {
			t.Fatal("duplicate accepted")
		}
	})
	t.Run("invalid entry fails closed with 421 on every request", func(t *testing.T) {
		policy := crossOriginPolicy(
			CrossOriginRoute{Method: http.MethodPost, Path: "/oauth/register", Mode: CrossOriginAny},
			CrossOriginRoute{Method: http.MethodPost, Path: "/oauth/*", Mode: CrossOriginAny},
		)
		for _, tc := range []struct{ method, target, origin string }{
			{http.MethodPost, "/oauth/register", "https://foreign.example"},
			{http.MethodPost, "/oauth/register", ""},
			{http.MethodGet, "/api/v1/anything", ""},
		} {
			headers := http.Header{}
			if tc.origin != "" {
				headers.Set("Origin", tc.origin)
			}
			status, called := runOrigin(t, policy, tc.method, tc.target, "example.test", headers)
			if status != http.StatusMisdirectedRequest || called {
				t.Fatalf("%s %s status=%d called=%v", tc.method, tc.target, status, called)
			}
		}
	})
}
