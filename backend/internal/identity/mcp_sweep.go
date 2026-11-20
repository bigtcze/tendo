package identity

import (
	"context"
	"errors"
	"time"
)

// MCPSweepStep names one retention step (ADR 0023, "Retention sweep"). Expiry
// is not revocation: the sweep deletes only expired or unreachable secrets and
// writes no grant event.
type MCPSweepStep string

const (
	// MCPSweepAccessTokensExpired deletes access tokens past expires_at.
	MCPSweepAccessTokensExpired MCPSweepStep = "access_tokens_expired"
	// MCPSweepRefreshTokensExpired deletes unconsumed refresh tokens past expires_at.
	// A consumed digest is kept until its grant ends, so replay stays detectable.
	MCPSweepRefreshTokensExpired MCPSweepStep = "refresh_tokens_expired"
	// MCPSweepEndedGrantSecrets deletes the codes and token rows of grants past
	// expires_at, revoked or not (a revoked grant already holds none). The grant
	// row itself is kept.
	MCPSweepEndedGrantSecrets MCPSweepStep = "ended_grant_secrets"
	// MCPSweepUnusedCodesExpired deletes unused authorization codes past expires_at.
	MCPSweepUnusedCodesExpired MCPSweepStep = "unused_codes_expired"
	// MCPSweepGrantlessClients deletes clients older than MCPGrantlessClientLifetime
	// that hold no grant and no authorization code.
	MCPSweepGrantlessClients MCPSweepStep = "grantless_clients"
)

// MCPSweepSteps is the run order: the steps are independent, and each one runs
// in its own transaction.
var MCPSweepSteps = []MCPSweepStep{
	MCPSweepAccessTokensExpired,
	MCPSweepRefreshTokensExpired,
	MCPSweepEndedGrantSecrets,
	MCPSweepUnusedCodesExpired,
	MCPSweepGrantlessClients,
}

// MCPSweepBatchSize bounds the rows one statement may delete. A step loops
// while a batch comes back full, and it stops when the context ends.
const MCPSweepBatchSize = 1000

var (
	// ErrMCPSweepLocked reports that a step gave up on a lock held by another
	// transaction (lock_timeout). The step ends for this run and the other steps
	// still run; the next run retries it.
	ErrMCPSweepLocked = errors.New("mcp sweep step yielded to a lock")
	// ErrMCPSweepFailed is the fixed class for any other sweep storage failure.
	// Driver text stays in the persistence layer.
	ErrMCPSweepFailed = errors.New("mcp sweep step failed")
)

// MCPSweepStore runs one retention step in one transaction, with lock_timeout
// set, and returns how many rows it deleted.
type MCPSweepStore interface {
	SweepStep(ctx context.Context, step MCPSweepStep, now time.Time) (int, error)
}

// MCPSweepReport is the outcome of one sweep run.
type MCPSweepReport struct {
	// Deleted counts the rows each completed step removed.
	Deleted map[MCPSweepStep]int
	// Locked lists the steps that yielded to a lock this run.
	Locked []MCPSweepStep
}

// Sweep runs the retention steps. It runs regardless of the enabled flag, so a
// disabled installation still clears expired secrets. A lock yield ends that
// step only; any other failure stops the run and returns ErrMCPSweepFailed.
func (s *MCPService) Sweep(ctx context.Context, now time.Time) (MCPSweepReport, error) {
	report := MCPSweepReport{Deleted: map[MCPSweepStep]int{}}
	if s.sweeper == nil {
		return report, ErrMCPSweepFailed
	}
	for _, step := range MCPSweepSteps {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		n, err := s.sweeper.SweepStep(ctx, step, now.UTC())
		switch {
		case errors.Is(err, ErrMCPSweepLocked):
			report.Locked = append(report.Locked, step)
		case err != nil:
			return report, ErrMCPSweepFailed
		default:
			report.Deleted[step] = n
		}
	}
	return report, nil
}
