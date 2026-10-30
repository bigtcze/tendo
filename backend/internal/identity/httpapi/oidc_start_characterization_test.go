package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

func newOIDCStartHTTPRequest(body, origin string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/oidc/start", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func oidcStartProblemCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code  string `json:"code"`
		Field string `json:"field"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem %q: %v", w.Body, err)
	}
	return body.Code + ":" + body.Field
}

func oidcFlowCookies(w *httptest.ResponseRecorder) []*http.Cookie {
	var got []*http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "tendo_oidc" {
			got = append(got, c)
		}
	}
	return got
}

func TestOIDCStartValidationAndPrecedenceCharacterization(t *testing.T) {
	for _, tc := range []struct {
		name, body, origin string
		service            oidcService
		wantStatus         int
		wantCode           string
	}{
		{"missing link password", `{"purpose":"link"}`, oidcTestPublicURL, &oidcHTTPFake{}, 422, "invalid_value:currentPassword"},
		{"missing password foreign origin", `{"purpose":"link"}`, "https://evil.test", &oidcHTTPFake{}, 422, "invalid_value:currentPassword"},
		{"missing password disabled", `{"purpose":"link"}`, "https://evil.test", nil, 422, "invalid_value:currentPassword"},
		{"unknown purpose disabled", `{"purpose":"wat"}`, "", nil, 422, "invalid_value:purpose"},
		{"nil before origin", `{"purpose":"login"}`, "https://evil.test", nil, 404, "oidc_disabled:"},
		{"origin before session", `{"purpose":"link","currentPassword":"pw"}`, "https://evil.test", &oidcHTTPFake{}, 403, "origin_not_allowed:"},
		{"link without session", `{"purpose":"link","currentPassword":"pw"}`, oidcTestPublicURL, &oidcHTTPFake{}, 401, "invalid_credentials:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, _ := tc.service.(*oidcHTTPFake)
			r := newOIDCStartHTTPRequest(tc.body, tc.origin)
			w := httptest.NewRecorder()
			h := NewOIDC(tc.service, NewSession(&fakeSessionService{}, oidcTestPublicURL), oidcTestPublicURL)
			h.start(w, r)
			if w.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.wantStatus == 422 {
				if got := oidcStartProblemCode(t, w); got != tc.wantCode {
					t.Fatalf("problem=%s", got)
				}
			} else if !strings.Contains(w.Body.String(), strings.Split(tc.wantCode, ":")[0]) {
				t.Fatalf("body=%s", w.Body)
			}
			if len(oidcFlowCookies(w)) != 0 {
				t.Fatalf("flow cookie set: %v", oidcFlowCookies(w))
			}
			if fake != nil && fake.lastStart != (identity.OIDCStartParams{}) {
				t.Fatalf("Start unexpectedly called: %+v", fake.lastStart)
			}
		})
	}
	t.Run("malformed before purpose", func(t *testing.T) {
		fake := &oidcHTTPFake{}
		r := newOIDCStartHTTPRequest(`{`, oidcTestPublicURL)
		w := httptest.NewRecorder()
		NewOIDC(fake, NewSession(&fakeSessionService{}, oidcTestPublicURL), oidcTestPublicURL).start(w, r)
		if w.Code != 400 || fake.lastStart != (identity.OIDCStartParams{}) {
			t.Fatalf("status=%d body=%s start=%+v", w.Code, w.Body, fake.lastStart)
		}
	})
	t.Run("rate limit before body", func(t *testing.T) {
		h := NewOIDC(&oidcHTTPFake{}, NewSession(&fakeSessionService{}, oidcTestPublicURL), oidcTestPublicURL)
		var w *httptest.ResponseRecorder
		for i := 0; i < 11; i++ {
			w = httptest.NewRecorder()
			h.start(w, newOIDCStartHTTPRequest(`{`, oidcTestPublicURL))
		}
		if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
			t.Fatalf("status=%d retry=%q body=%s", w.Code, w.Header().Get("Retry-After"), w.Body)
		}
	})
}

func TestOIDCStartSessionCharacterization(t *testing.T) {
	t.Run("invalid cookie authentication response", func(t *testing.T) {
		fake := &oidcHTTPFake{}
		sessions := &fakeSessionService{}
		r := newOIDCStartHTTPRequest(`{"purpose":"login"}`, oidcTestPublicURL)
		r.AddCookie(&http.Cookie{Name: "tendo_session", Value: "invalid"})
		w := httptest.NewRecorder()
		NewOIDC(fake, NewSession(sessions, oidcTestPublicURL), oidcTestPublicURL).start(w, r)
		cookies := w.Result().Cookies()
		if w.Code != 401 || !strings.Contains(w.Body.String(), "unauthenticated") || w.Header().Get("Cache-Control") != "no-store" || len(cookies) != 1 || cookies[0].Name != "tendo_session" || cookies[0].Value != "" || cookies[0].MaxAge >= 0 || fake.lastStart != (identity.OIDCStartParams{}) {
			t.Fatalf("status=%d headers=%v cookies=%+v body=%s start=%+v", w.Code, w.Header(), cookies, w.Body, fake.lastStart)
		}
	})
	t.Run("login passes valid session to Start", func(t *testing.T) {
		fake := &oidcHTTPFake{}
		sessions := &fakeSessionService{principal: identity.Principal{UserID: "user-1", Login: "owner"}, expires: time.Now().Add(time.Hour)}
		r := newOIDCStartHTTPRequest(`{"purpose":"login"}`, oidcTestPublicURL)
		r.AddCookie(&http.Cookie{Name: "tendo_session", Value: fakeToken})
		w := httptest.NewRecorder()
		NewOIDC(fake, NewSession(sessions, oidcTestPublicURL), oidcTestPublicURL).start(w, r)
		if fake.lastStart.Session.ID != "sid-1" || fake.lastStart.Principal.UserID != "user-1" || w.Code != 200 {
			t.Fatalf("status=%d start=%+v body=%s", w.Code, fake.lastStart, w.Body)
		}
	})
}

func TestOIDCStartServiceErrorMappingCharacterization(t *testing.T) {
	for _, tc := range []struct {
		name               string
		err                error
		status             int
		code, field, retry string
	}{{"invalid purpose", identity.ErrOIDCInvalidPurpose, 422, "invalid_value", "purpose", ""}, {"already signed in", identity.ErrOIDCAlreadySignedIn, 409, "already_signed_in", "", ""}, {"unauthenticated", identity.ErrUnauthenticated, 401, "invalid_credentials", "", ""}, {"invalid credentials", identity.ErrInvalidCredentials, 401, "invalid_credentials", "", ""}, {"work limit", security.ErrPasswordWorkLimit, 429, "rate_limited", "", "60"}, {"OIDC unavailable", identity.ErrOIDCUnavailable, 503, "oidc_unavailable", "", ""}, {"other", errors.New("internal"), 503, "unavailable", "", ""}} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &oidcHTTPFake{startErr: tc.err}
			w := httptest.NewRecorder()
			NewOIDC(fake, NewSession(&fakeSessionService{}, oidcTestPublicURL), oidcTestPublicURL).start(w, newOIDCStartHTTPRequest(`{"purpose":"login"}`, oidcTestPublicURL))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || w.Header().Get("Retry-After") != tc.retry || len(oidcFlowCookies(w)) != 0 || fake.lastStart.Purpose != identity.OIDCPurposeLogin {
				t.Fatalf("status=%d retry=%q cookies=%v body=%s start=%+v", w.Code, w.Header().Get("Retry-After"), oidcFlowCookies(w), w.Body, fake.lastStart)
			}
			if tc.status == 422 && oidcStartProblemCode(t, w) != "invalid_value:"+tc.field {
				t.Fatalf("problem=%s", oidcStartProblemCode(t, w))
			}
		})
	}
}

func TestOIDCStartFlowCookieAndSuccessCharacterization(t *testing.T) {
	t.Run("success cookie and body", func(t *testing.T) {
		fake := &oidcHTTPFake{start: identity.OIDCStartResult{AuthorizationURL: "https://issuer.test/auth", BrowserToken: fakeToken}}
		w := httptest.NewRecorder()
		NewOIDC(fake, NewSession(&fakeSessionService{}, oidcTestPublicURL), oidcTestPublicURL).start(w, newOIDCStartHTTPRequest(`{"purpose":"login"}`, oidcTestPublicURL))
		cookies := oidcFlowCookies(w)
		if w.Code != 200 || len(cookies) != 1 || cookies[0].Value != fakeToken || cookies[0].MaxAge != 600 || !strings.Contains(w.Body.String(), `"authorizationUrl":"https://issuer.test/auth"`) {
			t.Fatalf("status=%d cookies=%+v body=%s", w.Code, cookies, w.Body)
		}
	})
	for _, tc := range []struct {
		name   string
		values []string
		want   string
	}{{"single", []string{fakeToken}, fakeToken}, {"duplicate", []string{fakeToken, fakeToken}, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &oidcHTTPFake{}
			r := newOIDCStartHTTPRequest(`{"purpose":"login"}`, oidcTestPublicURL)
			for _, v := range tc.values {
				r.AddCookie(&http.Cookie{Name: "tendo_oidc", Value: v})
			}
			w := httptest.NewRecorder()
			NewOIDC(fake, NewSession(&fakeSessionService{}, oidcTestPublicURL), oidcTestPublicURL).start(w, r)
			if fake.lastStart.PreviousBrowserToken != tc.want || w.Code != 200 {
				t.Fatalf("status=%d previous=%q want=%q", w.Code, fake.lastStart.PreviousBrowserToken, tc.want)
			}
		})
	}
}

func TestOIDCStartCorruptPasswordHashIsUnauthorizedAndDiagnosed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		fault bool
	}{
		{"corrupt stored hash", fmt.Errorf("%w: %w", identity.ErrInvalidCredentials, security.ErrInvalidPasswordHash), true},
		{"password mismatch", identity.ErrInvalidCredentials, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &bytes.Buffer{}
			fake := &oidcHTTPFake{startErr: tc.err}
			h := NewOIDC(fake, NewSession(&fakeSessionService{}, oidcTestPublicURL), oidcTestPublicURL)
			app := httpx.NewAppWithLogger(nil, time.Second, nil, httpx.OriginPolicy{PublicURL: oidcTestPublicURL}, h.Register, slog.New(slog.NewJSONHandler(logs, nil)))
			r := newOIDCStartHTTPRequest(`{"purpose":"login"}`, oidcTestPublicURL)
			r.Host = "tendo.test"
			w := httptest.NewRecorder()
			app.ServeHTTP(w, r)
			if w.Code != 401 || oidcStartProblemCode(t, w) != "invalid_credentials:" {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if got := strings.Contains(logs.String(), `"fault":"password_hash_integrity_fault"`); got != tc.fault {
				t.Fatalf("fault logged=%v want %v: %s", got, tc.fault, logs.String())
			}
		})
	}
}
