package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// Slice B1b3 of #457: the administrator grant routes.

const (
	adminGrantsURL = "/api/v1/admin/accounts/" + policyTarget + "/mcp-grants"
	adminGrantURL  = adminGrantsURL + "/" + testGrantID
)

func (f *fakeMCPPolicyService) AccountGrants(_ context.Context, actor, user string, limit int, cursor string) (identity.MCPGrantPage, error) {
	f.grantCalls++
	f.grantActor, f.grantUser, f.grantLimit, f.grantCursor = actor, user, limit, cursor
	return f.grantPage, f.grantErr
}

func (f *fakeMCPPolicyService) RevokeAccountGrant(_ context.Context, actor, user, grantID string) error {
	f.grantCalls++
	f.grantActor, f.grantUser, f.grantID = actor, user, grantID
	return f.grantErr
}

func (f *fakeMCPPolicyService) RevokeAccountGrants(_ context.Context, actor, user string) error {
	f.grantCalls++
	f.grantActor, f.grantUser = actor, user
	return f.grantErr
}

func adminGrantCalls() []policyCall {
	return []policyCall{
		{method: http.MethodGet, target: adminGrantsURL},
		{method: http.MethodDelete, target: adminGrantURL},
		{method: http.MethodDelete, target: adminGrantsURL},
	}
}

func TestMCPAdminGrantRoutesRequireSessionCookie(t *testing.T) {
	svc := &fakeMCPPolicyService{}
	h := policyRouter(svc, signedIn())
	for _, c := range adminGrantCalls() {
		c.authorization = "Bearer " + strings.Repeat("A", 43)
		if w := policyServe(h, c); w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s bearer-only: status=%d cache=%q", c.method, c.target, w.Code, w.Header().Get("Cache-Control"))
		}
	}
	if svc.total() != 0 {
		t.Fatalf("service reached without a session cookie: %d calls", svc.total())
	}
}

// TestMCPAdminGrantRoutesAdminCheckRunsFirst: a non-administrator reads 403
// before the query is parsed, the rate limit is charged or the command runs.
func TestMCPAdminGrantRoutesAdminCheckRunsFirst(t *testing.T) {
	svc := &fakeMCPPolicyService{adminErr: identity.ErrInstanceAdminRequired}
	h := policyRouter(svc, signedIn())
	calls := adminGrantCalls()
	calls[0].target += "?limit=bogus"
	for _, c := range calls {
		c.cookie = sessionCookieHeader
		assertProblem(t, policyServe(h, c), http.StatusForbidden, "instance_admin_required")
	}
	if svc.grantCalls != 0 || svc.adminCalls != 3 {
		t.Fatalf("grant=%d admin=%d; the command must not run", svc.grantCalls, svc.adminCalls)
	}
}

// TestMCPAdminGrantDeletesRequireCanonicalOrigin: a foreign or missing Origin
// is refused before the session or the service.
func TestMCPAdminGrantDeletesRequireCanonicalOrigin(t *testing.T) {
	svc := &fakeMCPPolicyService{}
	h := policyRouter(svc, signedIn())
	for _, c := range adminGrantCalls()[1:] {
		c.cookie = sessionCookieHeader
		for _, origin := range []string{"https://evil.example", ""} {
			r := c.request()
			if origin == "" {
				r.Header.Del("Origin")
			} else {
				r.Header.Set("Origin", origin)
			}
			if w := serve(h, r); w.Code != http.StatusForbidden {
				t.Errorf("%s %s Origin %q: status=%d", c.method, c.target, origin, w.Code)
			}
		}
	}
	if svc.total() != 0 {
		t.Fatalf("a foreign or missing Origin reached the service: %d calls", svc.total())
	}
}

func TestMCPAdminGrantListShapeAndArguments(t *testing.T) {
	expires := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	svc := &fakeMCPPolicyService{grantPage: identity.MCPGrantPage{Items: []identity.MCPGrantItem{{
		ID: testGrantID, ClientName: "Notes app", Scopes: []string{"tendo:read"},
		CreatedAt: expires.Add(-time.Hour), ExpiresAt: expires,
	}}, NextCursor: "g1NEXT"}}
	h := policyRouter(svc, signedIn())
	w := policyServe(h, policyCall{method: http.MethodGet, target: adminGrantsURL + "?limit=5&cursor=g1PREV", cookie: sessionCookieHeader})
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d cache=%q body=%s", w.Code, w.Header().Get("Cache-Control"), w.Body)
	}
	if svc.grantUser != policyTarget || svc.grantLimit != 5 || svc.grantCursor != "g1PREV" || svc.grantActor == "" {
		t.Fatalf("service got actor=%q user=%q limit=%d cursor=%q", svc.grantActor, svc.grantUser, svc.grantLimit, svc.grantCursor)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	item := body["items"].([]any)[0].(map[string]any)
	if item["id"] != testGrantID || item["clientName"] != "Notes app" || item["clientNameVerified"] != false || body["nextCursor"] != "g1NEXT" {
		t.Fatalf("body=%v", body)
	}
}

func TestMCPAdminGrantListEmptyAndQueryErrors(t *testing.T) {
	h := policyRouter(&fakeMCPPolicyService{}, signedIn())
	w := policyServe(h, policyCall{method: http.MethodGet, target: adminGrantsURL, cookie: sessionCookieHeader})
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"items":[],"nextCursor":null}` {
		t.Fatalf("status=%d body=%s; want the empty page", w.Code, w.Body)
	}
	for _, q := range []string{"?limit=0", "?limit=101", "?cursor="} {
		svc := &fakeMCPPolicyService{}
		w := policyServe(policyRouter(svc, signedIn()), policyCall{method: http.MethodGet, target: adminGrantsURL + q, cookie: sessionCookieHeader})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"invalid_query"`) || svc.grantCalls != 0 {
			t.Errorf("%s: status=%d body=%s calls=%d", q, w.Code, w.Body, svc.grantCalls)
		}
	}
	svc := &fakeMCPPolicyService{grantErr: identity.ErrMCPInvalidCursor}
	w = policyServe(policyRouter(svc, signedIn()), policyCall{method: http.MethodGet, target: adminGrantsURL + "?cursor=forged", cookie: sessionCookieHeader})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"parameter":"cursor"`) {
		t.Fatalf("status=%d body=%s; want invalid_query naming cursor", w.Code, w.Body)
	}
}

func TestMCPAdminGrantRevokesAre204NoStore(t *testing.T) {
	for _, c := range adminGrantCalls()[1:] {
		svc := &fakeMCPPolicyService{}
		c.cookie = sessionCookieHeader
		w := policyServe(policyRouter(svc, signedIn()), c)
		if w.Code != http.StatusNoContent || w.Header().Get("Cache-Control") != "no-store" || w.Body.Len() != 0 {
			t.Fatalf("%s %s: status=%d cache=%q body=%s", c.method, c.target, w.Code, w.Header().Get("Cache-Control"), w.Body)
		}
		if svc.grantUser != policyTarget || svc.grantCalls != 1 {
			t.Fatalf("%s %s: user=%q calls=%d", c.method, c.target, svc.grantUser, svc.grantCalls)
		}
		if c.target == adminGrantURL && svc.grantID != testGrantID {
			t.Fatalf("grant id=%q", svc.grantID)
		}
	}
}

func TestMCPAdminGrantErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{identity.ErrNotFound, http.StatusNotFound, "not_found"},
		{errors.New("pq: connection refused"), http.StatusServiceUnavailable, "unavailable"},
	} {
		for _, c := range adminGrantCalls() {
			c.cookie = sessionCookieHeader
			w := policyServe(policyRouter(&fakeMCPPolicyService{grantErr: tc.err}, signedIn()), c)
			assertProblem(t, w, tc.status, tc.code)
			if strings.Contains(w.Body.String(), "pq:") {
				t.Fatalf("driver text leaked: %s", w.Body)
			}
		}
	}
}

// TestMCPAdminGrantRevokeRateLimitSharedAndPerActor: both DELETEs draw on one
// per-administrator bucket, charged before the service.
func TestMCPAdminGrantRevokeRateLimitSharedAndPerActor(t *testing.T) {
	svc := &fakeMCPPolicyService{}
	h := policyRouter(svc, signedIn())
	calls := adminGrantCalls()[1:]
	for i := 0; i < mcpAdminRevokePerActor; i++ {
		c := calls[i%2]
		c.cookie = sessionCookieHeader
		if w := policyServe(h, c); w.Code != http.StatusNoContent {
			t.Fatalf("call %d status=%d", i+1, w.Code)
		}
	}
	for _, c := range calls {
		c.cookie = sessionCookieHeader
		w := policyServe(h, c)
		assertProblem(t, w, http.StatusTooManyRequests, "rate_limited")
		if w.Header().Get("Retry-After") != "60" {
			t.Fatalf("Retry-After=%q", w.Header().Get("Retry-After"))
		}
	}
	if svc.grantCalls != mcpAdminRevokePerActor {
		t.Fatalf("service calls=%d want %d", svc.grantCalls, mcpAdminRevokePerActor)
	}
	w := policyServe(h, policyCall{method: http.MethodGet, target: adminGrantsURL, cookie: sessionCookieHeader})
	if w.Code != http.StatusOK {
		t.Fatalf("the list shares the revoke bucket: status=%d", w.Code)
	}
}
