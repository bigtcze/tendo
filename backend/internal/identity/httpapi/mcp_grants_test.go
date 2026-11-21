package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

const testGrantID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"

// fakeGrantService records the calls and answers the configured outcome.
type fakeGrantService struct {
	page       identity.MCPGrantPage
	listErr    error
	revokeErr  error
	listCalls  int
	revokeIDs  []string
	revokeUser []string
	gotCursor  string
	gotLimit   int
}

func (f *fakeGrantService) ListGrants(_ context.Context, _ string, limit int, cursor string) (identity.MCPGrantPage, error) {
	f.listCalls++
	f.gotLimit, f.gotCursor = limit, cursor
	return f.page, f.listErr
}

func (f *fakeGrantService) RevokeGrantForUser(_ context.Context, userID, grantID string) error {
	f.revokeUser = append(f.revokeUser, userID)
	f.revokeIDs = append(f.revokeIDs, grantID)
	return f.revokeErr
}

// grantRouter mounts the connected-apps routes exactly as composition does, with
// the disabled service: it must work with MCP off.
func grantRouter(service mcpGrantService, sessions *fakeSessionService, publicURL string) http.Handler {
	routes := chi.NewRouter()
	NewMCPGrants(service, NewSession(sessions, publicURL)).Register(routes)
	return httpx.NewAppWithRoutes(nil, time.Second, make(chan struct{}), httpx.OriginPolicy{PublicURL: mcpPublicURL}, func(r chi.Router) { r.Mount("/", routes) })
}

func grantRequest(method, target, origin string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Cookie", sessionCookieHeader)
	return r
}

func TestConnectedAppsRequireSessionCookie(t *testing.T) {
	svc := &fakeGrantService{}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	for _, tc := range []struct{ method, target string }{
		{http.MethodGet, "/api/v1/mcp/grants"},
		{http.MethodDelete, "/api/v1/mcp/grants/" + testGrantID},
	} {
		r := httptest.NewRequest(tc.method, mcpPublicURL+tc.target, nil)
		r.Header.Set("Origin", mcpPublicURL)
		// A bearer token is not a session: it never authorizes these routes.
		r.Header.Set("Authorization", "Bearer "+strings.Repeat("A", 43))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s bearer-only status = %d, want 401", tc.method, tc.target, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s 401 lacks no-store", tc.method, tc.target)
		}
	}
	if svc.listCalls != 0 || len(svc.revokeIDs) != 0 {
		t.Fatalf("service reached without a session: list=%d revoke=%v", svc.listCalls, svc.revokeIDs)
	}
}

func TestConnectedAppsListShapeAndNoStore(t *testing.T) {
	expires := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	svc := &fakeGrantService{page: identity.MCPGrantPage{Items: []identity.MCPGrantItem{{
		ID: testGrantID, ClientName: "Notes app", Scopes: []string{"tendo:read"},
		CreatedAt: expires.Add(-time.Hour), ExpiresAt: expires,
	}}, NextCursor: "g1NEXT"}}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, grantRequest(http.MethodGet, mcpPublicURL+"/api/v1/mcp/grants", mcpPublicURL))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", w.Code, w.Header().Get("Cache-Control"), w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	item := body["items"].([]any)[0].(map[string]any)
	if item["id"] != testGrantID || item["clientName"] != "Notes app" || item["clientNameVerified"] != false {
		t.Fatalf("item = %v", item)
	}
	if _, ok := item["token"]; ok {
		t.Fatal("a token field was returned")
	}
	if body["nextCursor"] != "g1NEXT" {
		t.Fatalf("nextCursor = %v", body["nextCursor"])
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS header on a credentialed route")
	}
}

func TestConnectedAppsEmptyListIsNonNullAndNullCursor(t *testing.T) {
	h := grantRouter(&fakeGrantService{}, signedIn(), mcpPublicURL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, grantRequest(http.MethodGet, mcpPublicURL+"/api/v1/mcp/grants", mcpPublicURL))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"items":[],"nextCursor":null}` {
		t.Fatalf("status=%d body=%s; want the empty page", w.Code, w.Body)
	}
}

func TestConnectedAppsListBadQueryIsInvalidQuery(t *testing.T) {
	for _, target := range []string{"?limit=0", "?limit=101", "?limit=x", "?cursor=", "?cursor=a&cursor=b"} {
		svc := &fakeGrantService{}
		h := grantRouter(svc, signedIn(), mcpPublicURL)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, grantRequest(http.MethodGet, mcpPublicURL+"/api/v1/mcp/grants"+target, mcpPublicURL))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"invalid_query"`) {
			t.Errorf("%s: status=%d body=%s", target, w.Code, w.Body)
		}
		if svc.listCalls != 0 {
			t.Errorf("%s reached the service", target)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s 400 lacks no-store", target)
		}
	}
}

func TestConnectedAppsListInvalidCursorIs400(t *testing.T) {
	svc := &fakeGrantService{listErr: identity.ErrMCPInvalidCursor}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, grantRequest(http.MethodGet, mcpPublicURL+"/api/v1/mcp/grants?cursor=forged", mcpPublicURL))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"parameter":"cursor"`) {
		t.Fatalf("status=%d body=%s; want invalid_query naming cursor", w.Code, w.Body)
	}
}

func TestConnectedAppsListFaultIs503WithoutDriverText(t *testing.T) {
	svc := &fakeGrantService{listErr: errors.New("pq: connection refused at 10.0.0.9")}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, grantRequest(http.MethodGet, mcpPublicURL+"/api/v1/mcp/grants", mcpPublicURL))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	if strings.Contains(w.Body.String(), "10.0.0.9") || strings.Contains(w.Body.String(), "pq:") {
		t.Fatalf("driver text leaked: %s", w.Body)
	}
}

func TestConnectedAppsRevokeNotFoundIsNonOracle(t *testing.T) {
	svc := &fakeGrantService{revokeErr: identity.ErrNotFound}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	for _, id := range []string{testGrantID, "not-a-uuid"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, grantRequest(http.MethodDelete, mcpPublicURL+"/api/v1/mcp/grants/"+id, mcpPublicURL))
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"mcp_grant_not_found"`) {
			t.Fatalf("id %s: status=%d body=%s", id, w.Code, w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("id %s 404 lacks no-store", id)
		}
	}
}

func TestConnectedAppsRevokeSuccessIs204NoStore(t *testing.T) {
	svc := &fakeGrantService{}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, grantRequest(http.MethodDelete, mcpPublicURL+"/api/v1/mcp/grants/"+testGrantID, mcpPublicURL))
	if w.Code != http.StatusNoContent || w.Header().Get("Cache-Control") != "no-store" || w.Body.Len() != 0 {
		t.Fatalf("status=%d cache=%q body=%q", w.Code, w.Header().Get("Cache-Control"), w.Body)
	}
	if len(svc.revokeIDs) != 1 || svc.revokeIDs[0] != testGrantID || svc.revokeUser[0] != "user-1" {
		t.Fatalf("revoke calls: ids=%v users=%v", svc.revokeIDs, svc.revokeUser)
	}
}

func TestConnectedAppsRevokeUnauthenticatedClearsCookie(t *testing.T) {
	svc := &fakeGrantService{revokeErr: identity.ErrUnauthenticated}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, grantRequest(http.MethodDelete, mcpPublicURL+"/api/v1/mcp/grants/"+testGrantID, mcpPublicURL))
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), `"unauthenticated"`) {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "__Host-tendo_session=;") {
		t.Fatalf("session cookie not cleared: %q", w.Header().Get("Set-Cookie"))
	}
}

func TestConnectedAppsRevokeFaultIs503NoStore(t *testing.T) {
	svc := &fakeGrantService{revokeErr: errors.New("driver failure")}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, grantRequest(http.MethodDelete, mcpPublicURL+"/api/v1/mcp/grants/"+testGrantID, mcpPublicURL))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q", w.Code, w.Header().Get("Cache-Control"))
	}
	if strings.Contains(w.Body.String(), "driver failure") {
		t.Fatal("driver text leaked")
	}
}

// The per-user bucket allows 30 revokes a minute; the 31st is refused before
// any service call, with Retry-After 60.
func TestConnectedAppsRevokeRateLimitBeforeService(t *testing.T) {
	svc := &fakeGrantService{}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	for i := 0; i < mcpGrantRevokePerUser; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, grantRequest(http.MethodDelete, mcpPublicURL+"/api/v1/mcp/grants/"+testGrantID, mcpPublicURL))
		if w.Code != http.StatusNoContent {
			t.Fatalf("call %d status=%d", i+1, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, grantRequest(http.MethodDelete, mcpPublicURL+"/api/v1/mcp/grants/"+testGrantID, mcpPublicURL))
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("31st call status=%d retry=%q", w.Code, w.Header().Get("Retry-After"))
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("429 lacks no-store")
	}
	if len(svc.revokeIDs) != mcpGrantRevokePerUser {
		t.Fatalf("service calls = %d, want %d", len(svc.revokeIDs), mcpGrantRevokePerUser)
	}
}

// The request log carries only the route template: neither the grant id nor the
// token reaches it, on the fault path that records a class.
func TestConnectedAppsLogCarriesNoGrantID(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := &fakeGrantService{revokeErr: errors.New("boom")}
	routes := chi.NewRouter()
	NewMCPGrants(svc, NewSession(signedIn(), mcpPublicURL)).Register(routes)
	app := httpx.NewAppWithLogger(nil, time.Second, make(chan struct{}), httpx.OriginPolicy{PublicURL: mcpPublicURL}, func(r chi.Router) { r.Mount("/", routes) }, logger)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, grantRequest(http.MethodDelete, mcpPublicURL+"/api/v1/mcp/grants/"+testGrantID, mcpPublicURL))
	out := logs.String()
	if out == "" {
		t.Fatal("no request log was written")
	}
	if strings.Contains(out, testGrantID) || strings.Contains(out, fakeToken) {
		t.Fatalf("log leaks the grant id or session token:\n%s", out)
	}
	if !strings.Contains(out, "route=/api/v1/mcp/grants/{grantId}") {
		t.Fatalf("log lacks the route template:\n%s", out)
	}
}

// The handler's only service dependency is the narrow connected-apps surface,
// which has no Enabled(). A disabled service still serves these routes.
func TestConnectedAppsServeWhileServiceReportsNoMCP(t *testing.T) {
	svc := &fakeGrantService{page: identity.MCPGrantPage{}}
	h := grantRouter(svc, signedIn(), mcpPublicURL)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, grantRequest(http.MethodGet, mcpPublicURL+"/api/v1/mcp/grants", mcpPublicURL))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with MCP reported off", w.Code)
	}
}
