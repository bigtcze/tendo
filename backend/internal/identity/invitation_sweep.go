package identity

import (
	"context"
	"errors"
	"time"
)

// Invitation retention (ADR 0024). Only a pending invitation keeps the digest
// of its bearer token. Acceptance and revocation clear it in their own
// transaction; this sweep clears it for invitations past expiry, which can
// never be accepted. The redacted row stays as history.

// InvitationSweepBatchSize bounds the digests one statement may clear. The
// store loops while a batch comes back full and stops when the context ends.
const InvitationSweepBatchSize = 1000

var (
	// ErrInvitationSweepLocked reports that the sweep gave up on a lock held by
	// another transaction (lock_timeout). The next run retries.
	ErrInvitationSweepLocked = errors.New("invitation sweep yielded to a lock")
	// ErrInvitationSweepFailed is the fixed class for any other storage failure.
	// Driver text stays in the persistence layer.
	ErrInvitationSweepFailed = errors.New("invitation sweep failed")
)

// InvitationSweepStore clears, in bounded batches that each commit on their
// own, the token digests of invitations whose expiry is at or before cutoff.
// It returns how many digests it cleared, also when it stops early.
type InvitationSweepStore interface {
	SweepExpiredInvitationDigests(ctx context.Context, cutoff time.Time) (int, error)
}

// InvitationSweepReport is the outcome of one sweep run.
type InvitationSweepReport struct {
	// Cleared counts the expired digests removed this run.
	Cleared int
	// Locked reports that the run yielded to a lock.
	Locked bool
}

// SweepInvitationDigests runs one retention pass at now. A lock yield is not a
// failure; a finished context returns its own error so the caller can classify
// a timeout or a cancellation; any other failure is ErrInvitationSweepFailed.
func SweepInvitationDigests(ctx context.Context, store InvitationSweepStore, now time.Time) (InvitationSweepReport, error) {
	if store == nil {
		return InvitationSweepReport{}, ErrInvitationSweepFailed
	}
	if err := ctx.Err(); err != nil {
		return InvitationSweepReport{}, err
	}
	n, err := store.SweepExpiredInvitationDigests(ctx, now.UTC())
	report := InvitationSweepReport{Cleared: n}
	switch {
	case err == nil:
		return report, nil
	case errors.Is(err, ErrInvitationSweepLocked):
		report.Locked = true
		return report, nil
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return report, err
	default:
		return report, ErrInvitationSweepFailed
	}
}
