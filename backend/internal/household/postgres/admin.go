package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// AssignmentEffectFactories build the transaction-scoped assignment effects of
// the identity and subject modules on the instance-admin transaction.
type AssignmentEffectFactories struct {
	Accounts func(dbgen.DBTX) household.AssignableAccountLocker
	Defaults func(dbgen.DBTX) household.AssignmentDefaultSetter
	Persons  func(dbgen.DBTX) household.AssignmentPersonProvisioner
}

// InstanceAdminCenterRepository owns the instance-admin transaction (ADR 0020).
// Role changes and removals reuse the household-admin statements; effects on
// other modules' tables arrive as factories built on the same transaction.
type InstanceAdminCenterRepository struct {
	pool       accountPool
	removal    MemberRemovalEffectFactories
	assignment AssignmentEffectFactories
}

func NewInstanceAdminCenterRepository(pool accountPool, removal MemberRemovalEffectFactories, assignment AssignmentEffectFactories) *InstanceAdminCenterRepository {
	return &InstanceAdminCenterRepository{pool: pool, removal: removal, assignment: assignment}
}

func (r *InstanceAdminCenterRepository) WithInstanceAdminTransaction(ctx context.Context, work func(household.InstanceAdminTransaction, household.InstanceAdminEffects) error) error {
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
	q := dbgen.New(tx)
	if err = work(&instanceAdminTx{accountTx: accountTx{q: q}, removalTx: removalTx{q: q}, q: q}, effects); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *InstanceAdminCenterRepository) effects(tx dbgen.DBTX) (household.InstanceAdminEffects, error) {
	removal, err := (&MemberRemovalRepository{factories: r.removal}).effects(tx)
	if err != nil {
		return household.InstanceAdminEffects{}, err
	}
	if r.assignment.Accounts == nil || r.assignment.Defaults == nil || r.assignment.Persons == nil {
		return household.InstanceAdminEffects{}, errors.New("assignment effects unavailable")
	}
	return household.InstanceAdminEffects{
		Removal: removal,
		Assignment: household.AssignmentEffects{
			Accounts: r.assignment.Accounts(tx),
			Defaults: r.assignment.Defaults(tx),
			Persons:  r.assignment.Persons(tx),
		},
	}, nil
}

// instanceAdminTx embeds both household-admin statement sets. The membership
// gate methods they share (LockHouseholdForMembership, GetMembership) are
// identical; the instance-admin service never calls them, it locks the
// household by ID after RequireInstanceAdmin.
type instanceAdminTx struct {
	accountTx
	removalTx
	q *dbgen.Queries
}

func (t *instanceAdminTx) LockHouseholdForMembership(ctx context.Context, userID, householdID string) error {
	return t.accountTx.LockHouseholdForMembership(ctx, userID, householdID)
}

func (t *instanceAdminTx) GetMembership(ctx context.Context, userID, householdID string) (string, error) {
	return t.accountTx.GetMembership(ctx, userID, householdID)
}

func (t *instanceAdminTx) RequireInstanceAdmin(ctx context.Context, actorID string) error {
	actor, err := scanUUID(actorID)
	if err != nil {
		return household.ErrInstanceAdminRequired
	}
	setupRequired, err := t.q.LockInstallationForInstanceAdmin(ctx)
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

func (t *instanceAdminTx) LockHousehold(ctx context.Context, householdID string) error {
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

func (t *instanceAdminTx) HouseholdExists(ctx context.Context, householdID string) error {
	house, err := scanUUID(householdID)
	if err != nil {
		return household.ErrNotFound
	}
	present, err := t.q.HouseholdExists(ctx, house)
	if err != nil {
		return err
	}
	if !present {
		return household.ErrNotFound
	}
	return nil
}

func (t *instanceAdminTx) ListHouseholds(ctx context.Context, afterID string, limit int) ([]household.AdminHousehold, error) {
	var after pgtype.UUID
	if afterID != "" {
		if err := after.Scan(afterID); err != nil {
			return nil, &household.ValidationError{Field: "cursor", Code: "invalid_format"}
		}
	}
	rows, err := t.q.ListAllHouseholds(ctx, dbgen.ListAllHouseholdsParams{AfterID: after, RowLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]household.AdminHousehold, 0, len(rows))
	for _, row := range rows {
		out = append(out, household.AdminHousehold{ID: row.ID, Name: row.Name, Timezone: row.Timezone, CreatedAt: row.CreatedAt.Time})
	}
	return out, nil
}

func (t *instanceAdminTx) InsertMembership(ctx context.Context, householdID, userID, role string) (int64, error) {
	var user, house pgtype.UUID
	if user.Scan(userID) != nil || house.Scan(householdID) != nil {
		return 0, household.ErrNotFound
	}
	version, err := t.q.InsertAssignedMembership(ctx, dbgen.InsertAssignedMembershipParams{UserID: user, HouseholdID: house, Role: role})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return 0, household.ErrAlreadyMember
	}
	return version, err
}

func (t *instanceAdminTx) RecordMemberAssignment(ctx context.Context, assignment household.MemberAssignment) error {
	house, err := scanUUID(assignment.HouseholdID)
	if err != nil {
		return household.ErrNotFound
	}
	target, err := scanUUID(assignment.TargetUserID)
	if err != nil {
		return household.ErrNotFound
	}
	actor, err := scanUUID(assignment.ActorUserID)
	if err != nil {
		return household.ErrNotFound
	}
	return t.q.InsertHouseholdMemberAssignment(ctx, dbgen.InsertHouseholdMemberAssignmentParams{
		HouseholdID: house, TargetUserID: target, ActorUserID: actor,
		Role: assignment.Role, ActorAuthority: assignment.ActorAuthority,
		AssignedAt: pgtype.Timestamptz{Time: assignment.AssignedAt, Valid: true},
	})
}
