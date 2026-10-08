package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	householdapp "github.com/bigtcze/tendo/backend/internal/household"
	householdhttp "github.com/bigtcze/tendo/backend/internal/household/httpapi"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

type membersStub struct{}

func (*membersStub) ListMembers(context.Context, string, string, string, int) ([]householdapp.Member, string, error) {
	return nil, "", nil
}

type invitationFakeService struct {
	create identity.InvitationResult
	items  []identity.Invitation
	next   string
	user   identity.InvitationUser
	err    error
	calls  int
}

func (f *invitationFakeService) CreateInvitation(context.Context, string, string, string) (identity.InvitationResult, error) {
	f.calls++
	r := f.create
	if !r.Created {
		r.Token = ""
	}
	return r, f.err
}
func (f *invitationFakeService) ListInvitations(context.Context, string, string, int, string) ([]identity.Invitation, string, error) {
	f.calls++
	return f.items, f.next, f.err
}
func (f *invitationFakeService) RevokeInvitation(context.Context, string, string, string) error {
	f.calls++
	return f.err
}
func (f *invitationFakeService) AcceptInvitationNewAccount(context.Context, string, string, string) (identity.InvitationUser, error) {
	f.calls++
	return f.user, f.err
}
func (f *invitationFakeService) AcceptInvitationExistingAccount(context.Context, string, string) (identity.InvitationUser, error) {
	f.calls++
	return f.user, f.err
}
func bodyMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &m); e != nil {
		t.Fatalf("json decode: %v body=%s", e, w.Body.String())
	}
	return m
}
func assertInvitationProblem(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	m := bodyMap(t, w)
	if w.Code != status || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" || m["status"] != float64(status) || m["code"] != code {
		t.Fatalf("status=%d headers=%v body=%v", w.Code, w.Header(), m)
	}
}
func headerValue(h map[string][]string, key string) string {
	if len(h[key]) == 0 {
		return ""
	}
	return h[key][0]
}
func makeRouter(s invitationService, auth func(http.Handler) http.Handler, valid bool) *chi.Mux {
	h := NewInvitations(s, auth, func(context.Context) (string, bool) { return "actor", valid })
	r := chi.NewRouter()
	h.Register(r)
	return r
}
func req(r http.Handler, method, path, body, media string, headers map[string][]string) *httptest.ResponseRecorder {
	q := httptest.NewRequest(method, path, strings.NewReader(body))
	if media != "" {
		q.Header.Set("Content-Type", media)
	}
	for k, v := range headers {
		q.Header[k] = v
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, q)
	return w
}
func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func TestInvitationHTTPCreateRetryListAndRevokeExactResponses(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	hid := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	iid := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b90"
	inv := identity.Invitation{ID: iid, HouseholdID: hid, CreatedAt: now, ExpiresAt: now.Add(7 * 24 * time.Hour), Status: "pending"}
	f := &invitationFakeService{create: identity.InvitationResult{Invitation: inv, Token: "only-once-secret", Created: true}, items: []identity.Invitation{inv}, next: "n1Y3Vyc29y"}
	r := makeRouter(f, nilAuth, true)
	headers := map[string][]string{"Idempotency-Key": {"key"}}
	w := req(r, "POST", "/api/v1/households/"+strings.ToUpper(hid)+"/invitations", "{}", "application/json", headers)
	want := map[string]any{"id": iid, "role": "member", "status": "pending", "createdAt": now.Format(time.RFC3339), "expiresAt": now.Add(7 * 24 * time.Hour).Format(time.RFC3339), "acceptedAt": nil, "revokedAt": nil, "token": "only-once-secret"}
	if w.Code != 201 || w.Header().Get("Location") != "/api/v1/households/"+hid+"/invitations/"+iid || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" || !sameJSON(bodyMap(t, w), want) {
		t.Fatalf("create=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	f.create.Created = false
	w = req(r, "POST", "/api/v1/households/"+hid+"/invitations", "{}", "application/json", headers)
	delete(want, "token")
	if w.Code != 200 || !sameJSON(bodyMap(t, w), want) {
		t.Fatalf("retry=%d %s", w.Code, w.Body.String())
	}
	w = req(r, "GET", "/api/v1/households/"+hid+"/invitations?limit=1", "", "", nil)
	listWant := map[string]any{"items": []any{want}, "nextCursor": "n1Y3Vyc29y"}
	if w.Code != 200 || !sameJSON(bodyMap(t, w), listWant) || strings.Contains(w.Body.String(), "token") {
		t.Fatalf("list=%d %s", w.Code, w.Body.String())
	}
	w = req(r, "DELETE", "/api/v1/households/"+hid+"/invitations/"+iid, "", "", nil)
	if w.Code != 204 || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("revoke=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
}
func nilAuth(next http.Handler) http.Handler { return next }
func TestProtectedInvitationRoutesReturn401BeforeService(t *testing.T) {
	f := &invitationFakeService{}
	r := makeRouter(f, nilAuth, false)
	members := householdhttp.NewMembers(&membersStub{}, nilAuth, func(context.Context) (string, bool) { return "actor", false })
	members.Register(r)
	routes := []struct {
		method, path, body string
		headers            map[string][]string
	}{{"POST", "/api/v1/households/h/invitations", "{}", map[string][]string{"Idempotency-Key": {"key"}, "Content-Type": {"application/json"}}}, {"GET", "/api/v1/households/h/invitations", "", nil}, {"DELETE", "/api/v1/households/h/invitations/i", "", nil}, {"POST", "/api/v1/invitations/accept", `{"token":"x"}`, map[string][]string{"Content-Type": {"application/json"}}}, {"GET", "/api/v1/households/h/members", "", nil}}
	for _, tc := range routes {
		w := req(r, tc.method, tc.path, tc.body, headerValue(tc.headers, "Content-Type"), tc.headers)
		assertInvitationProblem(t, w, 401, "unauthenticated")
	}
	if f.calls != 0 {
		t.Fatalf("service calls=%d", f.calls)
	}
}
func TestCreateIdempotencyHeaderAndStrictBodyFailures(t *testing.T) {
	f := &invitationFakeService{}
	r := makeRouter(f, nilAuth, true)
	for _, tc := range []struct {
		headers    map[string][]string
		body, code string
	}{{nil, "{}", "idempotency_key_required"}, {map[string][]string{"Idempotency-Key": {""}}, "{}", "idempotency_key_required"}, {map[string][]string{"Idempotency-Key": {"bad key"}}, "{}", "invalid_idempotency_key"}, {map[string][]string{"Idempotency-Key": {"a", "b"}}, "{}", "invalid_idempotency_key"}, {map[string][]string{"Idempotency-Key": {strings.Repeat("x", 129)}}, "{}", "invalid_idempotency_key"}, {map[string][]string{"Idempotency-Key": {"x"}}, `{"a":1}`, "invalid_request"}, {map[string][]string{"Idempotency-Key": {"x"}}, `[]`, "invalid_request"}, {map[string][]string{"Idempotency-Key": {"x"}}, `null`, "invalid_request"}} {
		h := map[string][]string{"Content-Type": {"application/json"}}
		for k, v := range tc.headers {
			h[k] = v
		}
		before := f.calls
		w := req(r, "POST", "/api/v1/households/h/invitations", tc.body, "application/json", h)
		assertInvitationProblem(t, w, 400, tc.code)
		if f.calls != before {
			t.Fatal("invalid request reached service")
		}
	}
}
func TestInvitationAcceptValidationBodyAndMediaErrors(t *testing.T) {
	f := &invitationFakeService{}
	r := makeRouter(f, nilAuth, true)
	invalidBodies := []struct{ path, body string }{{"/api/v1/auth/invitations/accept", `{"token":null,"login":"valid_login","password":"a long enough password"}`}, {"/api/v1/auth/invitations/accept", `{"token":"x","login":"valid_login","password":null}`}, {"/api/v1/auth/invitations/accept", `{"token":"x","login":"valid_login","password":4}`}, {"/api/v1/auth/invitations/accept", `{"token":"x","login":"valid_login","password":"a long enough password","extra":true}`}, {"/api/v1/auth/invitations/accept", `{"token":"x","login":"valid_login"}`}, {"/api/v1/auth/invitations/accept", `{"token":"x","login":4,"password":"a long enough password"}`}, {"/api/v1/invitations/accept", `{"token":null}`}, {"/api/v1/invitations/accept", `{"token":"x","extra":1}`}}
	for _, tc := range invalidBodies {
		f = &invitationFakeService{}
		r = makeRouter(f, nilAuth, true)
		before := f.calls
		w := req(r, "POST", tc.path, tc.body, "application/json", nil)
		assertInvitationProblem(t, w, 400, "invalid_request")
		if f.calls != before {
			t.Fatal("bad body reached service")
		}
	}
	for _, p := range []string{"/api/v1/auth/invitations/accept", "/api/v1/invitations/accept", "/api/v1/households/h/invitations"} {
		f = &invitationFakeService{}
		r = makeRouter(f, nilAuth, true)
		body := "{}"
		headers := map[string][]string{}
		if strings.Contains(p, "/households/") {
			headers["Idempotency-Key"] = []string{"key"}
		}
		w := req(r, "POST", p, body, "text/plain", headers)
		assertInvitationProblem(t, w, 415, "unsupported_media_type")
		w = req(r, "POST", p, strings.Repeat("x", 4097), "application/json", headers)
		assertInvitationProblem(t, w, 413, "content_too_large")
	}
}
func TestInvitationAcceptSuccessAndErrorMatrix(t *testing.T) {
	for _, tc := range []struct {
		err         error
		status      int
		code, field string
		newAccount  bool
	}{{&identity.ValidationError{Field: "login", Code: "invalid_format"}, 422, "invalid_format", "login", true}, {&identity.ValidationError{Field: "password", Code: "invalid_length"}, 422, "invalid_length", "password", true}, {identity.ErrInvalidInvitation, 404, "invalid_invitation", "", true}, {identity.ErrLoginUnavailable, 409, "login_unavailable", "", true}, {identity.ErrHouseholdConflict, 409, "household_conflict", "", false}, {identity.ErrAlreadyMember, 409, "already_member", "", false}, {identity.ErrNotFound, 401, "unauthenticated", "", false}, {security.ErrPasswordWorkLimit, 429, "rate_limited", "", true}, {errors.New("secret persistence"), 503, "unavailable", "", true}} {
		f := &invitationFakeService{err: tc.err, user: identity.InvitationUser{UserID: "u", Login: "login", HouseholdID: "h", Role: "member"}}
		r := makeRouter(f, nilAuth, true)
		path, body := "/api/v1/invitations/accept", `{"token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`
		if tc.newAccount {
			path = "/api/v1/auth/invitations/accept"
			body = `{"token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","login":"valid_login","password":"a sufficiently long passphrase"}`
		}
		w := req(r, "POST", path, body, "application/json", nil)
		assertInvitationProblem(t, w, tc.status, tc.code)
		if tc.field != "" {
			m := bodyMap(t, w)
			if m["field"] != tc.field || m["code"] != tc.code {
				t.Fatalf("field error=%v", m)
			}
		}
		if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "AAAAAAAA") {
			t.Fatalf("sensitive data leaked %s", w.Body.String())
		}
		if tc.err == identity.ErrNotFound {
			continue
		}
	}
	f := &invitationFakeService{user: identity.InvitationUser{UserID: "u", Login: "login", HouseholdID: "h", Role: "member"}}
	r := makeRouter(f, nilAuth, true)
	w := req(r, "POST", "/api/v1/auth/invitations/accept", `{"token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","login":"valid_login","password":"a sufficiently long passphrase"}`, "application/json", nil)
	if w.Code != 201 || w.Header().Get("Location") != "/api/v1/households/h" || w.Header().Get("Set-Cookie") != "" || !sameJSON(bodyMap(t, w), map[string]any{"userId": "u", "login": "login", "householdId": "h", "role": "member"}) {
		t.Fatalf("new accept=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	existing := &invitationFakeService{user: identity.InvitationUser{UserID: "u", HouseholdID: "h", Role: "member"}}
	r = makeRouter(existing, nilAuth, true)
	w = req(r, "POST", "/api/v1/invitations/accept", `{"token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`, "application/json", nil)
	if w.Code != 201 || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "/api/v1/households/h" || !sameJSON(bodyMap(t, w), map[string]any{"userId": "u", "householdId": "h", "role": "member"}) {
		t.Fatalf("existing accept=%d %s", w.Code, w.Body.String())
	}
}
func TestPerClientAcceptanceRateLimits(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		max        int
	}{{"/api/v1/auth/invitations/accept", `{"token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","login":"valid_login","password":"a sufficiently long passphrase"}`, 5}, {"/api/v1/invitations/accept", `{"token":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`, 10}} {
		f := &invitationFakeService{err: identity.ErrInvalidInvitation}
		r := makeRouter(f, nilAuth, true)
		for i := 0; i < tc.max+1; i++ {
			q := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			q.Header.Set("Content-Type", "application/json")
			q.RemoteAddr = "192.0.2.1:1"
			if !strings.Contains(tc.path, "/households/") {
				q = httpx.WithRequestMetadata(q, httpx.RequestMetadata{ClientIP: "192.0.2.1"})
			}
			w := httptest.NewRecorder()
			beforeCalls := f.calls
			r.ServeHTTP(w, q)
			if i < tc.max && (w.Code != 404 || f.calls != beforeCalls+1) {
				t.Fatalf("attempt %d status=%d", i, w.Code)
			}
			if i == tc.max {
				assertInvitationProblem(t, w, 429, "rate_limited")
				if f.calls != beforeCalls {
					t.Fatalf("over-limit request reached service: calls %d -> %d", beforeCalls, f.calls)
				}
				if w.Header().Get("Retry-After") != "60" {
					t.Fatalf("retry-after=%q", w.Header().Get("Retry-After"))
				}
			}
		}
		other := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		other.Header.Set("Content-Type", "application/json")
		other.RemoteAddr = "192.0.2.2:1"
		if !strings.Contains(tc.path, "/households/") {
			other = httpx.WithRequestMetadata(other, httpx.RequestMetadata{ClientIP: "192.0.2.2"})
		}
		w := httptest.NewRecorder()
		before := f.calls
		r.ServeHTTP(w, other)
		if w.Code != 404 || f.calls != before+1 {
			t.Fatalf("different client not admitted: status=%d calls=%d before=%d", w.Code, f.calls, before)
		}
	}
}
