package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/identity/oidcprovider"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/bigtcze/tendo/backend/internal/testoidc"
	"github.com/go-chi/chi/v5"
)

const oidcTestPassword = "correct horse battery"
const oidcTestPublicURL = "http://tendo.test"

type oidcHTTPFake struct {
	start    identity.OIDCStartResult
	startErr error
	callback identity.OIDCCallbackResult
	linked   bool
}

func (f *oidcHTTPFake) Status() (bool, string)                       { return true, "Example IdP" }
func (f *oidcHTTPFake) Linked(context.Context, string) (bool, error) { return f.linked, nil }
func (f *oidcHTTPFake) Start(context.Context, identity.OIDCStartParams) (identity.OIDCStartResult, error) {
	return f.start, f.startErr
}
func (f *oidcHTTPFake) Callback(context.Context, url.Values, string, string) (identity.OIDCCallbackResult, error) {
	return f.callback, nil
}

type oidcMemRepo struct {
	mu          sync.Mutex
	flows       map[string]identity.OIDCFlow
	identities  map[string]string
	credentials map[string]string
	sessions    map[string]identity.SessionInfo
}

func newOIDCMemRepo() *oidcMemRepo {
	return &oidcMemRepo{flows: map[string]identity.OIDCFlow{}, identities: map[string]string{}, credentials: map[string]string{}, sessions: map[string]identity.SessionInfo{}}
}
func (r *oidcMemRepo) InsertFlow(_ context.Context, f identity.OIDCFlow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flows[string(f.StateHash)] = f
	return nil
}
func (r *oidcMemRepo) ConsumeFlow(_ context.Context, state, browser []byte, now time.Time, issuer, client string) (identity.OIDCFlow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f, ok := r.flows[string(state)]
	if !ok || !bytes.Equal(f.BrowserTokenHash, browser) || f.Issuer != issuer || f.ClientID != client || !f.ExpiresAt.After(now) {
		return identity.OIDCFlow{}, identity.ErrNotFound
	}
	delete(r.flows, string(state))
	return f, nil
}
func (r *oidcMemRepo) FindIdentity(_ context.Context, issuer, subject string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.identities[issuer+"\x00"+subject]
	if !ok {
		return "", identity.ErrNotFound
	}
	return v, nil
}
func (r *oidcMemRepo) FindSession(_ context.Context, hash []byte, now time.Time) (identity.SessionInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.sessions[string(hash)]
	if !ok || !v.ExpiresAt.After(now) {
		return identity.SessionInfo{}, identity.ErrNotFound
	}
	return v, nil
}
func (r *oidcMemRepo) FindCredential(_ context.Context, user string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.credentials[user]
	if !ok {
		return "", identity.ErrNotFound
	}
	return v, nil
}
func (r *oidcMemRepo) CreateOIDCLoginSession(_ context.Context, s identity.NewSession) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := "oidc-login"
	r.sessions[string(s.TokenHash)] = identity.SessionInfo{ID: id, Principal: identity.Principal{UserID: s.UserID, Login: "owner"}, ExpiresAt: s.ExpiresAt}
	return id, nil
}
func (r *oidcMemRepo) LinkAndCreateSession(_ context.Context, f identity.OIDCFlow, v identity.OIDCVerifiedIdentity, s identity.NewSession, revoke []byte) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[string(revoke)]; !ok {
		return "", identity.ErrOIDCSessionChanged
	}
	key := v.Issuer + "\x00" + v.Subject
	if owner, ok := r.identities[key]; ok && owner != f.UserID {
		return "", identity.ErrOIDCConflict
	}
	for k, owner := range r.identities {
		if owner == f.UserID && strings.HasPrefix(k, v.Issuer+"\x00") && k != key {
			return "", identity.ErrOIDCConflict
		}
	}
	delete(r.sessions, string(revoke))
	r.identities[key] = f.UserID
	id := "oidc-link"
	r.sessions[string(s.TokenHash)] = identity.SessionInfo{ID: id, Principal: identity.Principal{UserID: s.UserID, Login: "owner"}, ExpiresAt: s.ExpiresAt}
	return id, nil
}
func (r *oidcMemRepo) Linked(_ context.Context, user, issuer string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, u := range r.identities {
		if u == user && strings.HasPrefix(k, issuer+"\x00") {
			return true, nil
		}
	}
	return false, nil
}

func oidcRouter(service oidcService, publicURL string, sessionService sessionService) http.Handler {
	r := chi.NewRouter()
	session := NewSession(sessionService, publicURL)
	session.Register(r)
	NewOIDC(service, session, publicURL).Register(r)
	return r
}
func TestOIDCStatusShapes(t *testing.T) {
	for _, tc := range []struct {
		service oidcService
		want    string
	}{{nil, `{"enabled":false}`}, {&oidcHTTPFake{}, `{"displayName":"Example IdP","enabled":true}`}} {
		w := httptest.NewRecorder()
		oidcRouter(tc.service, "http://localhost", &fakeSessionService{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc", nil))
		if w.Code != 200 || strings.TrimSpace(w.Body.String()) != tc.want {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
	}
}
func TestOIDCIdentitySessionAndDisabled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		service oidcService
		cookie  string
		status  int
		body    string
	}{{"disabled", nil, fakeToken, 404, "oidc_disabled"}, {"unauthenticated", &oidcHTTPFake{}, "", 401, "unauthenticated"}, {"linked", &oidcHTTPFake{linked: true}, fakeToken, 200, `{"linked":true}`}} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/identity", nil)
			if tc.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "tendo_session", Value: tc.cookie})
			}
			w := httptest.NewRecorder()
			oidcRouter(tc.service, "http://localhost", &fakeSessionService{principal: identity.Principal{UserID: "u", Login: "owner"}}).ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.status == 200 && strings.TrimSpace(w.Body.String()) != tc.body {
				t.Fatalf("body=%s", w.Body)
			}
			if tc.status != 200 && !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("body=%s", w.Body)
			}
		})
	}
}
func TestOIDCStartValidationAndProviderFailure(t *testing.T) {
	for _, tc := range []struct {
		body    string
		service oidcService
		status  int
		code    string
	}{{`{"purpose":"invalid"}`, &oidcHTTPFake{}, 422, "purpose"}, {`{"purpose":"login","currentPassword":"secret"}`, &oidcHTTPFake{}, 422, "purpose"}, {`{"purpose":"login"}`, &oidcHTTPFake{startErr: identity.ErrOIDCUnavailable}, 503, "oidc_unavailable"}, {`{"purpose":"login"}`, nil, 404, "oidc_disabled"}} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/start", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		oidcRouter(tc.service, "http://localhost", &fakeSessionService{}).ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body)
		}
	}
}
func TestOIDCStartCookiePolicy(t *testing.T) {
	for _, tc := range []struct {
		public, name string
		secure       bool
	}{{"http://localhost", "tendo_oidc", false}, {"https://example.test", "__Host-tendo_oidc", true}} {
		fake := &oidcHTTPFake{start: identity.OIDCStartResult{AuthorizationURL: "https://issuer.test/authorize", BrowserToken: fakeToken}}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/start", strings.NewReader(`{"purpose":"login"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		oidcRouter(fake, tc.public, &fakeSessionService{}).ServeHTTP(w, r)
		cookies := w.Result().Cookies()
		if w.Code != 200 || len(cookies) != 1 {
			t.Fatalf("status=%d cookies=%v", w.Code, cookies)
		}
		c := cookies[0]
		if c.Name != tc.name || c.Value != fakeToken || !c.HttpOnly || c.Secure != tc.secure || c.Path != "/" || c.MaxAge != 600 || c.SameSite != http.SameSiteLaxMode || c.Domain != "" {
			t.Fatalf("cookie=%+v", c)
		}
	}
}
func TestOIDCCallbackRedirectHeadersAndMultipleCookies(t *testing.T) {
	fake := &oidcHTTPFake{callback: identity.OIDCCallbackResult{Destination: "/", SessionToken: fakeToken}}
	w := httptest.NewRecorder()
	oidcRouter(fake, "http://localhost", &fakeSessionService{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=secret&state=opaque", nil))
	if w.Code != 303 || w.Header().Get("Location") != "/" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Body.Len() != 0 {
		t.Fatalf("status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	if len(w.Result().Cookies()) != 2 {
		t.Fatalf("cookies=%v", w.Result().Cookies())
	}
}

func newRealOIDCHTTP(t *testing.T, subject string) (*oidcMemRepo, *testoidc.Provider, *identity.OIDCService, *fakeSessionService, http.Handler) {
	t.Helper()
	publicURL := oidcTestPublicURL
	callback := publicURL + "/api/v1/auth/oidc/callback"
	provider, err := testoidc.NewWithOptions("", "client", "secret", callback, []string{"subject-1", subject})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.Close)
	if subject != "" {
		provider.Subject = subject
		provider.Subjects = []string{subject}
	}
	repo := newOIDCMemRepo()
	hash, err := security.HashPassword(oidcTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	repo.credentials["user-1"] = hash
	oldToken := "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	oldHash := sha256.Sum256([]byte(oldToken))
	repo.sessions[string(oldHash[:])] = identity.SessionInfo{ID: "old-session", Principal: identity.Principal{UserID: "user-1", Login: "owner"}, ExpiresAt: time.Now().Add(time.Hour)}
	providerAdapter := oidcprovider.New(provider.Issuer(), provider.ClientID, provider.ClientSecret, nil, time.Now, true)
	service, err := identity.NewOIDCService(repo, providerAdapter, provider.Issuer(), provider.ClientID, "Test IdP", publicURL, time.Now, nil, func(hash, password string) error {
		ok, e := security.VerifyPassword(hash, password)
		if e != nil {
			return e
		}
		if !ok {
			return identity.ErrInvalidCredentials
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessionService{principal: identity.Principal{UserID: "user-1", Login: "owner"}, expires: time.Now().Add(time.Hour)}
	return repo, provider, service, sessions, oidcRouter(service, publicURL, sessions)
}

type callbackCapturingProvider struct {
	identity.OIDCProvider
	params identity.OIDCAuthorizationParams
}

func (p *callbackCapturingProvider) AuthorizationURL(ctx context.Context, params identity.OIDCAuthorizationParams) (string, error) {
	p.params = params
	return p.OIDCProvider.AuthorizationURL(ctx, params)
}

func doOIDCStart(t *testing.T, mux http.Handler, purpose string, currentSession string) (url.Values, string) {
	t.Helper()
	body := `{"purpose":"` + purpose + `"}`
	if purpose == "link" {
		body = `{"purpose":"link","currentPassword":"` + oidcTestPassword + `"}`
	}
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/start", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", oidcTestPublicURL)
	if currentSession != "" {
		r.AddCookie(&http.Cookie{Name: "tendo_session", Value: currentSession})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("start status=%d body=%s", w.Code, w.Body)
	}
	var result struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	return mustParseURL(t, result.AuthorizationURL), cookie.Value
}
func mustParseURL(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}
func performOIDCCallback(t *testing.T, p *testoidc.Provider, mux http.Handler, authQuery url.Values, flowCookie, oldSession string) *httptest.ResponseRecorder {
	t.Helper()
	authURL := p.Issuer() + "/authorize?" + authQuery.Encode()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		b, _ := io.ReadAll(response.Body)
		t.Fatalf("authorize status=%d body=%s", response.StatusCode, b)
	}
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, location.RequestURI(), nil)
	r.AddCookie(&http.Cookie{Name: "tendo_oidc", Value: flowCookie})
	if oldSession != "" {
		r.AddCookie(&http.Cookie{Name: "tendo_session", Value: oldSession})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func TestOIDCLoginHTTPRoundTrip(t *testing.T) {
	repo, p, _, _, mux := newRealOIDCHTTP(t, "subject-1")
	repo.identities[p.Issuer()+"\x00subject-1"] = "user-1"
	query, flow := doOIDCStart(t, mux, "login", "")
	w := performOIDCCallback(t, p, mux, query, flow, "")
	if w.Code != 303 || w.Header().Get("Location") != "/" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Body.Len() != 0 {
		t.Fatalf("status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cookies=%v", cookies)
	}
	var session string
	cleared := false
	for _, c := range cookies {
		if c.Name == "tendo_oidc" && c.MaxAge < 0 && c.Value == "" {
			cleared = true
		}
		if c.Name == "tendo_session" && c.Value != "" {
			session = c.Value
		}
	}
	if !cleared || session == "" {
		t.Fatalf("flow cleared=%v session=%q", cleared, session)
	}
	sum := sha256.Sum256([]byte(session))
	if _, ok := repo.sessions[string(sum[:])]; !ok {
		t.Fatal("new session not stored")
	}
}
func TestOIDCLinkHTTPRoundTripRevokesOldSession(t *testing.T) {
	repo, p, _, _, mux := newRealOIDCHTTP(t, "subject-1")
	old := "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	query, flow := doOIDCStart(t, mux, "link", old)
	w := performOIDCCallback(t, p, mux, query, flow, old)
	if w.Code != 303 || w.Header().Get("Location") != "/account#oidc=connected" || w.Header().Get("Referrer-Policy") != "no-referrer" || w.Body.Len() != 0 {
		t.Fatalf("status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
	var session string
	for _, c := range w.Result().Cookies() {
		if c.Name == "tendo_session" {
			session = c.Value
		}
	}
	if session == "" {
		t.Fatal("missing new session")
	}
	oldHash := sha256.Sum256([]byte(old))
	if _, ok := repo.sessions[string(oldHash[:])]; ok {
		t.Fatal("old session remains active")
	}
	newHash := sha256.Sum256([]byte(session))
	if _, ok := repo.sessions[string(newHash[:])]; !ok {
		t.Fatal("new session not stored")
	}
	linked, err := repo.Linked(context.Background(), "user-1", p.Issuer())
	if err != nil || !linked {
		t.Fatalf("linked=%v err=%v", linked, err)
	}
}
func TestOIDCUnknownIdentityDoesNotSetSession(t *testing.T) {
	_, p, _, _, mux := newRealOIDCHTTP(t, "unknown-subject")
	query, flow := doOIDCStart(t, mux, "login", "")
	w := performOIDCCallback(t, p, mux, query, flow, "")
	if w.Code != 303 || w.Header().Get("Location") != "/login#oidcError=identity_not_linked" || w.Body.Len() != 0 {
		t.Fatalf("status=%d location=%s body=%q", w.Code, w.Header().Get("Location"), w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "tendo_session" {
			t.Fatalf("session cookie set on failure: %+v", c)
		}
	}
}
func TestOIDCRedirectURIIgnoresForgedHost(t *testing.T) {
	_, p, _, _, mux := newRealOIDCHTTP(t, "subject-1")
	body := `{"purpose":"login"}`
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/start", strings.NewReader(body))
	r.RemoteAddr = "203.0.113.10:1234"
	r.Host = "forged.test"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", oidcTestPublicURL)
	r.Header.Set("X-Forwarded-Host", "attacker.test")
	r.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var out struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	u, _ := url.Parse(out.AuthorizationURL)
	if got := u.Query().Get("redirect_uri"); got != oidcTestPublicURL+"/api/v1/auth/oidc/callback" {
		t.Fatalf("redirect_uri=%q", got)
	}
	_ = p
}
func TestOIDCCallbackRequestLogOmitsSecrets(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	callback := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusSeeOther) })
	wrapped := httpx.RequestMiddlewareForTest(callback, logger)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?code=private-code&state=private-state", nil)
	r.AddCookie(&http.Cookie{Name: "tendo_oidc", Value: "private-cookie"})
	wrapped.ServeHTTP(httptest.NewRecorder(), r)
	for _, secret := range []string{"private-code", "private-state", "private-cookie", "code="} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("request log contains %q: %s", secret, logs.String())
		}
	}
}
