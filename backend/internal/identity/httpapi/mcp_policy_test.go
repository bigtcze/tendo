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

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

const (
	policyTarget      = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81"
	policyAdminURL    = "/api/v1/admin/accounts/" + policyTarget + "/mcp-policy"
	policySettingsURL = "/api/v1/mcp/settings"
	policyJSON        = "application/json"
	policyValidBody   = `{"enabled":false,"maxScopes":["tendo:read"]}`
	policySettingsOK  = `{"enabled":true,"scopes":["tendo:read"]}`
)

// fakeMCPPolicyService records every call and answers the configured outcome.
type fakeMCPPolicyService struct {
	adminErr      error
	adminCalls    int
	readCalls     int
	readActor     string
	readUser      string
	readOut       identity.MCPAccountPolicy
	readErr       error
	updCalls      int
	updUser       string
	updIn         identity.MCPAccountPolicyChange
	updExpected   int64
	updOut        identity.MCPAccountPolicy
	updErr        error
	settingsCalls int
	settingsOut   identity.MCPSettings
	settingsErr   error
	setUpdCalls   int
	setUpdIn      identity.MCPSettingsChange
	setUpdExpect  int64
	setUpdOut     identity.MCPSettings
	setUpdErr     error
	// The administrator grant routes (B1b3).
	grantCalls  int
	grantActor  string
	grantUser   string
	grantID     string
	grantLimit  int
	grantCursor string
	grantPage   identity.MCPGrantPage
	grantErr    error
}

// total counts every service call, admin pre-check included.
func (f *fakeMCPPolicyService) total() int {
	return f.adminCalls + f.readCalls + f.updCalls + f.settingsCalls + f.setUpdCalls + f.grantCalls
}

func (f *fakeMCPPolicyService) RequireInstanceAdmin(_ context.Context, _ string) error {
	f.adminCalls++
	return f.adminErr
}

func (f *fakeMCPPolicyService) AccountPolicy(_ context.Context, actor, user string) (identity.MCPAccountPolicy, error) {
	f.readCalls++
	f.readActor, f.readUser = actor, user
	return f.readOut, f.readErr
}

func (f *fakeMCPPolicyService) UpdateAccountPolicy(_ context.Context, _, user string, change identity.MCPAccountPolicyChange, expected int64) (identity.MCPAccountPolicy, error) {
	f.updCalls++
	f.updUser, f.updIn, f.updExpected = user, change, expected
	return f.updOut, f.updErr
}

func (f *fakeMCPPolicyService) Settings(_ context.Context, _ string) (identity.MCPSettings, error) {
	f.settingsCalls++
	return f.settingsOut, f.settingsErr
}

func (f *fakeMCPPolicyService) UpdateSettings(_ context.Context, _ string, change identity.MCPSettingsChange, expected int64) (identity.MCPSettings, error) {
	f.setUpdCalls++
	f.setUpdIn, f.setUpdExpect = change, expected
	return f.setUpdOut, f.setUpdErr
}

// InstallationPolicy, UpdateInstallationPolicy and PolicyEvents (B1b2) count
// into the same total, so a refused caller is shown to reach no command.
func (f *fakeMCPPolicyService) InstallationPolicy(context.Context, string) (identity.MCPInstallationView, error) {
	f.readCalls++
	return identity.MCPInstallationView{}, nil
}

func (f *fakeMCPPolicyService) UpdateInstallationPolicy(context.Context, string, identity.MCPInstallationChange, int64) (identity.MCPInstallationView, error) {
	f.updCalls++
	return identity.MCPInstallationView{}, nil
}

func (f *fakeMCPPolicyService) PolicyEvents(context.Context, string, int, string) (identity.MCPPolicyEventList, error) {
	f.readCalls++
	return identity.MCPPolicyEventList{}, nil
}

// policySessions authenticates each token to its own principal, so one test can
// sign in two accounts at once.
type policySessions struct {
	principals map[string]identity.Principal
}

func (f *policySessions) Login(context.Context, string, string) (identity.Session, error) {
	return identity.Session{}, errors.New("not used by policy tests")
}

func (f *policySessions) Lookup(_ context.Context, token string) (identity.SessionInfo, error) {
	p, ok := f.principals[token]
	if !ok {
		return identity.SessionInfo{}, identity.ErrUnauthenticated
	}
	return identity.SessionInfo{ID: "sid-" + token, Principal: p, ExpiresAt: time.Now().Add(time.Hour), SignInMethod: identity.SignInPassword}, nil
}

func (f *policySessions) Logout(context.Context, string) error { return nil }

func (f *policySessions) SetDefaultHousehold(context.Context, string, string) error { return nil }

// policyRouter mounts the policy adapter exactly as composition does.
func policyRouter(service mcpPolicyService, sessions sessionService) http.Handler {
	routes := chi.NewRouter()
	NewMCPPolicy(service, NewSession(sessions, mcpPublicURL)).Register(routes)
	return httpx.NewAppWithRoutes(nil, time.Second, make(chan struct{}), httpx.OriginPolicy{PublicURL: mcpPublicURL}, func(r chi.Router) { r.Mount("/", routes) })
}

// policyCall is one request to a policy route. Every request carries the
// canonical Origin; empty fields are left unset.
type policyCall struct {
	method, target, body  string
	ifMatch, contentType  string
	cookie, authorization string
}

func (c policyCall) request() *http.Request {
	r := httptest.NewRequest(c.method, mcpPublicURL+c.target, strings.NewReader(c.body))
	r.Header.Set("Origin", mcpPublicURL)
	if c.cookie != "" {
		r.Header.Set("Cookie", c.cookie)
	}
	if c.authorization != "" {
		r.Header.Set("Authorization", c.authorization)
	}
	if c.ifMatch != "" {
		r.Header.Set("If-Match", c.ifMatch)
	}
	if c.contentType != "" {
		r.Header.Set("Content-Type", c.contentType)
	}
	return r
}

func policyServe(h http.Handler, c policyCall) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, c.request())
	return w
}

// policyPut is a valid administrator PUT, signed in and versioned at 7.
func policyPut(body string) policyCall {
	return policyCall{method: http.MethodPut, target: policyAdminURL, body: body, ifMatch: `"7"`, contentType: policyJSON, cookie: sessionCookieHeader}
}

func TestMCPPolicyRoutesRequireSessionCookie(t *testing.T) {
	svc := &fakeMCPPolicyService{}
	h := policyRouter(svc, signedIn())
	bearer := "Bearer " + strings.Repeat("A", 43)
	for _, c := range []policyCall{
		{method: http.MethodGet, target: policyAdminURL},
		{method: http.MethodPut, target: policyAdminURL, body: policyValidBody, ifMatch: `"7"`, contentType: policyJSON},
		{method: http.MethodGet, target: policySettingsURL},
		{method: http.MethodPut, target: policySettingsURL, body: policySettingsOK, ifMatch: `"1"`, contentType: policyJSON},
	} {
		c.authorization = bearer
		w := policyServe(h, c)
		if w.Code != http.StatusUnauthorized || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s bearer-only: status=%d cache=%q", c.method, c.target, w.Code, w.Header().Get("Cache-Control"))
		}
	}
	if svc.total() != 0 {
		t.Fatalf("service reached without a session cookie: %d calls", svc.total())
	}
}

// TestMCPPolicyAdminCheckRunsFirst: a non-administrator is refused with 403
// before If-Match, body validation or the target lookup.
func TestMCPPolicyAdminCheckRunsFirst(t *testing.T) {
	svc := &fakeMCPPolicyService{adminErr: identity.ErrInstanceAdminRequired}
	h := policyRouter(svc, signedIn())
	for _, c := range []policyCall{
		{method: http.MethodGet, target: policyAdminURL, cookie: sessionCookieHeader},
		{method: http.MethodPut, target: policyAdminURL, body: `{"bogus"`, contentType: "text/plain", cookie: sessionCookieHeader},
	} {
		assertProblem(t, policyServe(h, c), http.StatusForbidden, "instance_admin_required")
	}
	if svc.readCalls != 0 || svc.updCalls != 0 || svc.adminCalls != 2 {
		t.Fatalf("read=%d update=%d admin=%d; the command must not run", svc.readCalls, svc.updCalls, svc.adminCalls)
	}
}

func TestMCPPolicyAdminPutPreconditionHeaders(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ifMatch  string
		setMatch bool
		status   int
		code     string
	}{
		{"missing If-Match", "", false, http.StatusPreconditionRequired, "precondition_required"},
		{"weak If-Match", `W/"1"`, true, http.StatusPreconditionFailed, "precondition_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeMCPPolicyService{}
			c := policyPut(policyValidBody)
			c.ifMatch = tc.ifMatch
			if !tc.setMatch {
				c.ifMatch = ""
			}
			assertProblem(t, policyServe(policyRouter(svc, signedIn()), c), tc.status, tc.code)
			if svc.updCalls != 0 {
				t.Fatal("the command ran without a strong If-Match")
			}
		})
	}
}

func TestMCPPolicyAdminPutRejectsWrongContentType(t *testing.T) {
	svc := &fakeMCPPolicyService{}
	c := policyPut(policyValidBody)
	c.contentType = "text/plain"
	assertProblem(t, policyServe(policyRouter(svc, signedIn()), c), http.StatusUnsupportedMediaType, "unsupported_media_type")
	if svc.updCalls != 0 {
		t.Fatal("the command ran with a wrong content type")
	}
}

// TestMCPPolicyAdminPutInvalidBodies: every body that is not exactly the
// declared object is a 400 before the command.
func TestMCPPolicyAdminPutInvalidBodies(t *testing.T) {
	for _, body := range []string{
		`{"enabled":false,"maxScopes":["tendo:read"],"extra":1}`,
		`{"enabled":false,"enabled":true,"maxScopes":["tendo:read"]}`,
		`{"enabled":"yes","maxScopes":["tendo:read"]}`,
		`{"enabled":false,"maxScopes":[1]}`,
		`{"enabled":false}`,
		`{"maxScopes":["tendo:read"]}`,
		`{}`,
	} {
		svc := &fakeMCPPolicyService{}
		assertProblem(t, policyServe(policyRouter(svc, signedIn()), policyPut(body)), http.StatusBadRequest, "invalid_request")
		if svc.updCalls != 0 {
			t.Fatalf("body %s reached the command", body)
		}
	}
}

// TestMCPPolicyAdminPutNullTermsPassNil: null terms reach the command as nil,
// the "follow the default" value, and not as an empty list.
func TestMCPPolicyAdminPutNullTermsPassNil(t *testing.T) {
	svc := &fakeMCPPolicyService{}
	policyServe(policyRouter(svc, signedIn()), policyPut(`{"enabled":null,"maxScopes":null}`))
	if svc.updCalls != 1 || svc.updIn.Enabled != nil || svc.updIn.MaxScopes != nil {
		t.Fatalf("change=%+v calls=%d", svc.updIn, svc.updCalls)
	}
}

func TestMCPPolicyAdminPutPassesValuesAndVersion(t *testing.T) {
	svc := &fakeMCPPolicyService{}
	policyServe(policyRouter(svc, signedIn()), policyPut(policyValidBody))
	if svc.updCalls != 1 || svc.updExpected != 7 || svc.updUser != policyTarget {
		t.Fatalf("user=%q expected=%d calls=%d", svc.updUser, svc.updExpected, svc.updCalls)
	}
	if svc.updIn.Enabled == nil || *svc.updIn.Enabled || len(svc.updIn.MaxScopes) != 1 || svc.updIn.MaxScopes[0] != "tendo:read" {
		t.Fatalf("change=%+v", svc.updIn)
	}
}

// TestMCPPolicyAdminPutResponseShape: the body has exactly the six keys, null
// for nil terms, [] for an empty effective set, and the version as the ETag.
func TestMCPPolicyAdminPutResponseShape(t *testing.T) {
	svc := &fakeMCPPolicyService{updOut: identity.MCPAccountPolicy{
		UserID: policyTarget, UserEnabled: true, UserScopes: []string{"tendo:read", "tendo:write"},
		Effective: []string{}, Version: 8,
	}}
	w := policyServe(policyRouter(svc, signedIn()), policyPut(`{"enabled":null,"maxScopes":null}`))
	if w.Code != http.StatusOK || w.Header().Get("ETag") != `"8"` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d etag=%q cache=%q body=%s", w.Code, w.Header().Get("ETag"), w.Header().Get("Cache-Control"), w.Body)
	}
	assertPolicyShape(t, w.Body.String(), map[string]string{
		"enabled": "null", "maxScopes": "null", "userId": `"` + policyTarget + `"`,
		"userEnabled": "true", "userScopes": `["tendo:read","tendo:write"]`, "effectiveScopes": "[]",
	})
}

// assertPolicyShape requires exactly the wanted keys, each with the wanted raw
// JSON value, so a missing key or a null for an empty list fails.
func assertPolicyShape(t *testing.T, body string, want map[string]string) {
	t.Helper()
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	if len(raw) != len(want) {
		t.Fatalf("keys=%d want %d: %s", len(raw), len(want), body)
	}
	for key, value := range want {
		got, ok := raw[key]
		if !ok || string(got) != value {
			t.Fatalf("%s=%s (present=%v) want %s: %s", key, got, ok, value, body)
		}
	}
}

// TestMCPPolicyAdminErrorMapping covers the admin write's failure answers.
func TestMCPPolicyAdminErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"not found", identity.ErrNotFound, http.StatusNotFound, "not_found"},
		{"precondition", identity.ErrMCPPolicyPreconditionFailed, http.StatusPreconditionFailed, "precondition_failed"},
		{"validation", &identity.ValidationError{Field: "maxScopes", Code: "invalid_value"}, http.StatusUnprocessableEntity, "invalid_value"},
		{"fault", errors.New("pq: connection refused at 10.0.0.9"), http.StatusServiceUnavailable, "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &fakeMCPPolicyService{updErr: tc.err}
			w := policyServe(policyRouter(svc, signedIn()), policyPut(policyValidBody))
			assertProblem(t, w, tc.status, tc.code)
			if strings.Contains(w.Body.String(), "10.0.0.9") {
				t.Fatalf("driver text leaked: %s", w.Body)
			}
		})
	}
}

func TestMCPPolicyValidationProblemNamesFieldAndCode(t *testing.T) {
	svc := &fakeMCPPolicyService{updErr: &identity.ValidationError{Field: "maxScopes", Code: "invalid_value"}}
	w := policyServe(policyRouter(svc, signedIn()), policyPut(policyValidBody))
	if !strings.Contains(w.Body.String(), `"field":"maxScopes"`) || !strings.Contains(w.Body.String(), `"code":"invalid_value"`) {
		t.Fatalf("body=%s", w.Body)
	}
}

func TestMCPPolicyUnauthenticatedClearsCookie(t *testing.T) {
	svc := &fakeMCPPolicyService{updErr: identity.ErrUnauthenticated}
	w := policyServe(policyRouter(svc, signedIn()), policyPut(policyValidBody))
	assertProblem(t, w, http.StatusUnauthorized, "unauthenticated")
	if !strings.Contains(w.Header().Get("Set-Cookie"), "__Host-tendo_session=;") || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("session cookie not cleared: %q", w.Header().Get("Set-Cookie"))
	}
}

func TestMCPPolicyAdminGetShapeAndNotFound(t *testing.T) {
	svc := &fakeMCPPolicyService{readOut: identity.MCPAccountPolicy{
		UserID: policyTarget, UserEnabled: true, UserScopes: []string{"tendo:read"},
		Effective: []string{}, Version: 2,
	}}
	w := policyServe(policyRouter(svc, signedIn()), policyCall{method: http.MethodGet, target: policyAdminURL, cookie: sessionCookieHeader})
	if w.Code != http.StatusOK || w.Header().Get("ETag") != `"2"` || svc.readUser != policyTarget || svc.readActor != "user-1" {
		t.Fatalf("status=%d etag=%q actor=%q user=%q", w.Code, w.Header().Get("ETag"), svc.readActor, svc.readUser)
	}
	assertPolicyShape(t, w.Body.String(), map[string]string{
		"enabled": "null", "maxScopes": "null", "userId": `"` + policyTarget + `"`,
		"userEnabled": "true", "userScopes": `["tendo:read"]`, "effectiveScopes": "[]",
	})

	svc = &fakeMCPPolicyService{readErr: identity.ErrNotFound}
	assertProblem(t, policyServe(policyRouter(svc, signedIn()), policyCall{method: http.MethodGet, target: policyAdminURL, cookie: sessionCookieHeader}), http.StatusNotFound, "not_found")
}

func TestMCPPolicySettingsGetShape(t *testing.T) {
	svc := &fakeMCPPolicyService{settingsOut: identity.MCPSettings{
		Enabled: true, Scopes: []string{"tendo:read"}, Maximum: []string{"tendo:read", "tendo:write"},
		Effective: []string{"tendo:read"}, Version: 4,
	}}
	w := policyServe(policyRouter(svc, signedIn()), policyCall{method: http.MethodGet, target: policySettingsURL, cookie: sessionCookieHeader})
	if w.Code != http.StatusOK || w.Header().Get("ETag") != `"4"` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d etag=%q", w.Code, w.Header().Get("ETag"))
	}
	assertPolicyShape(t, w.Body.String(), map[string]string{
		"enabled": "true", "scopes": `["tendo:read"]`, "maximumScopes": `["tendo:read","tendo:write"]`,
		"effectiveScopes": `["tendo:read"]`,
	})
}

func TestMCPPolicySettingsGetDeletedIsUnauthenticated(t *testing.T) {
	svc := &fakeMCPPolicyService{settingsErr: identity.ErrUnauthenticated}
	w := policyServe(policyRouter(svc, signedIn()), policyCall{method: http.MethodGet, target: policySettingsURL, cookie: sessionCookieHeader})
	assertProblem(t, w, http.StatusUnauthorized, "unauthenticated")
}

// TestMCPPolicySettingsPutBodyValidation: null scopes and null enabled are
// refused, and an unknown key is refused.
func TestMCPPolicySettingsPutBodyValidation(t *testing.T) {
	for _, body := range []string{
		`{"enabled":true,"scopes":null}`,
		`{"enabled":null,"scopes":["tendo:read"]}`,
		`{"enabled":true,"scopes":["tendo:read"],"maxScopes":["tendo:read"]}`,
		`{"enabled":true}`,
	} {
		svc := &fakeMCPPolicyService{}
		c := policyCall{method: http.MethodPut, target: policySettingsURL, body: body, ifMatch: `"1"`, contentType: policyJSON, cookie: sessionCookieHeader}
		assertProblem(t, policyServe(policyRouter(svc, signedIn()), c), http.StatusBadRequest, "invalid_request")
		if svc.setUpdCalls != 0 {
			t.Fatalf("body %s reached the command", body)
		}
	}
}

func TestMCPPolicySettingsPutPassesValuesAndResponds(t *testing.T) {
	svc := &fakeMCPPolicyService{setUpdOut: identity.MCPSettings{
		Enabled: false, Scopes: []string{"tendo:read"}, Maximum: []string{"tendo:read"}, Effective: []string{}, Version: 3,
	}}
	c := policyCall{method: http.MethodPut, target: policySettingsURL, body: `{"enabled":false,"scopes":["tendo:read"]}`, ifMatch: `"2"`, contentType: policyJSON, cookie: sessionCookieHeader}
	w := policyServe(policyRouter(svc, signedIn()), c)
	if w.Code != http.StatusOK || w.Header().Get("ETag") != `"3"` || svc.setUpdExpect != 2 {
		t.Fatalf("status=%d etag=%q expected=%d", w.Code, w.Header().Get("ETag"), svc.setUpdExpect)
	}
	if svc.setUpdIn.Enabled || len(svc.setUpdIn.Scopes) != 1 || svc.setUpdIn.Scopes[0] != "tendo:read" {
		t.Fatalf("change=%+v", svc.setUpdIn)
	}
	assertPolicyShape(t, w.Body.String(), map[string]string{
		"enabled": "false", "scopes": `["tendo:read"]`, "maximumScopes": `["tendo:read"]`, "effectiveScopes": "[]",
	})
}

func TestMCPPolicySettingsPutPreconditionHeaders(t *testing.T) {
	svc := &fakeMCPPolicyService{}
	c := policyCall{method: http.MethodPut, target: policySettingsURL, body: policySettingsOK, ifMatch: `W/"1"`, contentType: policyJSON, cookie: sessionCookieHeader}
	assertProblem(t, policyServe(policyRouter(svc, signedIn()), c), http.StatusPreconditionFailed, "precondition_failed")
	if svc.setUpdCalls != 0 {
		t.Fatal("the command ran with a weak If-Match")
	}
}

// TestMCPPolicySettingsRateLimitBeforeIfMatch: the 31st write by one user is
// refused with 429 and Retry-After 60, and a write without If-Match counts.
func TestMCPPolicySettingsRateLimitBeforeIfMatch(t *testing.T) {
	svc := &fakeMCPPolicyService{}
	h := policyRouter(svc, signedIn())
	for i := 0; i < mcpSettingsWritePerUser-1; i++ {
		c := policyCall{method: http.MethodPut, target: policySettingsURL, body: policySettingsOK, ifMatch: `"1"`, contentType: policyJSON, cookie: sessionCookieHeader}
		policyServe(h, c)
	}
	// Without If-Match this is a 428, and it still spends one attempt.
	policyServe(h, policyCall{method: http.MethodPut, target: policySettingsURL, body: policySettingsOK, contentType: policyJSON, cookie: sessionCookieHeader})
	if svc.setUpdCalls != mcpSettingsWritePerUser-1 {
		t.Fatalf("service calls=%d want %d", svc.setUpdCalls, mcpSettingsWritePerUser-1)
	}
	w := policyServe(h, policyCall{method: http.MethodPut, target: policySettingsURL, body: policySettingsOK, ifMatch: `"1"`, contentType: policyJSON, cookie: sessionCookieHeader})
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("31st status=%d retry=%q", w.Code, w.Header().Get("Retry-After"))
	}
	assertProblem(t, w, http.StatusTooManyRequests, "rate_limited")
	if svc.setUpdCalls != mcpSettingsWritePerUser-1 {
		t.Fatal("the limited write reached the service")
	}
}

// TestMCPPolicySettingsRateLimitIsPerUser: another signed-in account keeps its
// own bucket after the first account is limited.
func TestMCPPolicySettingsRateLimitIsPerUser(t *testing.T) {
	const otherToken = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
	sessions := &policySessions{principals: map[string]identity.Principal{
		fakeToken:  {UserID: "user-1", Login: "owner"},
		otherToken: {UserID: "user-2", Login: "other"},
	}}
	svc := &fakeMCPPolicyService{}
	h := policyRouter(svc, sessions)
	for i := 0; i < mcpSettingsWritePerUser+1; i++ {
		policyServe(h, policyCall{method: http.MethodPut, target: policySettingsURL, body: policySettingsOK, ifMatch: `"1"`, contentType: policyJSON, cookie: sessionCookieHeader})
	}
	w := policyServe(h, policyCall{method: http.MethodPut, target: policySettingsURL, body: policySettingsOK, ifMatch: `"1"`, contentType: policyJSON, cookie: "__Host-tendo_session=" + otherToken})
	if w.Code != http.StatusOK {
		t.Fatalf("second user status=%d body=%s", w.Code, w.Body)
	}
}
