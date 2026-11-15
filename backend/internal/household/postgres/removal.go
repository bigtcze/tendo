package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// MemberRemovalEffectFactories build the transaction-scoped effect adapters of
// the other modules. The removal transaction hands each factory the same
// pgx.Tx, so every effect commits or rolls back with the membership removal.
type MemberRemovalEffectFactories struct {
	Invitations func(dbgen.DBTX) household.InvitationRevoker
	Items       func(dbgen.DBTX) household.ResponsibilityReleaser
	Subjects    func(dbgen.DBTX) household.MemberPersonUnlinker
	Resets      func(dbgen.DBTX) household.PasswordResetRevoker
	Accounts    func(dbgen.DBTX) household.DefaultHouseholdReassigner
}

// MemberRemovalRepository owns the membership removal transaction. It never
// writes another module's tables; those effects arrive as factories built on
// the same transaction.
type MemberRemovalRepository struct {
	pool      accountPool
	factories MemberRemovalEffectFactories
}

func NewMemberRemovalRepository(pool accountPool, factories MemberRemovalEffectFactories) *MemberRemovalRepository {
	return &MemberRemovalRepository{pool: pool, factories: factories}
}

func (r *MemberRemovalRepository) WithMemberRemovalTransaction(ctx context.Context, work func(household.MemberRemovalTransaction, household.MemberRemovalEffects) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
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
	if err = work(&removalTx{q: dbgen.New(tx)}, effects); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *MemberRemovalRepository) effects(tx dbgen.DBTX) (household.MemberRemovalEffects, error) {
	if r.factories.Invitations == nil || r.factories.Items == nil || r.factories.Subjects == nil ||
		r.factories.Resets == nil || r.factories.Accounts == nil {
		return household.MemberRemovalEffects{}, errors.New("member removal effects unavailable")
	}
	return household.MemberRemovalEffects{
		Invitations: r.factories.Invitations(tx),
		Items:       r.factories.Items(tx),
		Subjects:    r.factories.Subjects(tx),
		Resets:      r.factories.Resets(tx),
		Accounts:    r.factories.Accounts(tx),
	}, nil
}

type removalTx struct{ q *dbgen.Queries }

func (t *removalTx) LockHouseholdForMembership(ctx context.Context, userID, householdID string) error {
	var user, house pgtype.UUID
	if user.Scan(userID) != nil || house.Scan(householdID) != nil {
		return household.ErrNotFound
	}
	_, err := t.q.LockHouseholdForMembership(ctx, dbgen.LockHouseholdForMembershipParams{ID: house, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return household.ErrNotFound
	}
	return err
}

func (t *removalTx) GetMembership(ctx context.Context, userID, householdID string) (string, error) {
	var user, house pgtype.UUID
	if user.Scan(userID) != nil || house.Scan(householdID) != nil {
		return "", household.ErrNotFound
	}
	role, err := t.q.GetMembership(ctx, dbgen.GetMembershipParams{UserID: user, HouseholdID: house})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", household.ErrNotFound
	}
	return role, err
}

func (t *removalTx) LockMemberAccount(ctx context.Context, userID string) error {
	var user pgtype.UUID
	if user.Scan(userID) != nil {
		return household.ErrNotFound
	}
	_, err := t.q.LockMemberAccountForRemoval(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return household.ErrNotFound
	}
	return err
}

func (t *removalTx) LockMembership(ctx context.Context, householdID, userID string) (household.MembershipRecord, int64, error) {
	var user, house pgtype.UUID
	if user.Scan(userID) != nil || house.Scan(householdID) != nil {
		return household.MembershipRecord{}, 0, household.ErrNotFound
	}
	row, err := t.q.LockMembershipForRemoval(ctx, dbgen.LockMembershipForRemovalParams{HouseholdID: house, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return household.MembershipRecord{}, 0, household.ErrNotFound
	}
	if err != nil {
		return household.MembershipRecord{}, 0, err
	}
	return household.MembershipRecord{UserID: userID, Role: row.Role}, row.Version, nil
}

func (t *removalTx) CountAdminsExcluding(ctx context.Context, householdID, userID string) (int, error) {
	var user, house pgtype.UUID
	if user.Scan(userID) != nil || house.Scan(householdID) != nil {
		return 0, household.ErrNotFound
	}
	count, err := t.q.CountHouseholdAdminsExcluding(ctx, dbgen.CountHouseholdAdminsExcludingParams{HouseholdID: house, UserID: user})
	return int(count), err
}

func (t *removalTx) DeleteMembership(ctx context.Context, householdID, userID string) error {
	var user, house pgtype.UUID
	if user.Scan(userID) != nil || house.Scan(householdID) != nil {
		return household.ErrNotFound
	}
	return t.q.DeleteMembership(ctx, dbgen.DeleteMembershipParams{HouseholdID: house, UserID: user})
}

func (t *removalTx) RecordMemberRemoval(ctx context.Context, removal household.MemberRemoval) error {
	house, err := scanUUID(removal.HouseholdID)
	if err != nil {
		return household.ErrNotFound
	}
	target, err := scanUUID(removal.TargetUserID)
	if err != nil {
		return household.ErrNotFound
	}
	actor, err := scanUUID(removal.ActorUserID)
	if err != nil {
		return household.ErrNotFound
	}
	return t.q.InsertHouseholdMemberRemoval(ctx, dbgen.InsertHouseholdMemberRemovalParams{
		HouseholdID: house, TargetUserID: target, ActorUserID: actor,
		PreviousRole: removal.PreviousRole,
		RemovedAt:    pgtype.Timestamptz{Time: removal.RemovedAt, Valid: true},
	})
}
