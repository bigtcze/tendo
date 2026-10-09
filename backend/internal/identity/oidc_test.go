package identity

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

const oidcTestPassword = "correct horse battery"
const oidcIssuer = "https://idp.test"

type oidcFakeRepo struct {
	mu                        sync.Mutex
	flows                     map[string]OIDCFlow
	identities                map[string]string
	credentials               map[string]string
	sessions                  map[string]SessionInfo
	identityErr, sessionErr   error
	consumeErr                error
	inserted, created, linked int
}

func newOIDCFakeRepo() *oidcFakeRepo {
	return &oidcFakeRepo{flows: map[string]OIDCFlow{}, identities: map[string]string{}, credentials: map[string]string{}, sessions: map[string]SessionInfo{}}
}
func identityKey(issuer, sub string) string { return issuer + "\x00" + sub }
func (r *oidcFakeRepo) InsertFlow(_ context.Context, f OIDCFlow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(f.PreviousBrowserTokenHash) == 32 {
		for state, old := range r.flows {
			if string(old.BrowserTokenHash) == string(f.PreviousBrowserTokenHash) {
				delete(r.flows, state)
			}
		}
	}
	r.flows[string(f.StateHash)] = f
	r.inserted++
	return nil
}
func (r *oidcFakeRepo) ConsumeFlow(_ context.Context, state, browser []byte, now time.Time, issuer, client string) (OIDCFlow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.consumeErr != nil {
		return OIDCFlow{}, r.consumeErr
	}
	k := string(state)
	f, ok := r.flows[k]
	if !ok || string(f.BrowserTokenHash) != string(browser) || f.Issuer != issuer || f.ClientID != client || !f.ExpiresAt.After(now) {
		return OIDCFlow{}, ErrNotFound
	}
	delete(r.flows, k)
	return f, nil
}
func (r *oidcFakeRepo) FindIdentity(_ context.Context, issuer, subject string) (string, error) {
	if r.identityErr != nil {
		return "", r.identityErr
	}
	u, ok := r.identities[identityKey(issuer, subject)]
	if !ok {
		return "", ErrNotFound
	}
	return u, nil
}
func (r *oidcFakeRepo) FindSession(_ context.Context, hash []byte, now time.Time) (SessionInfo, error) {
	if r.sessionErr != nil {
		return SessionInfo{}, r.sessionErr
	}
	s, ok := r.sessions[string(hash)]
	if !ok || !s.ExpiresAt.After(now) {
		return SessionInfo{}, ErrNotFound
	}
	return s, nil
}
func (r *oidcFakeRepo) FindCredential(_ context.Context, user string) (string, error) {
	v, ok := r.credentials[user]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}
func (r *oidcFakeRepo) CreateOIDCLoginSession(_ context.Context, s NewSession) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := "login-session"
	r.sessions[string(s.TokenHash)] = SessionInfo{ID: id, Principal: Principal{UserID: s.UserID}, ExpiresAt: s.ExpiresAt}
	r.created++
	return id, nil
}
func (r *oidcFakeRepo) LinkAndCreateSession(_ context.Context, f OIDCFlow, v OIDCVerifiedIdentity, s NewSession, revoke []byte) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := identityKey(v.Issuer, v.Subject)
	if owner, ok := r.identities[key]; ok && owner != f.UserID {
		return "", ErrOIDCConflict
	}
	for k, owner := range r.identities {
		if owner == f.UserID && strings.HasPrefix(k, v.Issuer+"\x00") && k != key {
			return "", ErrOIDCConflict
		}
	}
	if _, ok := r.sessions[string(revoke)]; !ok {
		return "", ErrOIDCSessionChanged
	}
	delete(r.sessions, string(revoke))
	r.identities[key] = f.UserID
	id := "linked-session"
	r.sessions[string(s.TokenHash)] = SessionInfo{ID: id, Principal: Principal{UserID: s.UserID}, ExpiresAt: s.ExpiresAt}
	r.created++
	r.linked++
	return id, nil
}
func (r *oidcFakeRepo) Linked(_ context.Context, user, issuer string) (bool, error) {
	for k, u := range r.identities {
		if u == user && strings.HasPrefix(k, issuer+"\x00") {
			return true, nil
		}
	}
	return false, nil
}

type oidcStubProvider struct {
	identity    OIDCVerifiedIdentity
	exchangeErr error
	calls       int
	auth        OIDCAuthorizationParams
}

func (p *oidcStubProvider) AuthorizationURL(_ context.Context, a OIDCAuthorizationParams) (string, error) {
	p.auth = a
	v := url.Values{"state": {a.State}, "redirect_uri": {a.RedirectURL}, "code_challenge_method": {"S256"}, "code_challenge": {"stub-challenge"}, "scope": {"openid"}}
	if a.Purpose == OIDCPurposeLink {
		v.Set("prompt", "login")
	}
	return "https://provider.test/auth?" + v.Encode(), nil
}
func (p *oidcStubProvider) Exchange(_ context.Context, x OIDCExchangeParams) (OIDCVerifiedIdentity, error) {
	p.calls++
	return p.identity, p.exchangeErr
}

type oidcFixture struct {
	t        *testing.T
	repo     *oidcFakeRepo
	service  *OIDCService
	clock    *time.Time
	provider *oidcStubProvider
	secret   string
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	h, err := security.HashPassword(oidcTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	repo := newOIDCFakeRepo()
	repo.credentials["user-1"] = h
	now := time.Now().UTC().Truncate(time.Second)
	stub := &oidcStubProvider{identity: OIDCVerifiedIdentity{Issuer: oidcIssuer, Subject: "subject-1"}}
	var seq byte = 1
	random := func(b []byte) error {
		for i := range b {
			b[i] = seq
			seq++
		}
		return nil
	}
	svc, err := NewOIDCService(repo, stub, oidcIssuer, "client-1", "Test IdP", "https://app.test", func() time.Time { return now }, random, func(hash, password string) error {
		ok, err := security.VerifyPassword(hash, password)
		if err != nil {
			return err
		}
		if !ok {
			return ErrInvalidCredentials
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return &oidcFixture{t: t, repo: repo, service: svc, clock: &now, provider: stub, secret: h}
}
func (f *oidcFixture) start(purpose OIDCPurpose, user, sessionID, token, password string) OIDCStartResult {
	return f.startWithPrevious(purpose, user, sessionID, token, password, "")
}
func (f *oidcFixture) startWithPrevious(purpose OIDCPurpose, user, sessionID, token, password, previous string) OIDCStartResult {
	f.t.Helper()
	pr := Principal{}
	si := SessionInfo{}
	if user != "" {
		pr = Principal{UserID: user}
		si = SessionInfo{ID: sessionID, Principal: Principal{UserID: user}, ExpiresAt: f.clock.Add(time.Hour)}
		if token != "" {
			f.repo.sessions[string(digest([]byte(token)))] = si
		}
	}
	r, e := f.service.Start(context.Background(), OIDCStartParams{Purpose: purpose, Principal: pr, Session: si, CurrentPassword: password, PreviousBrowserToken: previous})
	if e != nil {
		f.t.Fatalf("start %s: %v", purpose, e)
	}
	return r
}
func callbackParams(state, code string) url.Values {
	v := url.Values{}
	v.Set("state", state)
	if code != "" {
		v.Set("code", code)
	}
	return v
}
func startState(t *testing.T, r OIDCStartResult) string {
	t.Helper()
	u, e := url.Parse(r.AuthorizationURL)
	if e != nil {
		t.Fatal(e)
	}
	return u.Query().Get("state")
}
func invoke(t *testing.T, s *OIDCService, r OIDCStartResult, params url.Values, session string) OIDCCallbackResult {
	t.Helper()
	if params.Get("state") == "" {
		params.Set("state", startState(t, r))
	}
	got, e := s.Callback(context.Background(), params, r.BrowserToken, session)
	if e != nil {
		t.Fatal(e)
	}
	return got
}
func addIdentity(r *oidcFakeRepo, user, subject string) {
	r.identities[identityKey(oidcIssuer, subject)] = user
}
func assertNoSessionIdentity(t *testing.T, r *oidcFakeRepo) {
	t.Helper()
	if len(r.sessions) != 0 || len(r.identities) != 0 {
		t.Fatalf("sessions=%d identities=%v", len(r.sessions), r.identities)
	}
}

func TestOIDCLoginPresentedActiveSessionRejectedBeforeExchange(t *testing.T) {
	f := newOIDCFixture(t)
	token := "active-ordinary-session"
	f.repo.sessions[string(digest([]byte(token)))] = SessionInfo{ID: "active", Principal: Principal{UserID: "u"}, ExpiresAt: f.clock.Add(time.Hour)}
	start := f.start(OIDCPurposeLogin, "", "", "", "")
	result := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), token)
	if result.Destination != "/login#oidcError=session_changed" || f.provider.calls != 0 || len(f.repo.sessions) != 1 || len(f.repo.identities) != 0 {
		t.Fatalf("result=%+v calls=%d sessions=%d identities=%d", result, f.provider.calls, len(f.repo.sessions), len(f.repo.identities))
	}
}
func TestOIDCLoginPresentedSessionLookupOutageUnavailable(t *testing.T) {
	f := newOIDCFixture(t)
	f.repo.sessionErr = errors.New("database down")
	start := f.start(OIDCPurposeLogin, "", "", "", "")
	token := "token"
	r := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), token)
	if r.Destination != "/login#oidcError=unavailable" || f.provider.calls != 0 {
		t.Fatalf("%+v calls=%d", r, f.provider.calls)
	}
}

func TestOIDCLoginUnknownAndKnownIdentityState(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		f := newOIDCFixture(t)
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		state := startState(t, start)
		res := invoke(t, f.service, start, callbackParams(state, "code"), "")
		if res.Destination != "/login#oidcError=identity_not_linked" {
			t.Fatalf("%+v", res)
		}
		assertNoSessionIdentity(t, f.repo)
		if len(f.repo.flows) != 0 {
			t.Fatal("flow not consumed")
		}
	})
	t.Run("known", func(t *testing.T) {
		f := newOIDCFixture(t)
		addIdentity(f.repo, "user-1", "subject-1")
		presented := "ordinary-session-cookie"
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), presented)
		if res.Destination != "/" || res.SessionToken == "" || res.SessionToken == presented {
			t.Fatalf("%+v", res)
		}
		sum := sha256.Sum256([]byte(res.SessionToken))
		if len(f.repo.sessions) != 1 || f.repo.sessions[string(sum[:])].Principal.UserID != "user-1" {
			t.Fatalf("session state: %+v", f.repo.sessions)
		}
		if len(f.repo.flows) != 0 {
			t.Fatal("flow remains")
		}
	})
}
func TestOIDCLinkSuccessIdempotentAndConflict(t *testing.T) {
	t.Run("success idempotent", func(t *testing.T) {
		f := newOIDCFixture(t)
		old := "initiating-token"
		start := f.start(OIDCPurposeLink, "user-1", "session-1", old, oidcTestPassword)
		res := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), old)
		if res.Destination != "/account#oidc=connected" || res.SessionToken == "" {
			t.Fatalf("%+v", res)
		}
		if _, ok := f.repo.sessions[string(digest([]byte(old)))]; ok {
			t.Fatal("initiating session kept")
		}
		if f.repo.identities[identityKey(oidcIssuer, "subject-1")] != "user-1" || len(f.repo.sessions) != 1 {
			t.Fatalf("identity/session state=%v/%v", f.repo.identities, f.repo.sessions)
		}
		againOld := res.SessionToken
		st2 := f.start(OIDCPurposeLink, "user-1", "linked-session", againOld, oidcTestPassword)
		res2 := invoke(t, f.service, st2, callbackParams(startState(t, st2), "code"), againOld)
		if res2.Destination != "/account#oidc=connected" || len(f.repo.identities) != 1 || len(f.repo.sessions) != 1 {
			t.Fatalf("idempotent result=%+v identities=%v sessions=%v", res2, f.repo.identities, f.repo.sessions)
		}
	})
	t.Run("another owner", func(t *testing.T) {
		f := newOIDCFixture(t)
		addIdentity(f.repo, "user-2", "subject-1")
		old := "initiating"
		st := f.start(OIDCPurposeLink, "user-1", "s1", old, oidcTestPassword)
		res := invoke(t, f.service, st, callbackParams(startState(t, st), "code"), old)
		if res.Destination != "/account#oidcError=identity_conflict" {
			t.Fatalf("%+v", res)
		}
		if _, ok := f.repo.sessions[string(digest([]byte(old)))]; !ok || f.repo.created != 0 || f.repo.identities[identityKey(oidcIssuer, "subject-1")] != "user-2" {
			t.Fatalf("state sessions=%v identities=%v", f.repo.sessions, f.repo.identities)
		}
	})
	t.Run("different subject", func(t *testing.T) {
		f := newOIDCFixture(t)
		addIdentity(f.repo, "user-1", "other-subject")
		old := "initiating"
		st := f.start(OIDCPurposeLink, "user-1", "s1", old, oidcTestPassword)
		res := invoke(t, f.service, st, callbackParams(startState(t, st), "code"), old)
		if res.Destination != "/account#oidcError=identity_conflict" || f.repo.created != 0 {
			t.Fatalf("%+v sessions=%v", res, f.repo.sessions)
		}
	})
}
func TestOIDCLinkSessionChangedPrecheckAndConflict(t *testing.T) {
	f := newOIDCFixture(t)
	old := "revoked"
	st := f.start(OIDCPurposeLink, "user-1", "s1", old, oidcTestPassword)
	delete(f.repo.sessions, string(digest([]byte(old))))
	other := "other-user-cookie"
	f.repo.sessions[string(digest([]byte(other)))] = SessionInfo{ID: "s2", Principal: Principal{UserID: "user-2"}, ExpiresAt: f.clock.Add(time.Hour)}
	res := invoke(t, f.service, st, callbackParams(startState(t, st), "code"), other)
	if res.Destination != "/account#oidcError=session_changed" || f.provider.calls != 0 {
		t.Fatalf("res=%+v exchanges=%d", res, f.provider.calls)
	}
	if len(f.repo.identities) != 0 || f.repo.created != 0 {
		t.Fatal("state mutated")
	}
}
func TestOIDCLatestStartWinsForSameBrowser(t *testing.T) {
	f := newOIDCFixture(t)
	first := f.start(OIDCPurposeLogin, "", "", "", "")
	firstState := startState(t, first)
	second, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin, PreviousBrowserToken: first.BrowserToken})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.repo.flows) != 1 {
		t.Fatalf("flows=%d", len(f.repo.flows))
	}
	replay := invoke(t, f.service, first, callbackParams(firstState, "code"), "")
	if replay.Destination != "/login#oidcError=invalid_flow" {
		t.Fatalf("first flow remained: %+v", replay)
	}
	if _, ok := f.repo.flows[string(func() []byte { raw, _ := base64.RawURLEncoding.DecodeString(startState(t, second)); return digest(raw) }())]; !ok {
		t.Fatal("latest flow missing")
	}
}
func TestOIDCConsumeRepositoryOutageUnavailable(t *testing.T) {
	f := newOIDCFixture(t)
	start := f.start(OIDCPurposeLogin, "", "", "", "")
	f.repo.consumeErr = errors.New("db down")
	r := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
	if r.Destination != "/login#oidcError=unavailable" {
		t.Fatalf("%+v", r)
	}
}
func TestOIDCCallbackConsumeReplayCancelAndStrictParams(t *testing.T) {
	t.Run("replay", func(t *testing.T) {
		f := newOIDCFixture(t)
		addIdentity(f.repo, "user-1", "subject-1")
		st := f.start(OIDCPurposeLogin, "", "", "", "")
		params := callbackParams(startState(t, st), "code")
		if r := invoke(t, f.service, st, params, ""); r.Destination != "/" {
			t.Fatal(r)
		}
		r := invoke(t, f.service, st, params, "")
		if r.Destination != "/login#oidcError=invalid_flow" {
			t.Fatal(r)
		}
	})
	t.Run("cancel consumes by purpose", func(t *testing.T) {
		for _, p := range []OIDCPurpose{OIDCPurposeLogin, OIDCPurposeLink} {
			f := newOIDCFixture(t)
			user, session, token, password := "", "", "", ""
			if p == OIDCPurposeLink {
				user, session, token, password = "user-1", "session", "caller-token", oidcTestPassword
			}
			st := f.start(p, user, session, token, password)
			params := url.Values{"state": {startState(t, st)}, "error": {"access_denied"}}
			dest := "/login#oidcError=cancelled"
			if p == OIDCPurposeLink {
				dest = "/account#oidcError=cancelled"
			}
			r := invoke(t, f.service, st, params, token)
			if r.Destination != dest || len(f.repo.flows) != 0 {
				t.Fatalf("purpose=%s result=%+v flow=%v", p, r, f.repo.flows)
			}
		}
	})
	t.Run("invalid inputs", func(t *testing.T) {
		f := newOIDCFixture(t)
		st := f.start(OIDCPurposeLogin, "", "", "", "")
		for _, tc := range []struct {
			name   string
			params url.Values
			cookie string
		}{{"missing cookie", url.Values{"state": {startState(t, st)}, "code": {"x"}}, ""}, {"invalid cookie", url.Values{"state": {startState(t, st)}, "code": {"x"}}, "bad"}, {"duplicate state", url.Values{"state": {startState(t, st), startState(t, st)}, "code": {"x"}}, st.BrowserToken}, {"duplicate code", url.Values{"state": {startState(t, st)}, "code": {"x", "y"}}, st.BrowserToken}, {"oversized code", url.Values{"state": {startState(t, st)}, "code": {strings.Repeat("x", 4097)}}, st.BrowserToken}} {
			r, _ := f.service.Callback(context.Background(), tc.params, tc.cookie, "")
			if r.Destination != "/login#oidcError=invalid_flow" {
				t.Errorf("%s: %+v", tc.name, r)
			}
		}
		if len(f.repo.flows) != 1 {
			t.Fatal("invalid inputs consumed flow")
		}
	})
}
func TestOIDCExchangeAndRepositoryFailuresMapByPurpose(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		exchangeErr, errorRepo bool
		want                   string
	}{{"exchange", true, false, "authentication_failed"}, {"identity outage", false, true, "unavailable"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOIDCFixture(t)
			f.provider.exchangeErr = errors.New("upstream")
			if tc.errorRepo {
				f.provider.exchangeErr = nil
				f.repo.identityErr = errors.New("db down")
			}
			st := f.start(OIDCPurposeLogin, "", "", "", "")
			r := invoke(t, f.service, st, callbackParams(startState(t, st), "code"), "")
			if r.Destination != "/login#oidcError="+tc.want || len(f.repo.sessions) != 0 {
				t.Fatalf("%+v sessions=%v", r, f.repo.sessions)
			}
		})
	}
}
func TestOIDCStartValidationPasswordAndURL(t *testing.T) {
	f := newOIDCFixture(t)
	if _, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: "bad"}); !errors.Is(err, ErrOIDCInvalidPurpose) {
		t.Fatalf("purpose err=%v", err)
	}
	if _, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin, Session: SessionInfo{ID: "active"}}); !errors.Is(err, ErrOIDCAlreadySignedIn) {
		t.Fatalf("signed-in err=%v", err)
	}
	if _, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLink, Principal: Principal{UserID: "user-1"}, Session: SessionInfo{ID: "s1", Principal: Principal{UserID: "user-1"}}, CurrentPassword: strings.Repeat("x", 513)}); !errors.Is(err, ErrInvalidCredentials) || f.repo.inserted != 0 {
		t.Fatalf("wrong password err=%v inserted=%d", err, f.repo.inserted)
	}
	if _, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLink}); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing session err=%v", err)
	}
	st, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLink, Principal: Principal{UserID: "user-1"}, Session: SessionInfo{ID: "s1", Principal: Principal{UserID: "user-1"}}, CurrentPassword: oidcTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	flow := onlyFlow(t, f.repo)
	if flow.UserID != "user-1" || flow.SessionID != "s1" || len(flow.StateHash) != 32 || len(flow.BrowserTokenHash) != 32 {
		t.Fatalf("stored flow=%+v", flow)
	}
	q, _ := url.Parse(st.AuthorizationURL)
	vals := q.Query()
	if vals.Get("code_challenge_method") != "S256" || vals.Get("prompt") != "login" || vals.Get("scope") != "openid" || vals.Get("redirect_uri") != "https://app.test/api/v1/auth/oidc/callback" {
		t.Fatalf("link auth params=%v", vals)
	}
	login, _ := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLogin})
	lq, _ := url.Parse(login.AuthorizationURL)
	if lq.Query().Get("prompt") != "" {
		t.Fatalf("login prompt=%q", lq.Query().Get("prompt"))
	}
}
func onlyFlow(t *testing.T, r *oidcFakeRepo) OIDCFlow {
	t.Helper()
	if len(r.flows) != 1 {
		t.Fatalf("flows=%d", len(r.flows))
	}
	for _, f := range r.flows {
		return f
	}
	panic("unreachable")
}
func TestOIDCCancelledStartAndRepositoryContextBounded(t *testing.T) {
	f := newOIDCFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.service.Start(ctx, OIDCStartParams{Purpose: OIDCPurposeLogin}); !errors.Is(err, ErrOIDCUnavailable) {
		t.Fatalf("cancelled start err=%v", err)
	}
}
func TestOIDCStartPropagatesPasswordGateWorkLimit(t *testing.T) {
	f := newOIDCFixture(t)
	f.service.verifyPassword = func(string, string) error { return security.ErrPasswordWorkLimit }
	_, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLink, Principal: Principal{UserID: "user-1"}, Session: SessionInfo{ID: "s", Principal: Principal{UserID: "user-1"}}, CurrentPassword: oidcTestPassword})
	if !errors.Is(err, security.ErrPasswordWorkLimit) || f.repo.inserted != 0 {
		t.Fatalf("err=%v inserted=%d", err, f.repo.inserted)
	}
}

var _ OIDCRepository = (*oidcFakeRepo)(nil)
