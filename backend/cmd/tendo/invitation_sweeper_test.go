package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
)

// TestInvitationSweeperRunsAtStartupAndStops pins the lifecycle shared with the
// MCP sweeper: a run at startup, repeats on the interval, and a stop that waits
// for the goroutine.
func TestInvitationSweeperRunsAtStartupAndStops(t *testing.T) {
	started := make(chan struct{}, 8)
	s := invitationSweeper{
		sweep: func(context.Context) (identityapp.InvitationSweepReport, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			return identityapp.InvitationSweepReport{}, nil
		},
		interval: 5 * time.Millisecond, timeout: time.Second,
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	}
	stop := startSweeper(context.Background(), s.run)
	for _, phase := range []string{"startup", "interval"} {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("invitation sweep did not run at %s", phase)
		}
	}
	if !stop(time.Second) {
		t.Fatal("invitation sweeper did not exit within the stop timeout")
	}
}

// TestInvitationSweeperLogsCountAndFixedFaultClassOnly pins that logs carry the
// per-step counts and a fixed class, never driver text or token material.
func TestInvitationSweeperLogsCountAndFixedFaultClassOnly(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	for _, tc := range []struct {
		err   error
		class string
	}{
		{errors.New("pq: password authentication failed for secret-user"), "class=unknown"},
		{identityapp.ErrInvitationSweepFailed, "class=step_failed"},
		{context.DeadlineExceeded, "class=timeout"},
		{context.Canceled, "class=canceled"},
	} {
		buf.Reset()
		invitationSweeper{
			sweep: func(context.Context) (identityapp.InvitationSweepReport, error) {
				return identityapp.InvitationSweepReport{Swept: map[identityapp.InvitationSweepStep]int{identityapp.InvitationSweepExpiredDigests: 2}}, tc.err
			},
			timeout: time.Second, logger: logger,
		}.once(context.Background())
		out := buf.String()
		if strings.Contains(out, "secret-user") || strings.Contains(out, "password") || !strings.Contains(out, tc.class) || !strings.Contains(out, "expired_digests_cleared=2") {
			t.Fatalf("failure log for %v: %s", tc.err, out)
		}
	}
	buf.Reset()
	invitationSweeper{
		sweep: func(context.Context) (identityapp.InvitationSweepReport, error) {
			return identityapp.InvitationSweepReport{
				Swept:  map[identityapp.InvitationSweepStep]int{identityapp.InvitationSweepExpiredDigests: 5, identityapp.InvitationSweepOIDCFlowsExpired: 4, identityapp.InvitationSweepOIDCPendingLoginsExpired: 3},
				Locked: []identityapp.InvitationSweepStep{identityapp.InvitationSweepOIDCFlowsExpired},
			}, nil
		},
		timeout: time.Second, logger: logger,
	}.once(context.Background())
	if out := buf.String(); !strings.Contains(out, "expired_digests_cleared=5") || !strings.Contains(out, "oidc_flows_expired=4") || !strings.Contains(out, "oidc_pending_logins_expired=3") || !strings.Contains(out, "locked_steps=1") {
		t.Fatalf("success log: %s", out)
	}
}

// TestStopAllCancelsTogetherWithinOneTimeout pins that the combined stop calls
// every stop at once, so a slow sweeper cannot delay another one's cancellation
// and the total wait stays within one timeout, and that it reports false when
// any sweeper misses its timeout.
func TestStopAllCancelsTogetherWithinOneTimeout(t *testing.T) {
	// Each stop waits, bounded, until the other one has been called too. A
	// sequential stopAll would make the first stop give up, which fails below
	// instead of hanging the test.
	var calls atomic.Int32
	bothCalled := make(chan struct{})
	var together atomic.Bool
	together.Store(true)
	barrier := func() {
		if calls.Add(1) == 2 {
			close(bothCalled)
		}
		select {
		case <-bothCalled:
		case <-time.After(2 * time.Second):
			together.Store(false)
		}
	}
	slow := func(timeout time.Duration) bool {
		barrier()
		time.Sleep(timeout)
		return false
	}
	fast := func(time.Duration) bool {
		barrier()
		return true
	}
	started := time.Now()
	if stopAll(slow, fast)(50 * time.Millisecond) {
		t.Fatal("stopAll reported success although one sweeper missed its timeout")
	}
	if !together.Load() {
		t.Fatal("stopAll did not call the stops concurrently")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stopAll waited %v; the stops did not run together", elapsed)
	}
	done := func(time.Duration) bool { return true }
	if !stopAll(done, done)(time.Second) {
		t.Fatal("stopAll reported failure although every sweeper stopped")
	}
}
