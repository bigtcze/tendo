package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// TestOIDCRoutesOriginRejectionMatchesContractFixture pins the 403 envelope used by
// api/oidc-middleware.test.mjs to the production middleware output for every OIDC route.
func TestOIDCRoutesOriginRejectionMatchesContractFixture(t *testing.T) {
	reached := false
	ok := func(w http.ResponseWriter, _ *http.Request) { reached = true; w.WriteHeader(http.StatusOK) }
	app := NewAppWithRoutes(&testPinger{}, time.Second, make(chan struct{}), OriginPolicy{PublicURL: "https://tendo.test"}, func(r chi.Router) {
		r.Get("/api/v1/auth/oidc", ok)
		r.Get("/api/v1/auth/oidc/identity", ok)
		r.Post("/api/v1/auth/oidc/start", ok)
		r.Get("/api/v1/auth/oidc/callback", ok)
	})
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/auth/oidc"},
		{http.MethodGet, "/api/v1/auth/oidc/identity"},
		{http.MethodPost, "/api/v1/auth/oidc/start"},
		{http.MethodGet, "/api/v1/auth/oidc/callback"},
	}
	origins := map[string][]string{
		"foreign":    {"https://foreign.example"},
		"duplicated": {"https://tendo.test", "https://tendo.test"},
		"malformed":  {"https://tendo.test/path"},
	}
	for _, route := range routes {
		for name, values := range origins {
			t.Run(route.method+" "+route.path+" "+name, func(t *testing.T) {
				reached = false
				r := httptest.NewRequest(route.method, "https://tendo.test"+route.path, nil)
				r.Header["Origin"] = values
				w := httptest.NewRecorder()
				app.ServeHTTP(w, r)
				if w.Code != http.StatusForbidden || reached {
					t.Fatalf("status=%d reached=%v", w.Code, reached)
				}
				if got := w.Header().Get("Content-Type"); got != "application/problem+json" {
					t.Fatalf("content-type=%q", got)
				}
				if got := w.Header().Get("Cache-Control"); got != "no-store" {
					t.Fatalf("cache-control=%q", got)
				}
				var body map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				// Must equal the fixture body in api/oidc-middleware.test.mjs exactly.
				want := map[string]any{"type": "about:blank", "title": "Forbidden", "status": float64(403)}
				if len(body) != len(want) {
					t.Fatalf("body=%v want=%v", body, want)
				}
				for k, v := range want {
					if body[k] != v {
						t.Fatalf("body=%v want=%v", body, want)
					}
				}
			})
		}
	}
}
