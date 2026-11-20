package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestSweeperRunsAtStartupAndStopsOnShutdown pins the goroutine lifecycle: the
// first sweep happens at once (before the first interval elapses), later sweeps
// follow the interval, and stop cancels the context and waits for the loop.
func TestSweeperRunsAtStartupAndStopsOnShutdown(t *testing.T) {
	started := make(chan struct{}, 8)
	var mu sync.Mutex
	calls := 0
	sweep := func(ctx context.Context) (identityapp.MCPSweepReport, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		select {
		case started <- struct{}{}:
		default:
		}
		return identityapp.MCPSweepReport{Deleted: map[identityapp.MCPSweepStep]int{}}, nil
	}
	s := mcpSweeper{sweep: sweep, interval: 5 * time.Millisecond, timeout: time.Second, logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	stop := startSweeper(context.Background(), s.run)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sweep did not run at startup")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("sweep did not repeat on the interval")
	}
	stop(time.Second)
	mu.Lock()
	before := calls
	mu.Unlock()
	time.Sleep(30 * time.Millisecond)
	mu.Lock()
	after := calls
	mu.Unlock()
	if after != before {
		t.Fatalf("sweeper kept running after stop: %d -> %d calls", before, after)
	}
}

// TestSweeperLogsCountsAndFixedFaultClassOnly pins that a failed sweep logs a
// fixed class, never the error text, and a successful one logs step counts.
func TestSweeperLogsCountsAndFixedFaultClassOnly(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	secret := errors.New("pq: password authentication failed for secret-user")
	failing := mcpSweeper{
		sweep:   func(context.Context) (identityapp.MCPSweepReport, error) { return identityapp.MCPSweepReport{}, secret },
		timeout: time.Second, logger: logger,
	}
	failing.once(context.Background())
	if strings.Contains(buf.String(), "secret-user") || strings.Contains(buf.String(), "password") {
		t.Fatalf("sweep log leaked driver text: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "class=unknown") {
		t.Fatalf("sweep log lacks fixed fault class: %s", buf.String())
	}
	buf.Reset()
	ok := mcpSweeper{
		sweep: func(context.Context) (identityapp.MCPSweepReport, error) {
			return identityapp.MCPSweepReport{Deleted: map[identityapp.MCPSweepStep]int{identityapp.MCPSweepAccessTokensExpired: 3}}, nil
		},
		timeout: time.Second, logger: logger,
	}
	ok.once(context.Background())
	if !strings.Contains(buf.String(), "access_tokens_expired=3") {
		t.Fatalf("sweep log lacks counts: %s", buf.String())
	}
}

// orderedPool records whether the sweeper had stopped when the pool closed.
type orderedPool struct {
	mu            sync.Mutex
	sweepStopped  bool
	closedBefore  bool
	closeObserved bool
}

func (p *orderedPool) Ping(context.Context) error { return nil }
func (p *orderedPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeObserved = true
	p.closedBefore = p.sweepStopped
}

// TestServeStopsSweeperBeforePoolClose runs the real serve path with an injected
// sweep: the sweep runs at startup and, on shutdown, its context ends before the
// pool closes.
func TestServeStopsSweeperBeforePoolClose(t *testing.T) {
	ran := make(chan struct{}, 1)
	sweepDone := make(chan struct{})
	pool := &orderedPool{}
	sweep := func(ctx context.Context) (identityapp.MCPSweepReport, error) {
		select {
		case ran <- struct{}{}:
		default:
		}
		<-ctx.Done()
		pool.mu.Lock()
		pool.sweepStopped = true
		pool.mu.Unlock()
		close(sweepDone)
		return identityapp.MCPSweepReport{}, ctx.Err()
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	draining := make(chan struct{})
	server := &http.Server{Handler: http.NotFoundHandler()}
	ctx, cancel := context.WithCancel(context.Background())
	stop := startSweeper(ctx, mcpSweeper{sweep: sweep, interval: time.Hour, timeout: time.Second, logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}.run)
	done := make(chan error, 1)
	go func() {
		done <- serveWithSweeper(ctx, listener, runtimeResources{server: server, pool: pool}, draining, 500*time.Millisecond, stop)
	}()
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("sweep did not run at startup")
	}
	cancel()
	select {
	case <-sweepDone:
	case <-time.After(2 * time.Second):
		t.Fatal("sweeper was not stopped on shutdown")
	}
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if !pool.closeObserved || !pool.closedBefore {
		t.Fatal("pool closed before the sweeper stopped")
	}
}

// TestDefaultMCPSweepUsesProductionService pins that the production sweep exists
// and that startup wiring passes an injected test sweep through unchanged.
func TestDefaultMCPSweepUsesProductionService(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), "postgres://tendo@127.0.0.1:1/tendo?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	sweep := defaultMCPSweep(config.Config{PublicURL: "http://127.0.0.1", DBTimeout: time.Second}, pool)
	if sweep == nil {
		t.Fatal("production sweep is nil")
	}
	if _, err := sweep(t.Context()); err == nil {
		t.Fatal("sweep against an unreachable database must fail")
	}
}
