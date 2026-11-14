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
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

const rotatedToken = "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"

type fakePasswordChangeService struct {
	calls int
	got   identity.ChangePasswordInput
	reply identity.Session
	err   error
}

func (f *fakePasswordChangeService) ChangePassword(_ context.Context, in identity.ChangePasswordInput) (identity.Session, error) {
	f.calls++
	f.got = in
	if f.err != nil {
		return identity.Session{}, f.err
	}
	return f.reply, nil
}

// passwordChangeFixture composes the real session authentication middleware
// over a stub change service, so every request proves the cookie is required
// before the flow runs.
func passwordChangeFixture(method identity.SignInMethod) (*fakePasswordChangeService, *SessionHandler, *PasswordChangeHandler, http.Handler) {
	expires := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	principal := identity.Principal{UserID: "u-1", Login: "owner_1", DefaultHouseholdID: "h-1"}
	sessions := NewSession(&fakeSessionService{expires: expires, principal: principal, signInMethod: method}, "http://localhost")
	change := &fakePasswordChangeService{reply: identity.Session{
		Token: rotatedToken, ID: "sid-2", Principal: principal, ExpiresAt: expires, SignInMethod: identity.SignInPassword,
	}}
	handler := NewPasswordChange(change, sessions)
	r := chi.NewRouter()
	handler.Register(r)
	return change, sessions, handler, r
}

const validPasswordChangeBody = `{"currentPassword":"the old local password","newPassword":"a brand new local password"}`

func passwordChangeRequest(body, token, ip string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "tendo_session", Value: token})
	}
	return httpx.WithRequestMetadata(r, httpx.RequestMetadata{ClientIP: ip})
}

func TestPasswordChangeRequiresOneSessionCookie(t *testing.T) {
	change, _, _, mux := passwordChangeFixture(identity.SignInPassword)
	for name, r := range map[string]*http.Request{
		"no cookie":        passwordChangeRequest(validPasswordChangeBody, "", "192.0.2.10"),
		"duplicate cookie": passwordChangeRequest(validPasswordChangeBody, fakeToken, "192.0.2.11"),
	} {
		if name == "duplicate cookie" {
			r.AddCookie(&http.Cookie{Name: "tendo_session", Value: fakeToken})
		}
		w := serve(mux, r)
		if w.Code != 401 || !strings.Contains(w.Body.String(), "unauthenticated") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: status=%d body=%s headers=%v", name, w.Code, w.Body, w.Header())
		}
	}
	if change.calls != 0 {
		t.Fatalf("unauthenticated request reached the service: %d", change.calls)
	}
}

// An OIDC session is refused before the body is even parsed: a malformed media
// type must still answer the local-session refusal, not a decoding error.
func TestPasswordChangeRefusesOIDCSessionBeforeBodyWork(t *testing.T) {
	change, _, _, mux := passwordChangeFixture(identity.SignInOIDC)
	r := passwordChangeRequest(`{not valid json`, fakeToken, "192.0.2.12")
	r.Header.Set("Content-Type", "text/plain")
	w := serve(mux, r)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "local_session_required") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if change.calls != 0 {
		t.Fatalf("oidc session reached the service: %d", change.calls)
	}
}

func TestPasswordChangeRotatesTheCookieAndReportsTheSession(t *testing.T) {
	change, _, _, mux := passwordChangeFixture(identity.SignInPassword)
	w := serve(mux, passwordChangeRequest(validPasswordChangeBody, fakeToken, "192.0.2.13"))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value != rotatedToken || cookies[0].Name != "tendo_session" || cookies[0].MaxAge != 30*24*3600 {
		t.Fatalf("cookies=%v", cookies)
	}
	if strings.Contains(w.Body.String(), rotatedToken) {
		t.Fatal("rotated token leaked in the response body")
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["signInMethod"] != "password" || body["userId"] != "u-1" {
		t.Fatalf("body=%s err=%v", w.Body, err)
	}
	if change.got.Principal.UserID != "u-1" || change.got.SignInMethod != identity.SignInPassword || change.got.SessionToken != fakeToken {
		t.Fatalf("service input=%+v", change.got)
	}
	if change.got.CurrentPassword != "the old local password" || change.got.NewPassword != "a brand new local password" {
		t.Fatalf("service passwords=%+v", change.got)
	}
}

func TestPasswordChangeErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		status     int
		code       string
		field      string
		clears     bool
		retryAfter bool
	}{
		{name: "wrong current password", err: identity.ErrCurrentPasswordIncorrect, status: 422, code: "incorrect", field: "currentPassword"},
		{name: "weak new password", err: &identity.ValidationError{Field: "newPassword", Code: "invalid_length"}, status: 422, code: "invalid_length", field: "newPassword"},
		{name: "credential vanished", err: identity.ErrLocalSessionRequired, status: 403, code: "local_session_required"},
		{name: "own session vanished", err: identity.ErrUnauthenticated, status: 401, code: "unauthenticated", clears: true},
		{name: "password work limit", err: security.ErrPasswordWorkLimit, status: 429, code: "rate_limited", retryAfter: true},
		{name: "persistence", err: errors.New("boom"), status: 503, code: "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			change, _, _, mux := passwordChangeFixture(identity.SignInPassword)
			change.err = tc.err
			w := serve(mux, passwordChangeRequest(validPasswordChangeBody, fakeToken, "192.0.2.14"))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("headers=%v", w.Header())
			}
			if tc.retryAfter && w.Header().Get("Retry-After") != "60" {
				t.Fatalf("Retry-After=%q", w.Header().Get("Retry-After"))
			}
			if tc.field != "" && !strings.Contains(w.Body.String(), `"field":"`+tc.field+`"`) {
				t.Fatalf("body=%s", w.Body)
			}
			cookies := w.Result().Cookies()
			if tc.clears {
				if len(cookies) != 1 || cookies[0].Value != "" || cookies[0].MaxAge != -1 {
					t.Fatalf("stale cookie was not cleared: %v", cookies)
				}
			} else if len(cookies) != 0 {
				t.Fatalf("error response rewrote the cookie: %v", cookies)
			}
		})
	}
}

// The per-account budget is spent even when the current password is wrong:
// guessing must not be free.
func TestPasswordChangePerAccountLimit(t *testing.T) {
	change, _, _, mux := passwordChangeFixture(identity.SignInPassword)
	change.err = identity.ErrCurrentPasswordIncorrect
	for i := 0; i < passwordChangeUserLimit; i++ {
		if w := serve(mux, passwordChangeRequest(validPasswordChangeBody, fakeToken, "192.0.2."+string(rune('A'+i)))); w.Code != 422 {
			t.Fatalf("attempt %d status=%d body=%s", i, w.Code, w.Body)
		}
	}
	if w := serve(mux, passwordChangeRequest(validPasswordChangeBody, fakeToken, "192.0.2.99")); w.Code != 429 {
		t.Fatalf("over-budget status=%d body=%s", w.Code, w.Body)
	}
	if change.calls != passwordChangeUserLimit {
		t.Fatalf("service calls=%d", change.calls)
	}
}

func TestPasswordChangeHourlyLimit(t *testing.T) {
	change, _, handler, mux := passwordChangeFixture(identity.SignInPassword)
	change.err = identity.ErrCurrentPasswordIncorrect
	handler.userHourLimit = newWindowLimiter(2, time.Hour)
	for i := 0; i < 2; i++ {
		if w := serve(mux, passwordChangeRequest(validPasswordChangeBody, fakeToken, "192.0.2."+string(rune('A'+i)))); w.Code != 422 {
			t.Fatalf("attempt %d status=%d", i, w.Code)
		}
	}
	if w := serve(mux, passwordChangeRequest(validPasswordChangeBody, fakeToken, "192.0.2.80")); w.Code != 429 {
		t.Fatalf("hourly over-budget status=%d body=%s", w.Code, w.Body)
	}
}

// The per-address budget is spent before authentication, so a cookie-less
// flood cannot amplify into session lookups.
func TestPasswordChangePerAddressLimitPrecedesAuthentication(t *testing.T) {
	change, sessions, _, mux := passwordChangeFixture(identity.SignInPassword)
	for i := 0; i < passwordChangeClientLimit; i++ {
		if w := serve(mux, passwordChangeRequest(validPasswordChangeBody, "", "192.0.2.20")); w.Code != 401 {
			t.Fatalf("attempt %d status=%d", i, w.Code)
		}
	}
	if w := serve(mux, passwordChangeRequest(validPasswordChangeBody, fakeToken, "192.0.2.20")); w.Code != 429 {
		t.Fatalf("over-address-budget status=%d body=%s", w.Code, w.Body)
	}
	if change.calls != 0 || sessions.service.(*fakeSessionService).lookupCalls != 0 {
		t.Fatalf("service calls=%d lookups=%d", change.calls, sessions.service.(*fakeSessionService).lookupCalls)
	}
}
