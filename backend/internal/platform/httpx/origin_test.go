package httpx

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOriginMiddleware(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	cases := []struct {
		name, method, host, origin   string
		remote                       string
		tls                          bool
		headers                      http.Header
		want                         int
		wantScheme, wantHost, wantIP string
	}{
		{name: "safe without origin", method: "GET", host: "example.test", remote: "203.0.113.5:1234", want: http.StatusMisdirectedRequest},
		{name: "safe origin", method: "POST", host: "example.test", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: http.StatusNoContent, wantScheme: "https", wantHost: "example.test", wantIP: "192.0.2.4"},
		{name: "canonical socket tls", method: "GET", host: "example.test", remote: "203.0.113.5:1234", tls: true, want: http.StatusNoContent, wantScheme: "https", wantHost: "example.test", wantIP: "203.0.113.5"},
		{name: "untrusted malformed forwarding ignored over socket tls", method: "GET", host: "example.test", remote: "203.0.113.5:1234", tls: true, headers: http.Header{"Forwarded": {"not valid"}, "X-Forwarded-Proto": {"http,https"}, "X-Forwarded-Host": {"forged.test,example.test"}, "X-Forwarded-For": {"bad,chain"}}, want: http.StatusNoContent, wantScheme: "https", wantHost: "example.test", wantIP: "203.0.113.5"},
		{name: "untrusted forged ignored", method: "POST", host: "example.test", remote: "203.0.113.5:1234", headers: http.Header{"X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}, "Origin": {"https://example.test"}}, want: http.StatusMisdirectedRequest},
		{name: "trusted forwarded", method: "POST", host: "internal", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"for=\"[2001:db8::1]:443\";proto=https;host=example.test"}}, want: http.StatusNoContent, wantScheme: "https", wantHost: "example.test", wantIP: "2001:db8::1"},
		{name: "forwarded quoted bracket IPv6 without port", method: "POST", host: "internal", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"for=\"[2001:db8::1]\";proto=https;host=example.test"}}, want: http.StatusNoContent, wantScheme: "https", wantHost: "example.test", wantIP: "2001:db8::1"},
		{name: "untrusted malformed forwarded ignored", method: "GET", host: "example.test", remote: "203.0.113.5:1234", headers: http.Header{"Forwarded": {"not valid"}, "X-Forwarded-For": {"bad,chain"}}, want: http.StatusMisdirectedRequest},
		{name: "forwarded conflicting client IP rejected", method: "POST", host: "internal", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"for=203.0.113.1;proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}, "X-Forwarded-For": {"203.0.113.2"}}, want: http.StatusBadRequest},
		{name: "forwarded equivalent authority accepted", method: "POST", host: "internal", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=EXAMPLE.test:443"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}}, want: http.StatusNoContent, wantScheme: "https", wantHost: "EXAMPLE.test:443", wantIP: "192.0.2.4"},
		{name: "forwarded-only socket client IP retained", method: "POST", host: "internal", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: http.StatusNoContent, wantScheme: "https", wantHost: "example.test", wantIP: "192.0.2.4"},
		{name: "trusted x forwarded optional for", method: "POST", host: "internal", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}}, want: http.StatusNoContent, wantScheme: "https", wantHost: "example.test", wantIP: "192.0.2.4"},
		{name: "trusted malformed", method: "POST", host: "example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https,host=example.test"}}, want: http.StatusBadRequest},
		{name: "trusted xff only rejected", method: "POST", host: "example.test", remote: "192.0.2.4:1234", headers: http.Header{"X-Forwarded-For": {"203.0.113.8"}}, want: http.StatusBadRequest},
		{name: "forwarded with xff only rejected", method: "POST", host: "internal", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}, "X-Forwarded-For": {"203.0.113.8"}}, want: http.StatusBadRequest},
		{name: "untrusted malformed forwarding ignored", method: "GET", host: "example.test", remote: "203.0.113.5:1234", headers: http.Header{"X-Forwarded-For": {"bad,chain"}}, want: http.StatusMisdirectedRequest},
		{name: "forwarded and matching x", method: "POST", host: "internal", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}, "X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}}, want: http.StatusNoContent, wantScheme: "https", wantHost: "example.test", wantIP: "192.0.2.4"},
		{name: "conflicting x rejected", method: "POST", host: "internal", origin: "https://example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}, "X-Forwarded-Proto": {"http"}, "X-Forwarded-Host": {"example.test"}}, want: http.StatusBadRequest},
		{name: "repeated forwarded", method: "GET", host: "example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test", "proto=https;host=example.test"}}, want: http.StatusBadRequest},
		{name: "duplicate field", method: "GET", host: "example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;proto=https;host=example.test"}}, want: http.StatusBadRequest},
		{name: "malformed quoting", method: "GET", host: "example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=\"https;host=example.test"}}, want: http.StatusBadRequest},
		{name: "x chain", method: "GET", host: "example.test", remote: "192.0.2.4:1234", headers: http.Header{"X-Forwarded-Proto": {"https,http"}, "X-Forwarded-Host": {"example.test"}}, want: http.StatusBadRequest},
		{name: "x invalid client ip", method: "GET", host: "example.test", remote: "192.0.2.4:1234", headers: http.Header{"X-Forwarded-Proto": {"https"}, "X-Forwarded-Host": {"example.test"}, "X-Forwarded-For": {"unknown"}}, want: http.StatusBadRequest},
		{name: "authority mismatch", method: "GET", host: "wrong.test", remote: "203.0.113.5:1234", want: http.StatusMisdirectedRequest},
		{name: "origin credentials", method: "POST", host: "example.test", origin: "https://user@example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: http.StatusForbidden},
		{name: "origin path", method: "POST", host: "example.test", origin: "https://example.test/path", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: http.StatusForbidden},
		{name: "origin empty fragment", method: "POST", host: "example.test", origin: "https://example.test#", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: http.StatusForbidden},
		{name: "origin query", method: "POST", host: "example.test", origin: "https://example.test?x", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: http.StatusForbidden},
		{name: "origin null", method: "POST", host: "example.test", origin: "null", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: http.StatusForbidden},
		{name: "duplicate origin fields", method: "POST", host: "example.test", remote: "192.0.2.4:1234", tls: true, headers: http.Header{"Origin": {"https://example.test", "https://example.test"}}, want: http.StatusForbidden},
		{name: "combined origins", method: "POST", host: "example.test", remote: "192.0.2.4:1234", tls: true, headers: http.Header{"Origin": {"https://example.test, https://example.test"}}, want: http.StatusForbidden},
		{name: "empty origin", method: "POST", host: "example.test", remote: "192.0.2.4:1234", tls: true, headers: http.Header{"Origin": {""}}, want: http.StatusForbidden},
		{name: "unsafe requires origin", method: "POST", host: "example.test", remote: "192.0.2.4:1234", headers: http.Header{"Forwarded": {"proto=https;host=example.test"}}, want: http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				metadata, ok := MetadataFromRequest(r)
				if !ok || metadata.Scheme != tc.wantScheme || metadata.Host != tc.wantHost || metadata.ClientIP != tc.wantIP {
					t.Errorf("metadata=%+v present=%v", metadata, ok)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			wrapped := requestMiddleware(originMiddlewareConfig(mustOriginConfig(t), nil)(probe), slog.Default())
			req := httptest.NewRequest(tc.method, "http://"+tc.host+"/api/v1/items", nil)
			req.Host = tc.host
			req.RemoteAddr = tc.remote
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			for key, values := range tc.headers {
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}
			w := httptest.NewRecorder()
			wrapped.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.want != http.StatusNoContent && called {
				t.Fatal("downstream handler called for rejected request")
			}
			if tc.want == http.StatusNoContent && !called {
				t.Fatal("handler not called")
			}
			if tc.want != http.StatusNoContent {
				assertProblemResponse(t, w)
				if w.Header().Get("X-Request-ID") == "" {
					t.Fatal("request id missing")
				}
			}
		})
	}
	if !strings.Contains(logs.String(), "request_id") || !strings.Contains(logs.String(), "status=400") || !strings.Contains(logs.String(), "status=403") || !strings.Contains(logs.String(), "status=421") {
		t.Fatalf("missing result evidence in logs: %s", logs.String())
	}
	if strings.Contains(logs.String(), "example.test") || strings.Contains(logs.String(), "internal") || strings.Contains(logs.String(), "Forwarded") {
		t.Fatalf("request log leaked host or header data: %s", logs.String())
	}
}

func mustOriginConfig(t *testing.T) originConfig {
	t.Helper()
	c, err := newOriginConfig(OriginPolicy{PublicURL: "https://example.test", TrustedProxyCIDRs: []string{"192.0.2.0/24"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func assertProblemResponse(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
		t.Fatalf("headers=%v", w.Header())
	}
	var problem Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || problem.Status != w.Code || problem.Type != "about:blank" || problem.Title != map[int]string{400: "Bad Request", 403: "Forbidden", 421: "Misdirected Request"}[w.Code] {
		t.Fatalf("problem=%+v err=%v body=%s", problem, err, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "example.test") {
		t.Fatalf("response leaked host: %s", w.Body.String())
	}
}

func TestNewAppCanonicalIPv6Origin(t *testing.T) {
	app := NewApp(&testPinger{}, time.Second, make(chan struct{}), OriginPolicy{PublicURL: "https://[2001:db8::1]"})
	for _, tc := range []struct {
		name, host, origin string
		want               int
	}{
		{name: "expanded socket authority and canonical browser origin", host: "[2001:0DB8:0:0:0:0:0:1]:443", origin: "https://[2001:db8::1]", want: http.StatusNotFound},
		{name: "different IPv6 authority", host: "[2001:db8::2]", origin: "https://[2001:db8::1]", want: http.StatusMisdirectedRequest},
		{name: "different IPv6 browser origin", host: "[2001:db8::1]", origin: "https://[2001:db8::2]", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "https://placeholder/unknown", nil)
			r.Host = tc.host
			r.TLS = &tls.ConnectionState{}
			r.RemoteAddr = "203.0.113.5:1234"
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestNewAppAndHealthOriginExemption(t *testing.T) {
	for _, path := range []string{"/health/live", "/health/ready"} {
		t.Run(path, func(t *testing.T) {
			app := NewApp(&testPinger{}, time.Second, make(chan struct{}), OriginPolicy{PublicURL: "https://example.test", TrustedProxyCIDRs: []string{"203.0.113.0/24"}})
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Host = "untrusted.example"
			r.RemoteAddr = "203.0.113.1:1234"
			r.Header.Set("Origin", "https://foreign.example")
			r.Header.Set("Forwarded", "not valid")
			r.Header.Set("X-Forwarded-For", "bad,chain")
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
				t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
			}
			var response StatusResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Status != "ok" {
				t.Fatalf("response=%+v err=%v", response, err)
			}
		})
	}
	r := httptest.NewRequest(http.MethodGet, "/health/live/", nil)
	r.Host = "example.test"
	r.RemoteAddr = "203.0.113.1:1234"
	w := httptest.NewRecorder()
	NewApp(nil, time.Second, make(chan struct{}), OriginPolicy{PublicURL: "https://example.test"}).ServeHTTP(w, r)
	assertProblemResponse(t, w)
	var problem Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || w.Code != http.StatusMisdirectedRequest || problem.Type != "about:blank" || problem.Title != "Misdirected Request" || problem.Status != http.StatusMisdirectedRequest {
		t.Fatalf("health response status=%d problem=%+v err=%v", w.Code, problem, err)
	}
}
