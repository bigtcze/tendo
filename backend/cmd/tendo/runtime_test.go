package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

type httpResult struct {
	response *http.Response
	err      error
}

type blockingPool struct {
	pingStarted  chan struct{}
	pingRelease  chan struct{}
	closeStarted chan struct{}
	closeRelease chan struct{}
	pingOnce     sync.Once
}

func (p *blockingPool) Ping(ctx context.Context) error {
	p.pingOnce.Do(func() { close(p.pingStarted) })
	select {
	case <-p.pingRelease:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *blockingPool) Close() {
	if p.closeStarted != nil {
		close(p.closeStarted)
	}
	if p.closeRelease != nil {
		<-p.closeRelease
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitHTTP(t *testing.T, result <-chan httpResult, what string) httpResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return httpResult{}
	}
}

func startRequest(client *http.Client, url string) <-chan httpResult {
	result := make(chan httpResult, 1)
	go func() {
		response, err := client.Get(url)
		result <- httpResult{response: response, err: err}
	}()
	return result
}

func TestOIDCRuntimeCompositionDoesNotDiscoverProviderAtStartup(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		wantBody string
	}{
		{"disabled", false, `{"enabled":false}`},
		{"configured unreachable issuer", true, `{"displayName":"Unreachable IdP","enabled":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{PublicURL: "http://127.0.0.1"}
			if tc.enabled {
				cfg.OIDCIssuer = "http://127.0.0.1:1"
				cfg.OIDCClientID = "client"
				cfg.OIDCClientSecret = "secret"
				cfg.OIDCDisplayName = "Unreachable IdP"
			}
			gate := security.NewPasswordGate(2)
			var repo identityapp.OIDCRepository
			if tc.enabled {
				repo = &startupOIDCRepo{}
			}
			oidc, err := newOIDCService(cfg, repo, gate, time.Now)
			if err != nil {
				t.Fatalf("service composition failed: %v", err)
			}
			routes := chi.NewRouter()
			sessions := identityhttp.NewSession(&fakeOIDCSessionService{}, cfg.PublicURL)
			identityhttp.NewOIDC(oidc, sessions, cfg.PublicURL).Register(routes)
			response := httptest.NewRecorder()
			routes.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc", nil))
			if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != tc.wantBody {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}
}

type startupOIDCRepo struct{}

func (*startupOIDCRepo) InsertFlow(context.Context, identityapp.OIDCFlow) error { return nil }
func (*startupOIDCRepo) ConsumeFlow(context.Context, []byte, []byte, time.Time, string, string) (identityapp.OIDCFlow, error) {
	return identityapp.OIDCFlow{}, identityapp.ErrNotFound
}
func (*startupOIDCRepo) FindIdentity(context.Context, string, string) (string, error) {
	return "", identityapp.ErrNotFound
}
func (*startupOIDCRepo) FindSession(context.Context, []byte, time.Time) (identityapp.SessionInfo, error) {
	return identityapp.SessionInfo{}, identityapp.ErrNotFound
}
func (*startupOIDCRepo) FindCredential(context.Context, string) (string, error) {
	return "", identityapp.ErrNotFound
}
func (*startupOIDCRepo) CreateOIDCLoginSession(context.Context, identityapp.NewSession) (string, error) {
	return "", nil
}
func (*startupOIDCRepo) LinkAndCreateSession(context.Context, identityapp.OIDCFlow, identityapp.OIDCVerifiedIdentity, identityapp.NewSession, []byte) (string, error) {
	return "", nil
}
func (*startupOIDCRepo) Linked(context.Context, string, string) (bool, error) { return false, nil }

type fakeOIDCSessionService struct{}

func (*fakeOIDCSessionService) Login(context.Context, string, string) (identityapp.Session, error) {
	return identityapp.Session{}, nil
}
func (*fakeOIDCSessionService) Lookup(context.Context, string) (identityapp.SessionInfo, error) {
	return identityapp.SessionInfo{}, nil
}
func (*fakeOIDCSessionService) Logout(context.Context, string) error { return nil }

func TestRuntimeServerWriteBudgetCoversReadAndServiceBudgets(t *testing.T) {
	server := newRuntimeServer(":0", http.NotFoundHandler(), 5*time.Second)
	if server.ReadTimeout != 15*time.Second || server.WriteTimeout < server.ReadTimeout+5*time.Second+5*time.Second {
		t.Fatalf("read timeout=%s write timeout=%s", server.ReadTimeout, server.WriteTimeout)
	}
	server = newRuntimeServer(":0", http.NotFoundHandler(), 30*time.Second)
	if server.WriteTimeout < server.ReadTimeout+30*time.Second+5*time.Second {
		t.Fatalf("write timeout=%s does not cover service budget", server.WriteTimeout)
	}
}

func TestServeDrainsReadinessAndBoundsPoolClose(t *testing.T) {
	pool := &blockingPool{pingStarted: make(chan struct{}), pingRelease: make(chan struct{}), closeStarted: make(chan struct{}), closeRelease: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-pool.pingRelease:
		default:
			close(pool.pingRelease)
		}
		select {
		case <-pool.closeRelease:
		default:
			close(pool.closeRelease)
		}
	})
	draining := make(chan struct{})
	server := &http.Server{Handler: httpx.NewHealth(pool, time.Second, draining)}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	t.Cleanup(func() { server.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runDone := make(chan error, 1)
	go func() {
		runDone <- serve(ctx, listener, runtimeResources{server: server, pool: pool}, draining, 300*time.Millisecond)
	}()
	result := startRequest(&http.Client{Timeout: 2 * time.Second}, "http://"+listener.Addr().String()+"/health/ready")
	waitSignal(t, pool.pingStarted, "readiness ping")
	cancel()
	waitSignal(t, draining, "draining")
	select {
	case got := <-result:
		if got.response != nil {
			got.response.Body.Close()
		}
		t.Fatalf("in-flight response ended before ping release: response=%v err=%v", got.response, got.err)
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case <-pool.closeStarted:
		t.Fatal("pool closed before the in-flight readiness request completed")
	default:
	}
	close(pool.pingRelease)
	got := waitHTTP(t, result, "drained readiness response")
	if got.err != nil {
		t.Fatalf("readiness request: %v", got.err)
	}
	if got.response == nil {
		t.Fatal("missing readiness response")
	}
	if got.response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", got.response.StatusCode)
	}
	got.response.Body.Close()
	waitSignal(t, pool.closeStarted, "pool close")
	select {
	case err := <-runDone:
		if err == nil || !strings.Contains(err.Error(), "shutdown") {
			t.Fatalf("error=%v, want bounded shutdown failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown exceeded its termination budget")
	}
}

func TestServeWaitsForInFlightReadiness(t *testing.T) {
	pool := &blockingPool{pingStarted: make(chan struct{}), pingRelease: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-pool.pingRelease:
		default:
			close(pool.pingRelease)
		}
	})
	draining := make(chan struct{})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: httpx.NewHealth(pool, time.Second, draining)}
	t.Cleanup(func() { listener.Close() })
	t.Cleanup(func() { server.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runDone := make(chan error, 1)
	go func() {
		runDone <- serve(ctx, listener, runtimeResources{server: server, pool: pool}, draining, time.Second)
	}()
	result := startRequest(&http.Client{Timeout: 2 * time.Second}, "http://"+listener.Addr().String()+"/health/ready")
	waitSignal(t, pool.pingStarted, "readiness ping")
	cancel()
	waitSignal(t, draining, "draining")
	select {
	case err := <-runDone:
		t.Fatalf("shutdown did not await request: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	select {
	case <-pool.closeStarted:
		t.Fatal("pool closed before the in-flight readiness request completed")
	default:
	}
	close(pool.pingRelease)
	got := waitHTTP(t, result, "readiness response")
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
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish after request drained")
	}
}

func TestServeForciblyClosesServerWhenHandlerOutlivesShutdown(t *testing.T) {
	pool := &blockingPool{pingStarted: make(chan struct{}), pingRelease: make(chan struct{})}
	handlerStarted, handlerRelease := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-handlerRelease:
		default:
			close(handlerRelease)
		}
	})
	draining := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		<-handlerRelease
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{Handler: handler}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	t.Cleanup(func() { server.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runDone := make(chan error, 1)
	go func() {
		runDone <- serve(ctx, listener, runtimeResources{server: server, pool: pool}, draining, 80*time.Millisecond)
	}()
	result := startRequest(&http.Client{Timeout: 5 * time.Second}, "http://"+listener.Addr().String()+"/")
	waitSignal(t, handlerStarted, "blocked handler")
	cancel()
	select {
	case err := <-runDone:
		if err == nil || !strings.Contains(err.Error(), "shutdown") {
			t.Fatalf("error=%v, want bounded shutdown failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not enforce its deadline")
	}
	got := waitHTTP(t, result, "forcibly closed request")
	if got.response != nil {
		got.response.Body.Close()
	}
	if got.err == nil {
		t.Fatalf("request unexpectedly completed: response=%v", got.response)
	}
	close(handlerRelease)
}

type failingListener struct{ err error }

func (l failingListener) Accept() (net.Conn, error) { return nil, l.err }
func (l failingListener) Close() error              { return nil }
func (l failingListener) Addr() net.Addr            { return testAddr("failed-listener") }

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

func TestServeUnexpectedListenerFailureBoundsPoolClose(t *testing.T) {
	pool := &blockingPool{closeStarted: make(chan struct{}), closeRelease: make(chan struct{})}
	t.Cleanup(func() {
		select {
		case <-pool.closeRelease:
		default:
			close(pool.closeRelease)
		}
	})
	server := &http.Server{}
	runDone := make(chan error, 1)
	go func() {
		runDone <- serve(context.Background(), failingListener{err: errors.New("private listener failure")}, runtimeResources{server: server, pool: pool}, make(chan struct{}), 50*time.Millisecond)
	}()
	waitSignal(t, pool.closeStarted, "pool close after listener failure")
	select {
	case err := <-runDone:
		if err == nil || err.Error() != "HTTP server stopped unexpectedly" {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unexpected listener failure waited indefinitely for pool close")
	}
}

var _ net.Listener = failingListener{}
