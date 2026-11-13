package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

type passwordResetFakeService struct {
	issued identity.PasswordResetIssued
	login  string
	err    error
	calls  int
	last   string
}

func (f *passwordResetFakeService) IssuePasswordReset(context.Context, string, string, string) (identity.PasswordResetIssued, error) {
	f.calls++
	return f.issued, f.err
}
func (f *passwordResetFakeService) RevokePasswordReset(context.Context, string, string, string) error {
	f.calls++
	return f.err
}
func (f *passwordResetFakeService) RedeemPasswordReset(_ context.Context, token, password string) (string, error) {
	f.calls++
	f.last = token + ":" + password
	if f.err != nil {
		return "", f.err
	}
	if len(token) != 43 {
		return "", identity.ErrInvalidPasswordReset
	}
	if len([]rune(password)) < 15 {
		return "", &identity.ValidationError{Field: "password", Code: "invalid_length"}
	}
	return f.login, nil
}

func passwordResetRouter(service passwordResetService, valid bool) *chi.Mux {
	h := NewPasswordReset(service, func(next http.Handler) http.Handler { return next }, func(context.Context) (string, bool) {
		return "actor", valid
	})
	r := chi.NewRouter()
	h.Register(r)
	return r
}

func assertPasswordResetProblem(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	if body := bodyMap(t, w); body["status"] != float64(status) || body["code"] != code {
		t.Fatalf("body=%v", body)
	}
}

func TestPasswordResetIssueAndRevokeHTTPContract(t *testing.T) {
	expires := time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)
	created := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	token := strings.Repeat("A", 43)
	service := &passwordResetFakeService{issued: identity.PasswordResetIssued{Token: token, CreatedAt: created, ExpiresAt: expires}}
	router := passwordResetRouter(service, true)
	path := "/api/v1/households/house/accounts/target/password-reset"
	w := req(router, http.MethodPost, path, `{}`, "application/json", nil)
	if w.Code != http.StatusCreated || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	body := bodyMap(t, w)
	if body["token"] != token || body["createdAt"] != created.Format(time.RFC3339) || body["expiresAt"] != expires.Format(time.RFC3339) {
		t.Fatalf("body=%v", body)
	}
	if strings.Contains(w.Body.String(), "password") {
		t.Fatalf("response echoed a password field: %s", w.Body.String())
	}
	w = req(router, http.MethodDelete, path, "", "", nil)
	if w.Code != http.StatusNoContent || w.Header().Get("Cache-Control") != "no-store" || w.Body.Len() != 0 {
		t.Fatalf("delete status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}

	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"admin required", household.ErrAdminRequired, 403, "admin_required"},
		{"not found", household.ErrNotFound, 404, "not_found"},
		{"identity not found", identity.ErrNotFound, 404, "not_found"},
		{"self", identity.ErrCannotResetSelf, 409, "cannot_reset_self"},
		{"not permitted", identity.ErrPasswordResetNotPermitted, 409, "password_reset_not_permitted"},
		{"no local password", identity.ErrNoLocalPassword, 409, "no_local_password"},
		{"work limit", security.ErrPasswordWorkLimit, 429, "rate_limited"},
		{"default", errors.New("boom"), 503, "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := passwordResetRouter(&passwordResetFakeService{err: test.err}, true)
			for _, method := range []string{http.MethodPost, http.MethodDelete} {
				w := req(router, method, path, bodyFor(method), bodyForMedia(method), nil)
				assertPasswordResetProblem(t, w, test.status, test.code)
			}
		})
	}
}

func bodyFor(method string) string {
	if method == http.MethodPost {
		return `{}`
	}
	return ""
}

func bodyForMedia(method string) string {
	if method == http.MethodPost {
		return "application/json"
	}
	return ""
}

func TestPasswordResetIssueStrictBodyAndLimiter(t *testing.T) {
	router := passwordResetRouter(&passwordResetFakeService{issued: identity.PasswordResetIssued{Token: strings.Repeat("A", 43)}}, true)
	w := req(router, http.MethodPost, "/api/v1/households/house/accounts/target/password-reset", `{"extra":1}`, "application/json", nil)
	assertPasswordResetProblem(t, w, 400, "invalid_request")
	w = req(router, http.MethodPost, "/api/v1/households/house/accounts/target/password-reset", `{}`, "text/plain", nil)
	assertPasswordResetProblem(t, w, 415, "unsupported_media_type")

	service := &passwordResetFakeService{issued: identity.PasswordResetIssued{Token: strings.Repeat("A", 43)}}
	router = passwordResetRouter(service, true)
	for n := 0; n < 10; n++ {
		w = req(router, http.MethodPost, fmt.Sprintf("/api/v1/households/house/accounts/target-%d/password-reset", n), `{}`, "application/json", nil)
		if w.Code != http.StatusCreated {
			t.Fatalf("attempt %d status=%d", n, w.Code)
		}
	}
	w = req(router, http.MethodPost, "/api/v1/households/house/accounts/target/password-reset", `{}`, "application/json", nil)
	assertPasswordResetProblem(t, w, 429, "rate_limited")
	if w.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After=%q", w.Header().Get("Retry-After"))
	}
	if service.calls != 10 {
		t.Fatalf("limiter admitted calls=%d", service.calls)
	}
}

func TestPasswordResetIssueTargetThrottle(t *testing.T) {
	service := &passwordResetFakeService{issued: identity.PasswordResetIssued{Token: strings.Repeat("A", 43)}}
	router := passwordResetRouter(service, true)
	path := "/api/v1/households/house/accounts/target/password-reset"
	for n := 0; n < passwordResetTargetLimit; n++ {
		if w := req(router, http.MethodPost, path, `{}`, "application/json", nil); w.Code != http.StatusCreated {
			t.Fatalf("issue %d status=%d", n, w.Code)
		}
	}
	w := req(router, http.MethodPost, path, `{}`, "application/json", nil)
	assertPasswordResetProblem(t, w, 429, "rate_limited")
	if w.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After=%q", w.Header().Get("Retry-After"))
	}
	if service.calls != passwordResetTargetLimit {
		t.Fatalf("per-target limiter admitted calls=%d", service.calls)
	}
	// The bucket belongs to the target, not to the client address: a different
	// member can still be reset, and cancelling is never target-throttled.
	if w = req(router, http.MethodPost, "/api/v1/households/house/accounts/other/password-reset", `{}`, "application/json", nil); w.Code != http.StatusCreated {
		t.Fatalf("other target status=%d body=%s", w.Code, w.Body.String())
	}
	if w = req(router, http.MethodDelete, path, "", "", nil); w.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPasswordResetRedeemHTTPContract(t *testing.T) {
	service := &passwordResetFakeService{login: "bob"}
	router := passwordResetRouter(service, true)
	password := "a brand new long password"
	w := req(router, http.MethodPost, "/api/v1/auth/password-reset", `{"token":"`+strings.Repeat("A", 43)+`","password":"`+password+`"}`, "application/json", nil)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	if body := bodyMap(t, w); body["login"] != "bob" || len(body) != 1 {
		t.Fatalf("body=%v", body)
	}
	if strings.Contains(w.Body.String(), password) {
		t.Fatalf("response echoed the password: %s", w.Body.String())
	}
	if service.last != strings.Repeat("A", 43)+":"+password {
		t.Fatalf("service saw %q", service.last)
	}

	w = req(router, http.MethodPost, "/api/v1/auth/password-reset", `{"token":"short","password":"`+password+`"}`, "application/json", nil)
	assertPasswordResetProblem(t, w, 404, "invalid_password_reset")
	w = req(router, http.MethodPost, "/api/v1/auth/password-reset", `{"token":"`+strings.Repeat("A", 43)+`","password":"short"}`, "application/json", nil)
	assertPasswordResetProblem(t, w, 422, "invalid_length")
	w = req(router, http.MethodPost, "/api/v1/auth/password-reset", `{"token":"`+strings.Repeat("A", 43)+`","password":"short","extra":1}`, "application/json", nil)
	assertPasswordResetProblem(t, w, 400, "invalid_request")

	for _, test := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid", identity.ErrInvalidPasswordReset, 404, "invalid_password_reset"},
		{"work limit", security.ErrPasswordWorkLimit, 429, "rate_limited"},
		{"default", errors.New("boom"), 503, "unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := passwordResetRouter(&passwordResetFakeService{err: test.err}, true)
			w := req(router, http.MethodPost, "/api/v1/auth/password-reset", `{"token":"`+strings.Repeat("A", 43)+`","password":"`+password+`"}`, "application/json", nil)
			assertPasswordResetProblem(t, w, test.status, test.code)
		})
	}
	router = passwordResetRouter(&passwordResetFakeService{err: &identity.ValidationError{Field: "password", Code: "invalid_length"}}, true)
	w = req(router, http.MethodPost, "/api/v1/auth/password-reset", `{"token":"`+strings.Repeat("A", 43)+`","password":"short"}`, "application/json", nil)
	assertPasswordResetProblem(t, w, 422, "invalid_length")

	service = &passwordResetFakeService{login: "bob"}
	router = passwordResetRouter(service, true)
	for n := 0; n < 5; n++ {
		w = req(router, http.MethodPost, "/api/v1/auth/password-reset", `{"token":"`+strings.Repeat("A", 43)+`","password":"`+password+`"}`, "application/json", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d", n, w.Code)
		}
	}
	w = req(router, http.MethodPost, "/api/v1/auth/password-reset", `{"token":"`+strings.Repeat("A", 43)+`","password":"`+password+`"}`, "application/json", nil)
	assertPasswordResetProblem(t, w, 429, "rate_limited")
	if service.calls != 5 {
		t.Fatalf("limiter admitted calls=%d", service.calls)
	}
}

func TestPasswordResetProtectedRoutesRequireSession(t *testing.T) {
	service := &passwordResetFakeService{}
	router := passwordResetRouter(service, false)
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		w := req(router, method, "/api/v1/households/house/accounts/target/password-reset", bodyFor(method), bodyForMedia(method), nil)
		assertPasswordResetProblem(t, w, 401, "unauthenticated")
	}
	if service.calls != 0 {
		t.Fatalf("unauthenticated calls reached service: %d", service.calls)
	}
}

func TestPasswordResetRedeemRejectsForeignOrigin(t *testing.T) {
	service := &passwordResetFakeService{login: "bob"}
	app := httpx.NewAppWithRoutes(nil, time.Second, nil, httpx.OriginPolicy{PublicURL: "https://tendo.example"}, func(r chi.Router) {
		NewPasswordReset(service, func(next http.Handler) http.Handler { return next }, func(context.Context) (string, bool) { return "actor", true }).Register(r)
	})
	body := `{"token":"` + strings.Repeat("A", 43) + `","password":"a brand new long password"}`
	request := httptest.NewRequest(http.MethodPost, "https://tendo.example/api/v1/auth/password-reset", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	response := httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || service.calls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "https://tendo.example/api/v1/auth/password-reset", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://tendo.example")
	response = httptest.NewRecorder()
	app.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.calls != 1 {
		t.Fatalf("same-origin status=%d calls=%d body=%s", response.Code, service.calls, response.Body.String())
	}
}
