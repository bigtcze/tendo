package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
)

// mcpSweepInterval is the retention cadence after the run at startup (ADR 0023).
const mcpSweepInterval = 10 * time.Minute

// mcpSweeper configures the retention goroutine. sweep runs one retention pass,
// bounded by timeout. Interval and sweep are injectable so tests need no clock
// or database. Logs carry counts and fixed fault classes only, never error
// text, which can contain driver detail.
type mcpSweeper struct {
	sweep    func(context.Context) (identityapp.MCPSweepReport, error)
	interval time.Duration
	timeout  time.Duration
	logger   *slog.Logger
}

// run sweeps once at start and then every interval until ctx ends.
func (s mcpSweeper) run(ctx context.Context) {
	for {
		s.once(ctx)
		timer := time.NewTimer(s.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s mcpSweeper) once(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	report, err := s.sweep(runCtx)
	if err != nil {
		s.logger.Warn("mcp retention sweep failed", "class", sweepFaultClass(err))
		return
	}
	attrs := make([]any, 0, 2*len(identityapp.MCPSweepSteps)+2)
	for _, step := range identityapp.MCPSweepSteps {
		attrs = append(attrs, string(step), report.Deleted[step])
	}
	s.logger.Info("mcp retention sweep", append(attrs, "locked_steps", len(report.Locked))...)
}

// sweepFaultClass maps a sweep failure to a fixed class.
func sweepFaultClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, identityapp.ErrMCPSweepFailed):
		return "step_failed"
	default:
		return "unknown"
	}
}

// startSweeper runs run in its own goroutine under a context derived from ctx.
// The returned stop cancels that context and waits for the goroutine, up to
// timeout, so the caller can close the pool after the sweep has left it. stop
// reports whether the goroutine exited within the timeout.
func startSweeper(ctx context.Context, run func(context.Context)) (stop func(timeout time.Duration) bool) {
	sweepCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(sweepCtx)
	}()
	return func(timeout time.Duration) bool {
		cancel()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-done:
			return true
		case <-timer.C:
			return false
		}
	}
}
