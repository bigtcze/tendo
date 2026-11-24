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
	if err != nil {
		s.logger.Warn("invitation retention sweep failed", "class", sweepFaultClass(err), "expired_digests_cleared", report.Cleared)
		return
	}
	s.logger.Info("invitation retention sweep", "expired_digests_cleared", report.Cleared, "locked", report.Locked)
}

// defaultInvitationSweep runs the production invitation retention pass. It needs
// no household services: it only clears digests.
func defaultInvitationSweep(pool *pgxpool.Pool) func(context.Context) (identityapp.InvitationSweepReport, error) {
	store := identitypostgres.NewInvitationRepository(pool, nil)
	return func(ctx context.Context) (identityapp.InvitationSweepReport, error) {
		return identityapp.SweepInvitationDigests(ctx, store, time.Now())
	}
}

// stopAll combines sweeper stop functions. Each gets the full timeout because
// they were cancelled together and run concurrently. It reports whether every
// sweeper exited in time.
func stopAll(stops ...func(time.Duration) bool) func(time.Duration) bool {
	return func(timeout time.Duration) bool {
		ok := true
		for _, stop := range stops {
			if !stop(timeout) {
				ok = false
			}
		}
		return ok
	}
}
