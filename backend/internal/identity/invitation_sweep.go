package identity

import (
	"context"
	"errors"
	"time"
)

// Invitation retention (ADR 0024). Only a pending invitation keeps the digest
// of its bearer token. Acceptance and revocation clear it in their own
// transaction; this sweep clears it for invitations past expiry, which can
// never be accepted. The redacted row stays as history. The sweep also deletes
// expired OIDC flows and pending sign-ins: they can carry a copy of an
// invitation digest, and nothing reads them after expiry.

// InvitationSweepStep names one invitation retention step.
type InvitationSweepStep string

const (
	// InvitationSweepExpiredDigests clears the token digest of invitations past
	// expires_at. The row is kept.
	InvitationSweepExpiredDigests InvitationSweepStep = "expired_digests_cleared"
	// InvitationSweepOIDCFlowsExpired deletes OIDC flows past expires_at.
	InvitationSweepOIDCFlowsExpired InvitationSweepStep = "oidc_flows_expired"
	// InvitationSweepOIDCPendingLoginsExpired deletes pending OIDC sign-ins past
	// expires_at.
	InvitationSweepOIDCPendingLoginsExpired InvitationSweepStep = "oidc_pending_logins_expired"
)

// InvitationSweepSteps is the run order. The steps are independent; each batch
// runs in its own transaction.
var InvitationSweepSteps = []InvitationSweepStep{
	InvitationSweepExpiredDigests,
	InvitationSweepOIDCFlowsExpired,
	InvitationSweepOIDCPendingLoginsExpired,
}

// InvitationSweepBatchSize bounds the rows one statement may change. A step
// loops while a batch comes back full and stops when the context ends.
const InvitationSweepBatchSize = 1000

var (
	// ErrInvitationSweepLocked reports that a step gave up on a lock held by
	// another transaction (lock_timeout). The other steps still run; the next
	// run retries it.
	ErrInvitationSweepLocked = errors.New("invitation sweep step yielded to a lock")
	// ErrInvitationSweepFailed is the fixed class for any other storage failure.
	// Driver text stays in the persistence layer.
	ErrInvitationSweepFailed = errors.New("invitation sweep step failed")
)

// InvitationSweepStore runs one step for rows whose expiry is at or before
// cutoff, in bounded batches that each commit on their own. It returns how
// many rows it changed, also when it stops early.
type InvitationSweepStore interface {
	SweepInvitationStep(ctx context.Context, step InvitationSweepStep, cutoff time.Time) (int, error)
}

// InvitationSweepReport is the outcome of one sweep run.
type InvitationSweepReport struct {
	// Swept counts the rows each completed step changed.
	Swept map[InvitationSweepStep]int
	// Locked lists the steps that yielded to a lock this run.
	Locked []InvitationSweepStep
}

// SweepInvitationDigests runs one retention pass at now. A lock yield ends
// that step only; a finished context returns its own error so the caller can
// classify a timeout or a cancellation; any other failure stops the run with
// ErrInvitationSweepFailed.
func SweepInvitationDigests(ctx context.Context, store InvitationSweepStore, now time.Time) (InvitationSweepReport, error) {
	report := InvitationSweepReport{Swept: map[InvitationSweepStep]int{}}
	if store == nil {
		return report, ErrInvitationSweepFailed
	}
	for _, step := range InvitationSweepSteps {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		n, err := store.SweepInvitationStep(ctx, step, now.UTC())
		switch {
		case err == nil:
			report.Swept[step] = n
		case errors.Is(err, ErrInvitationSweepLocked):
			report.Swept[step] = n
			report.Locked = append(report.Locked, step)
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			report.Swept[step] = n
			return report, err
		default:
			return report, ErrInvitationSweepFailed
		}
	}
	return report, nil
}
