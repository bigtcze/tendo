package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// invitationSweepStatements maps each retention step to its bounded batch
// statement. Every statement changes at most identity.InvitationSweepBatchSize
// rows. The digest step skips rows another transaction holds; the OIDC delete
// steps take no row lock first (the runtime role has no UPDATE on those
// tables), match only expired rows that no live request uses, and wait at most
// lock_timeout before the step yields.
var invitationSweepStatements = map[identity.InvitationSweepStep]func(*identitydb.Queries, context.Context, time.Time) (int64, error){
	identity.InvitationSweepExpiredDigests: func(q *identitydb.Queries, ctx context.Context, cutoff time.Time) (int64, error) {
		return q.SweepExpiredInvitationDigests(ctx, ts(cutoff))
	},
	identity.InvitationSweepOIDCFlowsExpired: func(q *identitydb.Queries, ctx context.Context, cutoff time.Time) (int64, error) {
		return q.SweepExpiredOIDCFlows(ctx, ts(cutoff))
	},
	identity.InvitationSweepOIDCPendingLoginsExpired: func(q *identitydb.Queries, ctx context.Context, cutoff time.Time) (int64, error) {
		return q.SweepExpiredOIDCPendingLogins(ctx, ts(cutoff))
	},
}

var _ identity.InvitationSweepStore = (*InvitationRepository)(nil)

// SweepInvitationStep runs one invitation retention step (ADR 0024). Each
// batch is one bounded statement in its own transaction with lock_timeout set,
// so progress survives an early stop. It loops while batches come back full
// and ends on the context.
func (r *InvitationRepository) SweepInvitationStep(ctx context.Context, step identity.InvitationSweepStep, cutoff time.Time) (int, error) {
	statement, ok := invitationSweepStatements[step]
	if !ok {
		return 0, identity.ErrInvitationSweepFailed
	}
	total := 0
	for {
		n, err := r.sweepInvitationBatch(ctx, statement, cutoff)
		total += n
		if err != nil {
			return total, err
		}
		if n < identity.InvitationSweepBatchSize {
			return total, nil
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}

func (r *InvitationRepository) sweepInvitationBatch(ctx context.Context, statement func(*identitydb.Queries, context.Context, time.Time) (int64, error), cutoff time.Time) (int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, classifyInvitationSweepError(ctx, err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '"+sweepLockTimeout+"'"); err != nil {
		return 0, classifyInvitationSweepError(ctx, err)
	}
	n, err := statement(identitydb.New(tx), ctx, cutoff)
	if err != nil {
		return 0, classifyInvitationSweepError(ctx, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, classifyInvitationSweepError(ctx, err)
	}
	return int(n), nil
}

// classifyInvitationSweepError maps driver errors to fixed identity-layer
// classes so no driver text reaches the caller. A finished context is returned
// as itself.
func classifyInvitationSweepError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return identity.ErrInvitationSweepLocked
	}
	return identity.ErrInvitationSweepFailed
}
