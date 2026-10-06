package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

type fakeService struct {
	required bool
	err      error
	calls    int
}

func (f *fakeService) Required(context.Context) (bool, error) { return f.required, nil }
func (f *fakeService) CreateOwner(context.Context, identity.SetupInput) error {
	f.calls++
	return f.err
}
func TestSetupHTTPContract(t *testing.T) {
	f := &fakeService{required: true}
	h := New(f, "a-secret-of-at-least-thirty-two-bytes")
	mux := chi.NewRouter()
	h.Register(mux)
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/auth/setup", nil))
	if get.Code != 200 || get.Header().Get("ETag") != "\"setup-v1-required\"" || get.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET: %d %v", get.Code, get.Header())
	}
	valid := `{"login":"owner_1","password":"a sufficiently long password","householdName":"Home","timezone":"UTC"}`
	request := func(token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPut, "/api/v1/auth/setup", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Tendo-Setup-Token", token)
		r.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		status int
		code   string
	}{
		{name: "wrong token", mutate: func(r *http.Request) { r.Header.Set("X-Tendo-Setup-Token", "wrong") }, status: 401, code: "unauthorized"},
		{name: "duplicate token", mutate: func(r *http.Request) { r.Header.Add("X-Tendo-Setup-Token", "a-secret-of-at-least-thirty-two-bytes") }, status: 401, code: "unauthorized"},
		{name: "wrong content type", mutate: func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, status: 415, code: "unsupported_media_type"},
		{name: "oversized body", mutate: func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", 8193))) }, status: 413, code: "content_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h.limiter.attempts = map[string][]time.Time{}
			r := httptest.NewRequest(http.MethodPut, "/api/v1/auth/setup", strings.NewReader(valid))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Tendo-Setup-Token", "a-secret-of-at-least-thirty-two-bytes")
			r.RemoteAddr = "192.0.2.1:1234"
			tc.mutate(r)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			assertSetupProblem(t, w, tc.status, tc.code)
			if f.calls != 0 {
				t.Fatalf("rejected request invoked service %d times", f.calls)
			}
		})
	}
	for _, body := range []string{`{"login":null,"password":"a sufficiently long password","householdName":"Home","timezone":"UTC"}`, `{"login":"a","login":"b","password":"a sufficiently long password","householdName":"Home","timezone":"UTC"}`, `{"Login":"a","password":"a sufficiently long password","householdName":"Home","timezone":"UTC"}`, `{"login":"a","password":"a sufficiently long password","householdName":"Home","timezone":null}`, `{"login":"a","password":"a sufficiently long password","householdName":"Home","timezone":"UTC","extra":1}`, `{"login":"a","password":"a sufficiently long password","householdName":"Home","timezone":"UTC"} {}`, `[]`} {
		h.limiter.attempts = map[string][]time.Time{}
		if w := request("a-secret-of-at-least-thirty-two-bytes", body); w.Code != 400 {
			t.Fatalf("invalid body accepted: %d %s", w.Code, w.Body)
		}
	}
	h.limiter.attempts = map[string][]time.Time{}
	r := httptest.NewRequest(http.MethodPut, "/api/v1/auth/setup", strings.NewReader(valid))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tendo-Setup-Token", "a-secret-of-at-least-thirty-two-bytes")
	r = httpx.WithRequestMetadata(r, httpx.RequestMetadata{ClientIP: "192.0.2.1"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 201 || w.Header().Get("Location") == "" {
		t.Fatalf("setup response %d %s", w.Code, w.Body)
	}
	f.err = identity.ErrComplete
	r = httptest.NewRequest(http.MethodPut, "/api/v1/auth/setup", strings.NewReader(valid))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tendo-Setup-Token", "a-secret-of-at-least-thirty-two-bytes")
	r = httpx.WithRequestMetadata(r, httpx.RequestMetadata{ClientIP: "192.0.2.1"})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 409 {
		t.Fatalf("repeat setup %d", w.Code)
	}
	f.err = errors.New("db error")
	r = httptest.NewRequest(http.MethodPut, "/api/v1/auth/setup", strings.NewReader(valid))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Tendo-Setup-Token", "a-secret-of-at-least-thirty-two-bytes")
	r = httpx.WithRequestMetadata(r, httpx.RequestMetadata{ClientIP: "192.0.2.1"})
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 503 || strings.Contains(w.Body.String(), "db error") {
		t.Fatalf("sanitized error %d %s", w.Code, w.Body)
	}
}
func assertSetupProblem(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
	var problem struct {
		Status int    `json:"status"`
		Type   string `json:"type"`
		Code   string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || problem.Status != status || problem.Type != "about:blank" || problem.Code != code {
		t.Fatalf("problem=%+v err=%v", problem, err)
	}
}

func TestSetupDisabled(t *testing.T) {
	h := New(&fakeService{}, "")
	mux := chi.NewRouter()
	h.Register(mux)
	r := httptest.NewRequest(http.MethodPut, "/api/v1/auth/setup", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatalf("got %d", w.Code)
	}
}
