package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type testPinger struct {
	started chan struct{}
	release chan struct{}
	err     error
}

func (p *testPinger) Ping(ctx context.Context) error {
	if p.started != nil {
		select {
		case p.started <- struct{}{}:
		default:
		}
	}
	if p.release != nil {
		select {
		case <-p.release:
			return p.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return p.err
}

func TestReadiness(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pool     Pinger
		draining bool
		want     int
	}{
		{name: "nil", want: http.StatusServiceUnavailable},
		{name: "failure", pool: &testPinger{err: errors.New("private db detail")}, want: http.StatusServiceUnavailable},
		{name: "success", pool: &testPinger{}, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draining := make(chan struct{})
			if tc.draining {
				close(draining)
			}
			r := httptest.NewRequest(http.MethodGet, "/health/ready", nil)
			w := httptest.NewRecorder()
			NewHealth(tc.pool, time.Second, draining).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if tc.want == http.StatusServiceUnavailable {
				if w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("headers=%v", w.Header())
				}
				var problem Problem
				if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
					t.Fatal(err)
				}
				if problem.Type != "about:blank" || problem.Title != "Service Unavailable" || problem.Status != tc.want || strings.Contains(w.Body.String(), "private db detail") {
					t.Fatalf("problem=%+v", problem)
				}
			}
		})
	}
}

func TestReadinessDrainsInflightPing(t *testing.T) {
	p := &testPinger{started: make(chan struct{}, 1), release: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-p.release:
		default:
			close(p.release)
		}
	})
	draining := make(chan struct{})
	server := httptest.NewServer(NewHealth(p, time.Second, draining))
	t.Cleanup(server.Close)
	result := make(chan struct {
		response *http.Response
		err      error
	}, 1)
	client := &http.Client{Timeout: 2 * time.Second}
	go func() {
		resp, err := client.Get(server.URL + "/health/ready")
		result <- struct {
			response *http.Response
			err      error
		}{resp, err}
	}()
	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("readiness ping did not start")
	}
	close(draining)
	select {
	case got := <-result:
		if got.response != nil {
			got.response.Body.Close()
		}
		t.Fatalf("request completed before ping release: response=%v err=%v", got.response, got.err)
	case <-time.After(25 * time.Millisecond):
	}
	close(p.release)
	var got struct {
		response *http.Response
		err      error
	}
	select {
	case got = <-result:
	case <-time.After(time.Second):
		t.Fatal("readiness request did not finish")
	}
	if got.err != nil {
		t.Fatalf("readiness request: %v", got.err)
	}
	if got.response == nil {
		t.Fatal("missing readiness response")
	}
	defer got.response.Body.Close()
	if got.response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", got.response.StatusCode)
	}
}

func TestReadinessPingDeadline(t *testing.T) {
	p := &testPinger{release: make(chan struct{})}
	t.Cleanup(func() { close(p.release) })
	h := NewHealth(p, 20*time.Millisecond, make(chan struct{}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Service Unavailable") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}

func TestMiddlewareRequestIDAndSafeLogging(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := requestMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }), logger)
	for _, tc := range []struct {
		input, expected string
		preserve        bool
	}{{"caller-1", "caller-1", true}, {"bad id\nsecret", "", false}} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/health/ready?token=secret", nil)
		r.Header.Set("X-Request-ID", tc.input)
		h.ServeHTTP(w, r)
		id := w.Header().Get("X-Request-ID")
		if tc.preserve && id != tc.expected {
			t.Fatalf("id=%q", id)
		}
		if !tc.preserve && (!safeID.MatchString(id) || id == tc.input) {
			t.Fatalf("unsafe generated id=%q", id)
		}
	}
	if strings.Contains(logs.String(), "secret") || !strings.Contains(logs.String(), "unmatched") {
		t.Fatalf("unsafe/incomplete logs: %s", logs.String())
	}
}

func TestUnknownAndMethodErrorsUseProblem(t *testing.T) {
	h := NewHealth(&testPinger{}, time.Second, make(chan struct{}))
	for _, r := range []*http.Request{httptest.NewRequest(http.MethodGet, "/missing", nil), httptest.NewRequest(http.MethodPost, "/health/ready", nil)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("status=%d content-type=%q", w.Code, w.Header().Get("Content-Type"))
		}
		var problem Problem
		if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
			t.Fatal(err)
		}
		if problem.Status != w.Code || problem.Type != "about:blank" {
			t.Fatalf("problem=%+v", problem)
		}
	}
}

func TestUIFallbackRouting(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ui-index"))
	})
	register := func(r chi.Router) {
		sub := chi.NewRouter()
		sub.Get("/api/v1/known", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
		r.Mount("/", sub)
	}
	app := NewAppWithUI(&testPinger{}, time.Second, make(chan struct{}), OriginPolicy{PublicURL: "http://example.test"}, register, ui)
	serve := func(method, target, host string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, target, nil)
		r.Host = host
		r.Header.Set("Origin", "http://example.test")
		w := httptest.NewRecorder()
		app.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		method, target string
		want           int
	}{
		{"GET", "/api/v1/nope", 404}, {"GET", "/api", 404}, {"GET", "/health/nope", 404},
		{"POST", "/health/ready", 405}, {"POST", "/api/v1/known", 405},
	} {
		w := serve(tc.method, tc.target, "example.test")
		if w.Code != tc.want || w.Header().Get("Content-Type") != "application/problem+json" || strings.Contains(w.Body.String(), "ui-index") {
			t.Fatalf("%s %s status=%d ct=%q body=%s", tc.method, tc.target, w.Code, w.Header().Get("Content-Type"), w.Body)
		}
	}
	if w := serve("GET", "/api/v1/known", "example.test"); w.Code != http.StatusNoContent {
		t.Fatalf("api route status=%d", w.Code)
	}
	logs.Reset()
	for _, target := range []string{"/", "/some/route?token=supersecret"} {
		w := serve("GET", target, "example.test")
		if w.Code != http.StatusTeapot || w.Body.String() != "ui-index" {
			t.Fatalf("%s reached fallback? status=%d body=%s", target, w.Code, w.Body)
		}
	}
	out := logs.String()
	if strings.Contains(out, "supersecret") || strings.Contains(out, "some/route") || strings.Count(out, `"route":"webui"`) != 2 {
		t.Fatalf("unsafe or wrong route label: %s", out)
	}
	if w := serve("GET", "/", "evil.test"); w.Code != http.StatusMisdirectedRequest || strings.Contains(w.Body.String(), "ui-index") {
		t.Fatalf("origin not enforced on UI: status=%d body=%s", w.Code, w.Body)
	}
	if w := serve("GET", "/some/route", "evil.test"); w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("origin not enforced on deep UI route: %d", w.Code)
	}
}
