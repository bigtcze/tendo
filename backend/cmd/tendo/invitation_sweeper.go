package main

import (
	"context"
	"log/slog"
	"time"

	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// invitationSweepInterval is the invitation retention cadence after the run at
// startup (ADR 0024).
const invitationSweepInterval = 10 * time.Minute

// invitationSweeper clears the token digests of expired invitations. It has the
// same lifecycle as mcpSweeper: once at startup, then every interval, each run
// bounded by timeout. Logs carry the count and fixed fault classes only.
type invitationSweeper struct {
	sweep    func(context.Context) (identityapp.InvitationSweepReport, error)
	interval time.Duration
	timeout  time.Duration
	logger   *slog.Logger
}

func (s invitationSweeper) run(ctx context.Context) {
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

func (s invitationSweeper) once(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	report, err := s.sweep(runCtx)
	attrs := make([]any, 0, 2*len(identityapp.InvitationSweepSteps)+4)
	for _, step := range identityapp.InvitationSweepSteps {
		attrs = append(attrs, string(step), report.Swept[step])
	}
	attrs = append(attrs, "locked_steps", len(report.Locked))
	if err != nil {
		s.logger.Warn("invitation retention sweep failed", append([]any{"class", sweepFaultClass(err)}, attrs...)...)
		return
	}
	s.logger.Info("invitation retention sweep", attrs...)
}

// defaultInvitationSweep runs the production invitation retention pass. It needs
// no household services: it only clears digests.
func defaultInvitationSweep(pool *pgxpool.Pool) func(context.Context) (identityapp.InvitationSweepReport, error) {
	store := identitypostgres.NewInvitationRepository(pool, nil)
	return func(ctx context.Context) (identityapp.InvitationSweepReport, error) {
		return identityapp.SweepInvitationDigests(ctx, store, time.Now())
	}
}

// stopAll combines sweeper stop functions. It calls them concurrently, so every
// sweeper is cancelled at once and all of them share the one timeout instead of
// waiting in turn. It reports whether every sweeper exited in time.
func stopAll(stops ...func(time.Duration) bool) func(time.Duration) bool {
	return func(timeout time.Duration) bool {
		results := make(chan bool, len(stops))
		for _, stop := range stops {
			go func() { results <- stop(timeout) }()
		}
		ok := true
		for range stops {
			if !<-results {
				ok = false
			}
		}
		return ok
	}
}
