package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

const fakeToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type fakeSessionService struct {
	loginCalls  int
	lookupCalls int
	logoutToken string
	logouts     int
	loginErr    error
	lookupErr   error
	logoutErr   error
	expires     time.Time
	principal   identity.Principal
}

func (f *fakeSessionService) Login(_ context.Context, login, password string) (identity.Session, error) {
	f.loginCalls++
	if f.loginErr != nil {
		return identity.Session{}, f.loginErr
	}
	return identity.Session{Token: fakeToken, ID: "sid-1", Principal: f.principal, ExpiresAt: f.expires}, nil
}
func (f *fakeSessionService) Lookup(_ context.Context, token string) (identity.SessionInfo, error) {
	f.lookupCalls++
	if f.lookupErr != nil {
		return identity.SessionInfo{}, f.lookupErr
	}
	if token != fakeToken {
		return identity.SessionInfo{}, identity.ErrUnauthenticated
	}
	return identity.SessionInfo{ID: "sid-1", Principal: f.principal, ExpiresAt: f.expires}, nil
}
func (f *fakeSessionService) Logout(_ context.Context, token string) error {
	f.logouts++
	f.logoutToken = token
	return f.logoutErr
}

type loginServiceFake struct {
	calls     int
	verify    func(string, string) (bool, error)
	validHash string
	err       error
}

func (f *loginServiceFake) Login(_ context.Context, _ string, password string) (identity.Session, error) {
	f.calls++
	_, err := f.verify(f.validHash, password)
	if err != nil {
		return identity.Session{}, err
	}
	if f.err != nil {
		return identity.Session{}, f.err
	}
	return identity.Session{Token: fakeToken, ID: "sid", Principal: identity.Principal{UserID: "u", Login: "owner_1"}, ExpiresAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)}, nil
}
func (*loginServiceFake) Lookup(context.Context, string) (identity.SessionInfo, error) {
	return identity.SessionInfo{}, nil
}
func (*loginServiceFake) Logout(context.Context, string) error { return nil }

func sessionFixture(publicURL string) (*fakeSessionService, *SessionHandler, http.Handler) {
	svc := &fakeSessionService{expires: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), principal: identity.Principal{UserID: "u-1", Login: "owner_1", DefaultHouseholdID: "h-1"}}
	h := NewSession(svc, publicURL)
	r := chi.NewRouter()
	h.Register(r)
	return svc, h, r
}

func loginRequest(body string, mutate ...func(*http.Request)) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/session", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = httpx.WithRequestMetadata(r, httpx.RequestMetadata{ClientIP: "192.0.2.1"})
	for _, m := range mutate {
		m(r)
	}
	return r
}

const validLogin = `{"login":"owner_1","password":"correct horse battery"}`

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func assertProblem(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
	var body struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Status != status || body.Code != code {
		t.Fatalf("problem body=%s err=%v", w.Body, err)
	}
}

func TestLoginCookiePolicyFollowsPublicURL(t *testing.T) {
	for _, tc := range []struct {
		name, url, cookie string
		secure            bool
	}{
		{"https", "https://tendo.example", "__Host-tendo_session", true},
		{"http", "http://localhost:8080", "tendo_session", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, mux := sessionFixture(tc.url)
			w := serve(mux, loginRequest(validLogin))
			if w.Code != 201 || w.Header().Get("Location") != "/api/v1/session" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("status=%d headers=%v", w.Code, w.Header())
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies=%v", cookies)
			}
			c := cookies[0]
			if c.Name != tc.cookie || c.Value != fakeToken || !c.HttpOnly || c.Secure != tc.secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.MaxAge != 30*24*3600 {
				t.Fatalf("cookie=%+v raw=%q", c, w.Header().Get("Set-Cookie"))
			}
			if tc.secure && !strings.Contains(w.Header().Get("Set-Cookie"), "; Secure") {
				t.Fatal("missing Secure attribute")
			}
			if strings.Contains(w.Body.String(), fakeToken) {
				t.Fatal("token leaked in response body")
			}
			var body map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"userId": "u-1", "login": "owner_1", "defaultHouseholdId": "h-1", "expiresAt": "2026-02-01T00:00:00Z"}
			if len(body) != len(want) {
				t.Fatalf("body=%v", body)
			}
			for k, v := range want {
				if body[k] != v {
					t.Fatalf("body[%s]=%v", k, body[k])
				}
			}
			if svc.loginCalls != 1 {
				t.Fatalf("login calls=%d", svc.loginCalls)
			}
		})
	}
}

func TestSessionBodyOmitsEmptyDefaultHousehold(t *testing.T) {
	svc, _, mux := sessionFixture("http://localhost")
	svc.principal.DefaultHouseholdID = ""
	w := serve(mux, loginRequest(validLogin))
	if w.Code != 201 || strings.Contains(w.Body.String(), "defaultHouseholdId") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestLoginStrictRequestHandling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		mutate func(*http.Request)
		status int
		code   string
	}{
		{"wrong media type", validLogin, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415, "unsupported_media_type"},
		{"missing media type", validLogin, func(r *http.Request) { r.Header.Del("Content-Type") }, 415, "unsupported_media_type"},
		{"wrong charset", validLogin, func(r *http.Request) { r.Header.Set("Content-Type", "application/json; charset=latin1") }, 415, "unsupported_media_type"},
		{"oversized", `{"login":"` + strings.Repeat("a", 4100) + `"}`, nil, 413, "content_too_large"},
		{"duplicate key", `{"login":"a","login":"b","password":"x"}`, nil, 400, "invalid_request"},
		{"unknown key", `{"login":"a","password":"b","extra":"c"}`, nil, 400, "invalid_request"},
		{"null value", `{"login":null,"password":"b"}`, nil, 400, "invalid_request"},
		{"missing key", `{"login":"a"}`, nil, 400, "invalid_request"},
		{"wrong case key", `{"Login":"a","password":"b"}`, nil, 400, "invalid_request"},
		{"number value", `{"login":1,"password":"b"}`, nil, 400, "invalid_request"},
		{"trailing data", validLogin + ` {}`, nil, 400, "invalid_request"},
		{"array", `[]`, nil, 400, "invalid_request"},
		{"invalid utf8", "{\"login\":\"a\xff\",\"password\":\"b\"}", nil, 400, "invalid_request"},
		{"empty", ``, nil, 400, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, mux := sessionFixture("http://localhost")
			r := loginRequest(tc.body)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := serve(mux, r)
			assertProblem(t, w, tc.status, tc.code)
			if svc.loginCalls != 0 || w.Header().Get("Set-Cookie") != "" {
				t.Fatal("rejected request reached service or set a cookie")
			}
		})
	}
	_, _, mux := sessionFixture("http://localhost")
	w := serve(mux, loginRequest(validLogin, func(r *http.Request) { r.Header.Set("Content-Type", "Application/JSON; charset=UTF-8") }))
	if w.Code != 201 {
		t.Fatalf("charset utf-8 rejected: %d", w.Code)
	}
}

func TestLoginCredentialFailuresShareOneBody(t *testing.T) {
	svc, _, mux := sessionFixture("https://tendo.example")
	svc.loginErr = identity.ErrInvalidCredentials
	first := serve(mux, loginRequest(validLogin))
	assertProblem(t, first, 401, "invalid_credentials")
	if first.Header().Get("Set-Cookie") != "" {
		t.Fatal("failure set a cookie")
	}
	second := serve(mux, loginRequest(`{"login":"someone_else","password":"another password value"}`))
	if first.Body.String() != second.Body.String() {
		t.Fatalf("bodies differ: %s vs %s", first.Body, second.Body)
	}
	svc.loginErr = errors.New("db down: secret detail")
	w := serve(mux, loginRequest(validLogin))
	assertProblem(t, w, 503, "unavailable")
	if strings.Contains(w.Body.String(), "secret detail") {
		t.Fatal("internal error leaked")
	}
}

func TestLoginRateLimits(t *testing.T) {
	svc, _, mux := sessionFixture("http://localhost")
	svc.loginErr = identity.ErrInvalidCredentials
	for i := 0; i < 10; i++ {
		if w := serve(mux, loginRequest(validLogin)); w.Code != 401 {
			t.Fatalf("attempt %d status %d", i, w.Code)
		}
	}
	w := serve(mux, loginRequest(validLogin))
	assertProblem(t, w, 429, "rate_limited")
	if w.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After=%q", w.Header().Get("Retry-After"))
	}
	if svc.loginCalls != 10 {
		t.Fatalf("limited request reached service: %d", svc.loginCalls)
	}
	other := loginRequest(validLogin)
	other = httpx.WithRequestMetadata(other, httpx.RequestMetadata{ClientIP: "192.0.2.99"})
	if w := serve(mux, other); w.Code != 401 {
		t.Fatalf("other client limited: %d", w.Code)
	}
}

func TestLoginPasswordVerifierGateErrorMapsTo429(t *testing.T) {
	_, h, mux := sessionFixture("http://localhost")
	gate := security.NewPasswordGate(1)
	if _, err := gate.HashPassword("saturate the single slot with sufficiently long passphrase"); err != nil {
		t.Fatal(err)
	}
	service := &loginServiceFake{verify: func(hash, password string) (bool, error) { return false, security.ErrPasswordWorkLimit }, validHash: "invalid"}
	h.service = service
	w := serve(mux, loginRequest(validLogin))
	assertProblem(t, w, 429, "rate_limited")
	if w.Header().Get("Retry-After") != "60" || service.calls != 1 {
		t.Fatalf("Retry-After=%q calls=%d", w.Header().Get("Retry-After"), service.calls)
	}
	service.verify = gate.VerifyPassword
	service.validHash, _ = security.HashPassword("correct horse battery")
	service.err = identity.ErrInvalidCredentials
	if w := serve(mux, loginRequest(validLogin)); w.Code != 401 {
		t.Fatalf("login result=%d", w.Code)
	}
}

func getRequest(cookies ...*http.Cookie) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}

func TestGetSession(t *testing.T) {
	svc, _, mux := sessionFixture("http://localhost")
	w := serve(mux, getRequest(&http.Cookie{Name: "tendo_session", Value: fakeToken}))
	if w.Code != 200 || w.Header().Get("ETag") != `"session-sid-1"` || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v %s", w.Code, w.Header(), w.Body)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["login"] != "owner_1" || body["defaultHouseholdId"] != "h-1" || body["userId"] != "u-1" || body["expiresAt"] != "2026-02-01T00:00:00Z" {
		t.Fatalf("body=%s", w.Body)
	}
	if strings.Contains(w.Body.String(), fakeToken) {
		t.Fatal("token leaked")
	}
	for name, r := range map[string]*http.Request{
		"no cookie":         getRequest(),
		"wrong cookie name": getRequest(&http.Cookie{Name: "__Host-tendo_session", Value: fakeToken}),
		"duplicate cookie":  getRequest(&http.Cookie{Name: "tendo_session", Value: fakeToken}, &http.Cookie{Name: "tendo_session", Value: fakeToken}),
		"unknown token":     getRequest(&http.Cookie{Name: "tendo_session", Value: "nope"}),
	} {
		before := svc.lookupCalls
		w := serve(mux, r)
		assertProblem(t, w, 401, "unauthenticated")
		if name != "unknown token" && svc.lookupCalls != before {
			t.Fatalf("%s reached service", name)
		}
	}
	svc.lookupErr = errors.New("db down")
	assertProblem(t, serve(mux, getRequest(&http.Cookie{Name: "tendo_session", Value: fakeToken})), 503, "unavailable")
}

func TestHTTPSDeploymentIgnoresPlainCookieName(t *testing.T) {
	_, _, mux := sessionFixture("https://tendo.example")
	assertProblem(t, serve(mux, getRequest(&http.Cookie{Name: "tendo_session", Value: fakeToken})), 401, "unauthenticated")
	w := serve(mux, getRequest(&http.Cookie{Name: "__Host-tendo_session", Value: fakeToken}))
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
}

func TestDeleteSessionAlwaysClearsCookie(t *testing.T) {
	for _, tc := range []struct {
		name, url, cookie string
		secure            bool
	}{{"https", "https://tendo.example", "__Host-tendo_session", true}, {"http", "http://localhost", "tendo_session", false}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, mux := sessionFixture(tc.url)
			r := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
			r.AddCookie(&http.Cookie{Name: tc.cookie, Value: fakeToken})
			w := serve(mux, r)
			if w.Code != 204 || w.Body.Len() != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%d %v %q", w.Code, w.Header(), w.Body)
			}
			if svc.logouts != 1 || svc.logoutToken != fakeToken {
				t.Fatalf("logouts=%d token=%q", svc.logouts, svc.logoutToken)
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookies=%v", cookies)
			}
			c := cookies[0]
			if c.Name != tc.cookie || c.Value != "" || c.MaxAge != -1 || !c.HttpOnly || c.Secure != tc.secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" {
				t.Fatalf("clearing cookie=%+v", c)
			}
			if !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
				t.Fatalf("Set-Cookie=%q lacks Max-Age=0", w.Header().Get("Set-Cookie"))
			}
			// No cookie: still 204 and still clears; no logout call.
			svc.logouts = 0
			w = serve(mux, httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil))
			if w.Code != 204 || svc.logouts != 0 || w.Header().Get("Set-Cookie") == "" {
				t.Fatalf("anonymous delete: %d logouts=%d", w.Code, svc.logouts)
			}
		})
	}
}

func TestDeleteSessionOutageKeepsCookieForRetry(t *testing.T) {
	svc, _, mux := sessionFixture("http://localhost")
	svc.logoutErr = errors.New("db down")
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/session", nil)
	r.AddCookie(&http.Cookie{Name: "tendo_session", Value: fakeToken})
	w := serve(mux, r)
	assertProblem(t, w, 503, "unavailable")
	if svc.logouts != 1 || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("logouts=%d Set-Cookie=%q", svc.logouts, w.Header().Get("Set-Cookie"))
	}
}

func TestRequireSessionProvidesPrincipal(t *testing.T) {
	svc, h, _ := sessionFixture("http://localhost")
	var got identity.Principal
	var ok bool
	next := h.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok = PrincipalFromContext(r.Context())
		w.WriteHeader(204)
	}))
	if w := serve(next, getRequest(&http.Cookie{Name: "tendo_session", Value: fakeToken})); w.Code != 204 || !ok || got != svc.principal {
		t.Fatalf("status=%d principal=%+v ok=%v", w.Code, got, ok)
	}
	ok = false
	assertProblem(t, serve(next, getRequest()), 401, "unauthenticated")
	if ok {
		t.Fatal("handler ran without a session")
	}
	if _, present := PrincipalFromContext(context.Background()); present {
		t.Fatal("empty context yielded a principal")
	}
}

func TestOutageDoesNotLogOutButUnknownSessionClearsCookie(t *testing.T) {
	svc, _, mux := sessionFixture("http://localhost")
	cookie := &http.Cookie{Name: "tendo_session", Value: fakeToken}
	svc.lookupErr = errors.New("db down")
	w := serve(mux, getRequest(cookie))
	assertProblem(t, w, 503, "unavailable")
	if w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("outage cleared the cookie: %q", w.Header().Get("Set-Cookie"))
	}
	svc.lookupErr = identity.ErrUnauthenticated
	w = serve(mux, getRequest(cookie))
	assertProblem(t, w, 401, "unauthenticated")
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 || cookies[0].Value != "" {
		t.Fatalf("401 must clear the cookie: %v", cookies)
	}
}

func TestPostAndGetReportIdenticalSubsecondFreeExpiry(t *testing.T) {
	svc, _, mux := sessionFixture("http://localhost")
	svc.expires = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	post := serve(mux, loginRequest(validLogin))
	get := serve(mux, getRequest(&http.Cookie{Name: "tendo_session", Value: fakeToken}))
	var a, b map[string]string
	if json.Unmarshal(post.Body.Bytes(), &a) != nil || json.Unmarshal(get.Body.Bytes(), &b) != nil || a["expiresAt"] != b["expiresAt"] || a["expiresAt"] != "2026-02-01T00:00:00Z" {
		t.Fatalf("post=%s get=%s", post.Body, get.Body)
	}
}

func TestSessionLimiterIsBounded(t *testing.T) {
	l := newLimiter(10)
	for i := 0; i < limiterMaxClients; i++ {
		if !l.admitAttempt("client-" + strconv.Itoa(i)) {
			t.Fatalf("client %d refused below bound", i)
		}
	}
	if l.admitAttempt("one-too-many") || len(l.attempts) != limiterMaxClients {
		t.Fatalf("map grew past bound: %d", len(l.attempts))
	}
	now := time.Now().Add(2 * limiterWindow)
	l.now = func() time.Time { return now }
	if !l.admitAttempt("one-too-many") || len(l.attempts) > limiterMaxClients {
		t.Fatal("stale entries were not evicted")
	}
}
