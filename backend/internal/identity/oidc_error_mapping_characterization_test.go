package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOIDCCallbackLinkDifferentSessionIDRejectedBeforeExchange(t *testing.T) {
	f := newOIDCFixture(t)
	start := f.start(OIDCPurposeLink, "user-1", "original-id", "initiating", oidcTestPassword)
	f.repo.sessions[string(digest([]byte("current")))] = SessionInfo{ID: "different-id", Principal: Principal{UserID: "user-1"}, ExpiresAt: f.clock.Add(time.Hour)}
	got := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "current")
	if got.Destination != "/account#oidcError=session_changed" || f.provider.calls != 0 {
		t.Fatalf("result=%+v exchange calls=%d", got, f.provider.calls)
	}
}

func TestOIDCCallbackLinkSessionChangedPersistenceMapsExactly(t *testing.T) {
	f := newOIDCFixture(t)
	start := f.start(OIDCPurposeLink, "user-1", "session-1", "initiating", oidcTestPassword)
	f.repo.linkErr = ErrOIDCSessionChanged
	got := invoke(t, f.service, start, callbackParams(startState(t, start), "code"), "initiating")
	if got.Destination != "/account#oidcError=session_changed" || got.Code != OIDCSessionChanged || got.SessionToken != "" {
		t.Fatalf("result=%+v", got)
	}
}

func TestOIDCCallbackOversizedErrorParamIsInvalidBeforeConsume(t *testing.T) {
	f := newOIDCFixture(t)
	start := f.start(OIDCPurposeLogin, "", "", "", "")
	params := callbackParams(startState(t, start), "code")
	params.Set("error", strings.Repeat("e", 129))
	got, err := f.service.Callback(context.Background(), params, start.BrowserToken, "")
	if err != nil || got.Code != OIDCInvalidFlow || got.Destination != "/login#oidcError=invalid_flow" || got.SessionToken != "" || len(f.repo.flows) != 1 || f.provider.calls != 0 {
		t.Fatalf("result=%+v err=%v flows=%d exchanges=%d", got, err, len(f.repo.flows), f.provider.calls)
	}
}

func TestOIDCStartCredentialOutageStillRunsDummyVerification(t *testing.T) {
	f := newOIDCFixture(t)
	f.repo.credentialErr = errors.New("database outage")
	calls := 0
	f.service.verifyPassword = func(hash, _ string) error {
		calls++
		if hash != f.service.dummyPasswordHash {
			t.Errorf("hash was not dummy")
		}
		return ErrPasswordWorkLimit
	}
	_, err := f.service.Start(context.Background(), OIDCStartParams{Purpose: OIDCPurposeLink, Principal: Principal{UserID: "user-1"}, Session: SessionInfo{ID: "sid", Principal: Principal{UserID: "user-1"}}, CurrentPassword: oidcTestPassword})
	if !errors.Is(err, ErrPasswordWorkLimit) || calls != 1 {
		t.Fatalf("err=%v verify calls=%d", err, calls)
	}
}
