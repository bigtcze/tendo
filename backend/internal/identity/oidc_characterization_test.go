package identity

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestOIDCCallbackStrictTokensRemainUnconsumed(t *testing.T) {
	for _, tc := range []struct{ name, state, browser string }{
		{"noncanonical state", strings.Repeat("!", 43), ""},
		{"wrong decoded length", strings.Repeat("A", 43), ""},
		{"noncanonical browser", "", strings.Repeat("!", 43)},
		{"wrong browser length", "", strings.Repeat("A", 43)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOIDCFixture(t)
			start := f.start(OIDCPurposeLogin, "", "", "", "")
			params := callbackParams(startState(t, start), "code")
			browser := start.BrowserToken
			if tc.state != "" {
				params.Set("state", tc.state)
			}
			if tc.browser != "" {
				browser = tc.browser
			}
			got, err := f.service.Callback(context.Background(), params, browser, "")
			if err != nil || got.Destination != "/login#oidcError=invalid_flow" || got.Code != OIDCInvalidFlow || got.SessionToken != "" || f.provider.calls != 0 || len(f.repo.flows) != 1 {
				t.Fatalf("result=%+v err=%v calls=%d flows=%d", got, err, f.provider.calls, len(f.repo.flows))
			}
		})
	}
}

func TestOIDCCallbackConsumedMissingCodeByPurpose(t *testing.T) {
	for _, purpose := range []OIDCPurpose{OIDCPurposeLogin, OIDCPurposeLink} {
		t.Run(string(purpose), func(t *testing.T) {
			f := newOIDCFixture(t)
			token := ""
			if purpose == OIDCPurposeLink {
				token = "initiating"
			}
			user, session, password := "", "", ""
			if purpose == OIDCPurposeLink {
				user, session, password = "user-1", "session-1", oidcTestPassword
			}
			start := f.start(purpose, user, session, token, password)
			got := invoke(t, f.service, start, callbackParams(startState(t, start), ""), token)
			want := "/login#oidcError=invalid_flow"
			if purpose == OIDCPurposeLink {
				want = "/account#oidcError=invalid_flow"
			}
			if got.Destination != want || got.SessionToken != "" || len(f.repo.flows) != 0 || f.provider.calls != 0 {
				t.Fatalf("result=%+v flows=%d calls=%d", got, len(f.repo.flows), f.provider.calls)
			}
		})
	}
}

func TestOIDCCallbackLinkSessionGatesPrecedeExchange(t *testing.T) {
	for _, tc := range []struct {
		name       string
		token      string
		sessionErr error
		activeID   string
		want       string
	}{
		{"missing", "gone", nil, "", "/account#oidcError=session_changed"},
		{"outage", "outage", errors.New("database down"), "", "/account#oidcError=unavailable"},
		{"empty token", "", nil, "", "/account#oidcError=session_changed"},
		{"different session", "other", nil, "different-id", "/account#oidcError=session_changed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOIDCFixture(t)
			start := f.start(OIDCPurposeLink, "user-1", "original-id", "initiating", oidcTestPassword)
			if tc.sessionErr != nil {
				f.repo.sessionErr = tc.sessionErr
			}
			if tc.activeID != "" {
				f.repo.sessions[string(digest([]byte(tc.token)))] = SessionInfo{ID: tc.activeID, Principal: Principal{UserID: "user-1"}, ExpiresAt: f.clock.Add(1)}
			}
			got := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), tc.token)
			if got.Destination != tc.want || got.SessionToken != "" || f.provider.calls != 0 {
				t.Fatalf("result=%+v calls=%d", got, f.provider.calls)
			}
		})
	}
}

func TestOIDCCallbackProviderUnavailableAndPersistenceFailures(t *testing.T) {
	for _, purpose := range []OIDCPurpose{OIDCPurposeLogin, OIDCPurposeLink} {
		t.Run(string(purpose)+" wrapped provider unavailable", func(t *testing.T) {
			f := newOIDCFixture(t)
			f.provider.exchangeErr = errors.Join(errors.New("wrapped"), ErrOIDCProviderUnavailable)
			token := ""
			if purpose == OIDCPurposeLink {
				token = "initiating"
			}
			user, session, password := "", "", ""
			if purpose == OIDCPurposeLink {
				user, session, password = "user-1", "session", oidcTestPassword
			}
			start := f.start(purpose, user, session, token, password)
			got := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), token)
			base := "/login"
			if purpose == OIDCPurposeLink {
				base = "/account"
			}
			if got.Destination != base+"#oidcError=unavailable" || got.SessionToken != "" {
				t.Fatalf("%+v", got)
			}
		})
	}
	t.Run("random failure", func(t *testing.T) {
		f := newOIDCFixture(t)
		addIdentity(f.repo, "user-1", "subject-1")
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		f.service.random = func([]byte) error { return errors.New("random unavailable") }
		got := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if got.Destination != "/login#oidcError=unavailable" || got.SessionToken != "" || f.repo.created != 0 {
			t.Fatalf("result=%+v created=%d", got, f.repo.created)
		}
	})
	t.Run("login persistence error", func(t *testing.T) {
		f := newOIDCFixture(t)
		addIdentity(f.repo, "user-1", "subject-1")
		f.repo.createErr = errors.New("db")
		start := f.start(OIDCPurposeLogin, "", "", "", "")
		got := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "")
		if got.Destination != "/login#oidcError=unavailable" || got.SessionToken != "" || f.repo.created != 0 {
			t.Fatalf("%+v", got)
		}
	})
}

func TestOIDCCallbackValidationPrecedesConsumptionAndCancelPrecedesCode(t *testing.T) {
	f := newOIDCFixture(t)
	start := f.start(OIDCPurposeLogin, "", "", "", "")
	params := url.Values{"state": {startState(t, start)}, "error": {strings.Repeat("e", 129)}}
	got := invoke(t, f.service, start, params, " ")
	if got.Destination != "/login#oidcError=invalid_flow" || len(f.repo.flows) != 1 {
		t.Fatalf("%+v flows=%d", got, len(f.repo.flows))
	}
	params = url.Values{"state": {startState(t, start)}, "error": {"cancelled"}}
	got = invoke(t, f.service, start, params, "")
	if got.Destination != "/login#oidcError=cancelled" || len(f.repo.flows) != 0 || f.provider.calls != 0 {
		t.Fatalf("%+v", got)
	}
}
