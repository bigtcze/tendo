package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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
// cleared count and a fixed class, never driver text or token material.
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
				return identityapp.InvitationSweepReport{Cleared: 2}, tc.err
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
			return identityapp.InvitationSweepReport{Cleared: 5, Locked: true}, nil
		},
		timeout: time.Second, logger: logger,
	}.once(context.Background())
	if out := buf.String(); !strings.Contains(out, "expired_digests_cleared=5") || !strings.Contains(out, "locked=true") {
		t.Fatalf("success log: %s", out)
	}
}

// TestStopAllWaitsForEverySweeper pins that the combined stop calls each stop
// and reports false when any sweeper misses its timeout.
func TestStopAllWaitsForEverySweeper(t *testing.T) {
	var calls []int
	ok := func(id int, result bool) func(time.Duration) bool {
		return func(time.Duration) bool { calls = append(calls, id); return result }
	}
	if !stopAll(ok(1, true), ok(2, true))(time.Second) || len(calls) != 2 {
		t.Fatalf("all stopped: calls=%v", calls)
	}
	calls = nil
	if stopAll(ok(1, false), ok(2, true))(time.Second) || len(calls) != 2 {
		t.Fatalf("one missed: calls=%v", calls)
	}
}
