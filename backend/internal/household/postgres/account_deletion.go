package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// AccountDeletionRepository owns the global account deletion transaction (ADR
// 0022). It reuses the membership removal statements of ADR 0019 and hands the
// identity-owned eraser the same transaction, so every effect commits or rolls
// back together.
type AccountDeletionRepository struct {
	pool    accountPool
	removal MemberRemovalEffectFactories
	account func(dbgen.DBTX) household.AccountEraser
}

func NewAccountDeletionRepository(pool accountPool, removal MemberRemovalEffectFactories, account func(dbgen.DBTX) household.AccountEraser) *AccountDeletionRepository {
	return &AccountDeletionRepository{pool: pool, removal: removal, account: account}
}

// WithAccountDeletionTransaction runs one deletion attempt in READ COMMITTED.
// ADR 0019 forbids REPEATABLE READ here: the last-admin and
// last-instance-administrator decisions are answered by rows read under the
// locks this transaction takes, and a snapshot could answer them stale.
func (r *AccountDeletionRepository) WithAccountDeletionTransaction(ctx context.Context, work func(household.AccountDeletionTransaction, household.AccountDeletionEffects) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	effects, err := r.effects(tx)
	if err != nil {
		return err
	}
	q := dbgen.New(tx)
	if err = work(&accountDeletionTx{removalTx: removalTx{q: q}, q: q}, effects); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *AccountDeletionRepository) effects(tx dbgen.DBTX) (household.AccountDeletionEffects, error) {
	removal, err := (&MemberRemovalRepository{factories: r.removal}).effects(tx)
	if err != nil {
		return household.AccountDeletionEffects{}, err
	}
	if r.account == nil {
		return household.AccountDeletionEffects{}, errors.New("account eraser unavailable")
	}
	return household.AccountDeletionEffects{Removal: removal, Account: r.account(tx)}, nil
}

// accountDeletionTx keeps the ADR 0019 global lock order: installation_state,
// the households, invitations, the account row, the memberships, then the
// item/subject/reset effects. It embeds the removal statements so each
// membership is removed through the shared core.
type accountDeletionTx struct {
	removalTx
	q *dbgen.Queries
}

func (t *accountDeletionTx) RequireInstanceAdminExclusive(ctx context.Context, actorID string) error {
	actor, err := scanUUID(actorID)
	if err != nil {
		return household.ErrInstanceAdminRequired
	}
	setupRequired, err := t.q.LockInstallationForInstanceAdminExclusive(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return household.ErrInstanceAdminRequired
	}
	if err != nil {
		return err
	}
	if setupRequired {
		return household.ErrInstanceAdminRequired
	}
	granted, err := t.q.IsInstanceAdmin(ctx, actor)
	if err != nil {
		return err
	}
	if !granted {
		return household.ErrInstanceAdminRequired
	}
	return nil
}

func (t *accountDeletionTx) LockHousehold(ctx context.Context, householdID string) error {
	house, err := scanUUID(householdID)
	if err != nil {
		return household.ErrNotFound
	}
	_, err = t.q.LockHouseholdByID(ctx, house)
	if errors.Is(err, pgx.ErrNoRows) {
		return household.ErrNotFound
	}
	return err
}

func (t *accountDeletionTx) ListUserHouseholds(ctx context.Context, userID string) ([]string, error) {
	user, err := scanUUID(userID)
	if err != nil {
		return nil, household.ErrNotFound
	}
	return t.q.ListUserHouseholds(ctx, user)
}

func (t *accountDeletionTx) LockUserMemberships(ctx context.Context, userID string) ([]household.AccountMembership, error) {
	user, err := scanUUID(userID)
	if err != nil {
		return nil, household.ErrNotFound
	}
	rows, err := t.q.LockUserMemberships(ctx, user)
	if err != nil {
		return nil, err
	}
	out := make([]household.AccountMembership, 0, len(rows))
	for _, row := range rows {
		out = append(out, household.AccountMembership{HouseholdID: row.HouseholdID, Role: row.Role})
	}
	return out, nil
}

func (t *accountDeletionTx) FindInstanceAdminGrant(ctx context.Context, userID string) (household.InstanceAdminGrantRecord, bool, error) {
	user, err := scanUUID(userID)
	if err != nil {
		return household.InstanceAdminGrantRecord{}, false, household.ErrNotFound
	}
	row, err := t.q.FindInstanceAdminGrant(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return household.InstanceAdminGrantRecord{}, false, nil
	}
	if err != nil {
		return household.InstanceAdminGrantRecord{}, false, err
	}
	return household.InstanceAdminGrantRecord{Source: row.GrantSource, GrantedAt: row.GrantedAt.Time}, true, nil
}

func (t *accountDeletionTx) CountLiveInstanceAdminsExcluding(ctx context.Context, userID string) (int, error) {
	user, err := scanUUID(userID)
	if err != nil {
		return 0, household.ErrNotFound
	}
	count, err := t.q.CountLiveInstanceAdminsExcluding(ctx, user)
	return int(count), err
}

// RevokeInstanceAdmin writes the append-only audit row first, then deletes the
// grant. The delete must hit exactly one row: any other count means the grant
// set moved under the installation lock, so the attempt is abandoned.
func (t *accountDeletionTx) RevokeInstanceAdmin(ctx context.Context, revocation household.InstanceAdminRevocation) error {
	target, err := scanUUID(revocation.TargetUserID)
	if err != nil {
		return household.ErrNotFound
	}
	actor, err := scanUUID(revocation.ActorUserID)
	if err != nil {
		return household.ErrNotFound
	}
	if err = t.q.InsertInstanceAdminRevocation(ctx, dbgen.InsertInstanceAdminRevocationParams{
		UserID: target, ActorUserID: actor, GrantSource: revocation.GrantSource,
		GrantedAt: pgtype.Timestamptz{Time: revocation.GrantedAt, Valid: true},
		RevokedAt: pgtype.Timestamptz{Time: revocation.RevokedAt, Valid: true},
		Reason:    revocation.Reason,
	}); err != nil {
		return err
	}
	deleted, err := t.q.DeleteInstanceAdminGrant(ctx, target)
	if err != nil {
		return err
	}
	if deleted != 1 {
		return fmt.Errorf("instance administrator grant of %s disappeared during revocation", revocation.TargetUserID)
	}
	return nil
}

func (t *accountDeletionTx) RecordAccountDeletion(ctx context.Context, deletion household.AccountDeletion) error {
	target, err := scanUUID(deletion.TargetUserID)
	if err != nil {
		return household.ErrNotFound
	}
	actor, err := scanUUID(deletion.ActorUserID)
	if err != nil {
		return household.ErrNotFound
	}
	return t.q.InsertAccountDeletion(ctx, dbgen.InsertAccountDeletionParams{
		UserID: target, ActorUserID: actor,
		DeletedAt: pgtype.Timestamptz{Time: deletion.DeletedAt, Valid: true},
	})
}
