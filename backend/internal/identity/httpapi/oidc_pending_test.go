package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
)

func pendingRequest(t *testing.T, mux http.Handler, method, path, body string, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	r := httptest.NewRequest(method, path, reader)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	r.RemoteAddr = "203.0.113.7:1234"
	r.Host = "tendo.test"
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestOIDCPendingReadContract(t *testing.T) {
	expires := time.Date(2026, 11, 10, 12, 30, 0, 0, time.UTC)
	fake := &oidcHTTPFake{pendingStatus: identity.OIDCPendingStatus{ExpiresAt: expires, EmailVerified: true, Invitation: true}}
	mux := oidcApp(fake)
	w := pendingRequest(t, mux, http.MethodGet, "/api/v1/auth/oidc/pending", "", nil, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") == "" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["expiresAt"] != "2026-11-10T12:30:00Z" || got["emailVerified"] != true || got["invitation"] != true {
		t.Fatalf("body=%v", got)
	}
}

func TestOIDCPendingReadMissingExpiredAndDuplicated(t *testing.T) {
	fake := &oidcHTTPFake{pendingErr: identity.ErrOIDCPendingNotFound}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	for _, tc := range []struct {
		name    string
		cookies []*http.Cookie
	}{
		{"absent", nil},
		{"duplicate treated as absent", []*http.Cookie{{Name: "tendo_oidc_pending", Value: fakeToken}, {Name: "tendo_oidc_pending", Value: fakeToken}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := pendingRequest(t, mux, http.MethodGet, "/api/v1/auth/oidc/pending", "", nil, tc.cookies...)
			if w.Code != 404 || !strings.Contains(w.Body.String(), "pending_not_found") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("cache=%q", w.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestOIDCPendingEndpointsRejectSignedInAndForeignOrigin(t *testing.T) {
	fake := &oidcHTTPFake{}
	sessions := &fakeSessionService{principal: identity.Principal{UserID: "user-1", Login: "owner"}, expires: time.Now().Add(time.Hour)}
	mux := oidcRouter(fake, oidcTestPublicURL, sessions)
	validSession := &http.Cookie{Name: "tendo_session", Value: fakeToken}
	for _, path := range []string{"/api/v1/auth/oidc/pending", "/api/v1/auth/oidc/pending/account", "/api/v1/auth/oidc/pending/link"} {
		method := http.MethodPost
		body := ""
		if path == "/api/v1/auth/oidc/pending" {
			method = http.MethodGet
		}
		if path == "/api/v1/auth/oidc/pending/link" {
			body = `{"login":"owner","password":"passphrase"}`
		}
		w := pendingRequest(t, mux, method, path, body, map[string]string{"Origin": oidcTestPublicURL}, validSession)
		if w.Code != 409 || !strings.Contains(w.Body.String(), "already_signed_in") {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body)
		}
	}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/auth/oidc/pending/account", ""},
		{http.MethodPost, "/api/v1/auth/oidc/pending/link", `{"login":"owner","password":"passphrase"}`},
		{http.MethodDelete, "/api/v1/auth/oidc/pending", ""},
	} {
		w := pendingRequest(t, mux, tc.method, tc.path, tc.body, map[string]string{"Origin": "http://evil.test"})
		if w.Code != 403 || !strings.Contains(w.Body.String(), "origin_not_allowed") {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.path, w.Code, w.Body)
		}
	}
}

func TestOIDCPendingAccountCreatesSessionAndClearsCookie(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	fake := &oidcHTTPFake{pendingAccount: identity.OIDCPendingAccountResult{
		Session:    identity.Session{Token: fakeToken, Principal: identity.Principal{UserID: "jit-1", Login: "member-abcdefgh"}, ExpiresAt: expires},
		Invitation: identity.OIDCInvitationAccepted,
	}}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	w := pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/pending/account", "", map[string]string{"Origin": oidcTestPublicURL}, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
	if w.Code != 201 || w.Header().Get("Location") != "/api/v1/session" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
	var body struct {
		Session struct {
			UserID string `json:"userId"`
			Login  string `json:"login"`
		} `json:"session"`
		Invitation string `json:"invitation"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Session.UserID != "jit-1" || body.Session.Login != "member-abcdefgh" || body.Invitation != "accepted" {
		t.Fatalf("body=%+v", body)
	}
	var pendingCleared, sessionSet bool
	for _, cookie := range w.Result().Cookies() {
		switch cookie.Name {
		case "tendo_oidc_pending":
			pendingCleared = cookie.Value == "" && cookie.MaxAge < 0
		case "tendo_session":
			sessionSet = cookie.Value == fakeToken
		}
	}
	if !pendingCleared || !sessionSet {
		t.Fatalf("cookies=%+v", w.Result().Cookies())
	}
}

func TestOIDCPendingLinkFailureIsUniform(t *testing.T) {
	fake := &oidcHTTPFake{pendingLinkErr: identity.ErrInvalidCredentials}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	w := pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/pending/link", `{"login":"owner","password":"wrong"}`, map[string]string{"Origin": oidcTestPublicURL}, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
	if w.Code != 401 || !strings.Contains(w.Body.String(), "invalid_credentials") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatalf("failure set cookies: %+v", w.Result().Cookies())
	}
}

func TestOIDCPendingLinkContract(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	fake := &oidcHTTPFake{pendingLink: identity.OIDCPendingLinkResult{
		Session:    identity.Session{Token: fakeToken, Principal: identity.Principal{UserID: "user-1", Login: "owner"}, ExpiresAt: expires},
		Invitation: identity.OIDCInvitationNone,
		EmailSaved: true,
	}}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	w := pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/pending/link", `{"login":"owner","password":"passphrase"}`, map[string]string{"Origin": oidcTestPublicURL}, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
	if w.Code != 201 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["emailSaved"] != true || body["invitation"] != "none" {
		t.Fatalf("body=%v", body)
	}
}

func TestOIDCPendingCancelAndDiscard(t *testing.T) {
	fake := &oidcHTTPFake{discardHandle: fakeToken, discardStatus: identity.OIDCPendingStatus{ExpiresAt: time.Now().Add(time.Minute), EmailVerified: true}}
	sessions := &fakeSessionService{principal: identity.Principal{UserID: "jit-user", Login: "member-abcdefgh"}, expires: time.Now().Add(time.Hour)}
	mux := oidcRouter(fake, oidcTestPublicURL, sessions)

	w := pendingRequest(t, mux, http.MethodDelete, "/api/v1/auth/oidc/pending", "", map[string]string{"Origin": oidcTestPublicURL}, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("cancel status=%d body=%s", w.Code, w.Body)
	}
	var cleared bool
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "tendo_oidc_pending" && cookie.Value == "" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("cancel cookies=%+v", w.Result().Cookies())
	}

	w = pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/account/discard", "", map[string]string{"Origin": oidcTestPublicURL}, &http.Cookie{Name: "tendo_session", Value: fakeToken})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"emailVerified":true`) {
		t.Fatalf("discard status=%d body=%s", w.Code, w.Body)
	}
	var sessionCleared, pendingSet bool
	for _, cookie := range w.Result().Cookies() {
		switch cookie.Name {
		case "tendo_session":
			sessionCleared = cookie.Value == "" && cookie.MaxAge < 0
		case "tendo_oidc_pending":
			pendingSet = cookie.Value == fakeToken && cookie.MaxAge == 600 && cookie.HttpOnly && cookie.SameSite == http.SameSiteLaxMode
		}
	}
	if !sessionCleared || !pendingSet {
		t.Fatalf("discard cookies=%+v", w.Result().Cookies())
	}
}

func TestOIDCPendingDiscardRefusalAndDisabled(t *testing.T) {
	fake := &oidcHTTPFake{discardErr: identity.ErrOIDCAccountNotDiscardable}
	sessions := &fakeSessionService{principal: identity.Principal{UserID: "user-1", Login: "owner"}, expires: time.Now().Add(time.Hour)}
	mux := oidcRouter(fake, oidcTestPublicURL, sessions)
	w := pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/account/discard", "", map[string]string{"Origin": oidcTestPublicURL}, &http.Cookie{Name: "tendo_session", Value: fakeToken})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "account_not_discardable") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	disabled := oidcRouter(nil, oidcTestPublicURL, &fakeSessionService{})
	w = pendingRequest(t, disabled, http.MethodPost, "/api/v1/auth/oidc/account/discard", "", map[string]string{"Origin": oidcTestPublicURL})
	if w.Code != 404 || !strings.Contains(w.Body.String(), "oidc_disabled") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	w = pendingRequest(t, disabled, http.MethodGet, "/api/v1/auth/oidc/pending", "", nil)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "oidc_disabled") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

func TestOIDCPendingRateLimit(t *testing.T) {
	fake := &oidcHTTPFake{pendingErr: identity.ErrOIDCPendingNotFound}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	status := 0
	for attempt := 0; attempt < 11; attempt++ {
		status = pendingRequest(t, mux, http.MethodGet, "/api/v1/auth/oidc/pending", "", nil).Code
	}
	if status != 429 {
		t.Fatalf("status after limit=%d", status)
	}
}

func TestOIDCStartInvitationTokenValidation(t *testing.T) {
	fake := &oidcHTTPFake{startErr: identity.ErrInvalidInvitation}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	w := pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/start", `{"purpose":"login","invitationToken":"short"}`, map[string]string{"Origin": oidcTestPublicURL})
	if w.Code != 422 || !strings.Contains(w.Body.String(), "invalid_invitation") || !strings.Contains(w.Body.String(), "invitationToken") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	fake.startErr = nil
	w = pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/start", `{"purpose":"login","invitationToken":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}`, map[string]string{"Origin": oidcTestPublicURL})
	if w.Code != 200 || fake.LastStart().InvitationToken == "" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

// oidcApp composes the OIDC handler behind the real request middleware so
// contract tests see X-Request-ID and origin enforcement as shipped.
func oidcApp(service oidcService) http.Handler {
	handler := NewOIDC(service, NewSession(&fakeSessionService{}, oidcTestPublicURL), oidcTestPublicURL)
	return httpx.NewAppWithLogger(nil, time.Second, nil, httpx.OriginPolicy{PublicURL: oidcTestPublicURL}, handler.Register, slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func TestOIDCPendingExhaustionClearsCookie(t *testing.T) {
	fake := &oidcHTTPFake{pendingLinkErr: identity.ErrOIDCPendingExhausted}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	w := pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/pending/link", `{"login":"owner","password":"wrong"}`, map[string]string{"Origin": oidcTestPublicURL}, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
	if w.Code != 401 || !strings.Contains(w.Body.String(), "invalid_credentials") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	cleared := false
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "tendo_oidc_pending" && cookie.Value == "" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("exhausted continuation did not clear the cookie: %+v", w.Result().Cookies())
	}
}

func TestOIDCPendingAccountBodyConventions(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	fake := &oidcHTTPFake{pendingAccount: identity.OIDCPendingAccountResult{
		Session: identity.Session{Token: fakeToken, Principal: identity.Principal{UserID: "jit-1", Login: "member-abcdefgh"}, ExpiresAt: expires},
	}}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	cookie := &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken}
	w := pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/pending/account", `{"unexpected":true}`, map[string]string{"Origin": oidcTestPublicURL}, cookie)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_request") {
		t.Fatalf("unknown field status=%d body=%s", w.Code, w.Body)
	}
	w = pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/pending/account", `{}`, map[string]string{"Origin": oidcTestPublicURL}, cookie)
	if w.Code != 201 {
		t.Fatalf("empty object status=%d body=%s", w.Code, w.Body)
	}
}

func TestOIDCPendingIgnoresStaleSessionCookie(t *testing.T) {
	fake := &oidcHTTPFake{pendingStatus: identity.OIDCPendingStatus{ExpiresAt: time.Now().Add(time.Minute)}}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	w := pendingRequest(t, mux, http.MethodGet, "/api/v1/auth/oidc/pending", "", nil,
		&http.Cookie{Name: "tendo_session", Value: "stale"}, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
	if w.Code != 200 {
		t.Fatalf("stale session blocked the continuation: status=%d body=%s", w.Code, w.Body)
	}
}

func TestOIDCPendingConflictClearsCookie(t *testing.T) {
	for _, tc := range []struct {
		name string
		fake *oidcHTTPFake
		path string
		body string
	}{
		{"account", &oidcHTTPFake{pendingAccountErr: identity.ErrOIDCConflict}, "/api/v1/auth/oidc/pending/account", ""},
		{"link", &oidcHTTPFake{pendingLinkErr: identity.ErrOIDCConflict}, "/api/v1/auth/oidc/pending/link", `{"login":"owner","password":"passphrase"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := oidcRouter(tc.fake, oidcTestPublicURL, &fakeSessionService{})
			w := pendingRequest(t, mux, http.MethodPost, tc.path, tc.body, map[string]string{"Origin": oidcTestPublicURL}, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
			if w.Code != 409 || !strings.Contains(w.Body.String(), "identity_conflict") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			cleared := false
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name == "tendo_oidc_pending" && cookie.Value == "" && cookie.MaxAge < 0 {
					cleared = true
				}
			}
			if !cleared {
				t.Fatalf("conflict did not clear the pending cookie: %+v", w.Result().Cookies())
			}
		})
	}
}

func TestOIDCPendingCancelAllowedWhileSignedIn(t *testing.T) {
	fake := &oidcHTTPFake{}
	sessions := &fakeSessionService{principal: identity.Principal{UserID: "user-1", Login: "owner"}, expires: time.Now().Add(time.Hour)}
	mux := oidcRouter(fake, oidcTestPublicURL, sessions)
	w := pendingRequest(t, mux, http.MethodDelete, "/api/v1/auth/oidc/pending", "", map[string]string{"Origin": oidcTestPublicURL},
		&http.Cookie{Name: "tendo_session", Value: fakeToken}, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
	if w.Code != 204 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
	cleared := false
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "tendo_oidc_pending" && cookie.Value == "" && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("cancel did not clear the pending cookie: %+v", w.Result().Cookies())
	}
}

func TestOIDCPendingReadAndWriteHaveSeparateBuckets(t *testing.T) {
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	fake := &oidcHTTPFake{
		pendingStatus: identity.OIDCPendingStatus{ExpiresAt: expires},
		pendingAccount: identity.OIDCPendingAccountResult{
			Session: identity.Session{Token: fakeToken, Principal: identity.Principal{UserID: "jit-1", Login: "member-abcdefgh"}, ExpiresAt: expires},
		},
	}
	mux := oidcRouter(fake, oidcTestPublicURL, &fakeSessionService{})
	for attempt := 0; attempt < 10; attempt++ {
		if w := pendingRequest(t, mux, http.MethodGet, "/api/v1/auth/oidc/pending", "", nil, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken}); w.Code != 200 {
			t.Fatalf("GET %d status=%d", attempt, w.Code)
		}
	}
	// The read bucket is exhausted now, but the write bucket is untouched.
	w := pendingRequest(t, mux, http.MethodPost, "/api/v1/auth/oidc/pending/account", "", map[string]string{"Origin": oidcTestPublicURL}, &http.Cookie{Name: "tendo_oidc_pending", Value: fakeToken})
	if w.Code != 201 {
		t.Fatalf("write after read budget status=%d body=%s", w.Code, w.Body)
	}
}

func TestOIDCPendingDisabledDoesNotConsumeLimiter(t *testing.T) {
	mux := oidcRouter(nil, oidcTestPublicURL, &fakeSessionService{})
	for attempt := 0; attempt < 12; attempt++ {
		w := pendingRequest(t, mux, http.MethodGet, "/api/v1/auth/oidc/pending", "", nil)
		if w.Code != 404 || !strings.Contains(w.Body.String(), "oidc_disabled") {
			t.Fatalf("attempt %d status=%d body=%s", attempt, w.Code, w.Body)
		}
	}
}
