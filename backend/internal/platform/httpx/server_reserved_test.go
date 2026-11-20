package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"github.com/go-chi/chi/v5"
)

func TestIsReservedPathBoundaries(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/api", true}, {"/api/v1/x", true}, {"/apix", false},
		{"/health", true}, {"/health/live/", true}, {"/healthz", false},
		{"/mcp", true}, {"/mcp/", true}, {"/mcp/tools/call", true}, {"/mcpx", false}, {"/mcp-x", false},
		{"/oauth", true}, {"/oauth/register", true}, {"/oauth/authorize", true}, {"/oauth/", true},
		{"/oauthy", false}, {"/OAUTH/register", false},
		{"/.well-known", true}, {"/.well-known/oauth-authorization-server", true}, {"/.well-known/", true},
		{"/.well-knownz", false},
		{"/", false}, {"/some/route", false},
	} {
		if got := isReservedPath(tc.path); got != tc.want {
			t.Errorf("isReservedPath(%q)=%v want %v", tc.path, got, tc.want)
		}
	}
}

// TestReservedPathsNeverFallBackToUI runs with a UI fallback present. Every
// unmatched request under a reserved prefix must answer a problem+json 404/405
// and never reach the SPA handler.
func TestReservedPathsNeverFallBackToUI(t *testing.T) {
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ui-index"))
	})
	register := func(r chi.Router) {
		r.Get("/oauth/register", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	}
	app := NewAppWithUI(&testPinger{}, time.Second, make(chan struct{}), OriginPolicy{PublicURL: "http://example.test"}, register, ui)
	serve := func(method, target string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		r.Host = "example.test"
		r.Header.Set("Origin", "http://example.test")
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w
	}
	assertProblem := func(t *testing.T, w *httptest.ResponseRecorder, code int) {
		t.Helper()
		if w.Code != code || w.Header().Get("Content-Type") != "application/problem+json" || strings.Contains(w.Body.String(), "ui-index") {
			t.Fatalf("status=%d ct=%q body=%s", w.Code, w.Header().Get("Content-Type"), w.Body)
		}
		var problem apimodel.Problem
		if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || problem.Status != code {
			t.Fatalf("problem=%+v err=%v", problem, err)
		}
	}

	// Unregistered reserved paths: GET, POST and DELETE all answer 404 problem+json.
	for _, path := range []string{
		"/mcp", "/mcp/", "/mcp/tools/call",
		"/oauth", "/oauth/authorize", "/oauth/register/",
		"/.well-known", "/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-authorization-server",
	} {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			t.Run(method+" "+path, func(t *testing.T) {
				assertProblem(t, serve(method, path), http.StatusNotFound)
			})
		}
	}

	// A registered GET route makes other methods on the same path 405 problem+json, not the SPA.
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method+" /oauth/register 405", func(t *testing.T) {
			assertProblem(t, serve(method, "/oauth/register"), http.StatusMethodNotAllowed)
		})
	}
	if w := serve(http.MethodGet, "/oauth/register"); w.Code != http.StatusNoContent {
		t.Fatalf("registered GET status=%d body=%s", w.Code, w.Body)
	}

	// Prefix boundary: segment-adjacent names are not reserved and still reach the SPA.
	for _, path := range []string{"/mcpx", "/mcp-x/y", "/oauthy", "/oauthy/register", "/.well-knownz", "/.well-knownz/a"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			t.Run(method+" "+path+" spa", func(t *testing.T) {
				w := serve(method, path)
				if w.Code != http.StatusTeapot || w.Body.String() != "ui-index" {
					t.Fatalf("status=%d body=%s", w.Code, w.Body)
				}
			})
		}
	}
}
