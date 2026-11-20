package main

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sweepOrder records the sweep's events in order, so a test can prove the
// sweep ran and had returned by the time serve returned. serve takes a concrete
// *pgxpool.Pool, so the pool close itself is not observable here; its position
// after the sweeper stop is fixed in serveWithSweeper and pinned by runtime_test.go.
type sweepOrder struct {
	mu     sync.Mutex
	events []string
}

func (o *sweepOrder) add(event string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
}

func (o *sweepOrder) snapshot() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.events...)
}

// TestServeStartsSweeperAndStopsItBeforeServeReturns drives the production serve
// path with an injected sweep. It must start the sweeper at startup (before the
// first interval), and on shutdown the sweep context must end and the sweep must
// return before serve returns.
func TestServeStartsSweeperAndStopsItBeforePoolClose(t *testing.T) {
	order := &sweepOrder{}
	started := make(chan struct{})
	var startOnce sync.Once
	sweep := func(ctx context.Context) (identityapp.MCPSweepReport, error) {
		startOnce.Do(func() { close(started) })
		order.add("sweep-start")
		<-ctx.Done()
		order.add("sweep-stopped")
		return identityapp.MCPSweepReport{}, ctx.Err()
	}

	pool, err := pgxpool.New(t.Context(), "postgres://tendo@127.0.0.1:1/tendo?connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		PublicURL:       "http://127.0.0.1",
		ListenAddr:      "127.0.0.1:0",
		DBTimeout:       time.Second,
		ShutdownTimeout: time.Second,
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	steps := startupSteps{
		listen:   func(string) (net.Listener, error) { return listener, nil },
		mcpSweep: sweep,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- steps.serve(ctx, cfg, pool) }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("production serve did not start the sweeper")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}

	events := order.snapshot()
	stoppedAt := indexOf(events, "sweep-stopped")
	if stoppedAt < 0 {
		t.Fatalf("sweeper was not stopped on shutdown: events=%v", events)
	}
	if indexOf(events, "sweep-start") < 0 || indexOf(events, "sweep-start") > stoppedAt {
		t.Fatalf("sweep did not start before it stopped: events=%v", events)
	}
	if stoppedAt != len(events)-1 {
		t.Fatalf("sweep stop was not the last event before serve returned: events=%v", events)
	}
}

func indexOf(events []string, want string) int {
	for i, e := range events {
		if e == want {
			return i
		}
	}
	return -1
}
