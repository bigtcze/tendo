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

type accountPool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// AccountRepository owns membership role and version persistence. It never
// reads identity-owned tables; logins and sign-in methods are injected lookups.
type AccountRepository struct{ pool accountPool }

func NewAccountRepository(pool accountPool) *AccountRepository {
	return &AccountRepository{pool: pool}
}

func AccountServiceFor(pool accountPool, logins household.LoginLookup, signIn household.SignInMethodsLookup, clock func() time.Time) *household.AccountService {
	return household.NewAccountService(NewAccountRepository(pool), logins, signIn, clock)
}

func (r *AccountRepository) WithAccountTransaction(ctx context.Context, work func(household.AccountTransaction) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if err = work(&accountTx{q: dbgen.New(tx)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type accountTx struct{ q *dbgen.Queries }

func (t *accountTx) LockHouseholdForMembership(ctx context.Context, userID, householdID string) error {
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

func (t *accountTx) GetMembership(ctx context.Context, userID, householdID string) (string, error) {
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

func (t *accountTx) GetAccount(ctx context.Context, householdID, userID string) (household.AccountRecord, error) {
	var user, house pgtype.UUID
	if user.Scan(userID) != nil || house.Scan(householdID) != nil {
		return household.AccountRecord{}, household.ErrNotFound
	}
	row, err := t.q.GetMembershipAccount(ctx, dbgen.GetMembershipAccountParams{HouseholdID: house, UserID: user})
	if errors.Is(err, pgx.ErrNoRows) {
		return household.AccountRecord{}, household.ErrNotFound
	}
	if err != nil {
		return household.AccountRecord{}, err
	}
	if row.Role != household.RoleAdmin && row.Role != household.RoleMember {
		return household.AccountRecord{}, errors.New("membership persistence returned unknown role")
	}
	return household.AccountRecord{UserID: row.UserID, Role: row.Role, Version: row.Version}, nil
}

func (t *accountTx) ListAccounts(ctx context.Context, householdID, cursor string, limit int) ([]household.AccountRecord, string, error) {
	var house, after pgtype.UUID
	if err := house.Scan(householdID); err != nil {
		return nil, "", household.ErrNotFound
	}
	if cursor != "" {
		if err := after.Scan(cursor); err != nil {
			return nil, "", &household.ValidationError{Field: "cursor", Code: "invalid_format"}
		}
	}
	rows, err := t.q.ListMembershipAccounts(ctx, dbgen.ListMembershipAccountsParams{HouseholdID: house, Column2: after, Limit: int32(limit + 1)})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = household.EncodeAccountCursor(rows[len(rows)-1].UserID)
	}
	out := make([]household.AccountRecord, 0, len(rows))
	for _, row := range rows {
		if row.Role != household.RoleAdmin && row.Role != household.RoleMember {
			return nil, "", errors.New("membership persistence returned unknown role")
		}
		out = append(out, household.AccountRecord{UserID: row.UserID, Role: row.Role, Version: row.Version})
	}
	return out, next, nil
}

func (t *accountTx) UpdateAccountRole(ctx context.Context, householdID, userID, role string) (int64, error) {
	var user, house pgtype.UUID
	if user.Scan(userID) != nil || house.Scan(householdID) != nil {
		return 0, household.ErrNotFound
	}
	version, err := t.q.UpdateMembershipRole(ctx, dbgen.UpdateMembershipRoleParams{HouseholdID: house, UserID: user, Role: role})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, household.ErrNotFound
	}
	return version, err
}

func (t *accountTx) CountAdmins(ctx context.Context, householdID string) (int, error) {
	var house pgtype.UUID
	if err := house.Scan(householdID); err != nil {
		return 0, household.ErrNotFound
	}
	count, err := t.q.CountHouseholdAdmins(ctx, house)
	return int(count), err
}

func (t *accountTx) RecordRoleChange(ctx context.Context, change household.RoleChange) error {
	house, err := scanUUID(change.HouseholdID)
	if err != nil {
		return household.ErrNotFound
	}
	target, err := scanUUID(change.TargetUserID)
	if err != nil {
		return household.ErrNotFound
	}
	actor, err := scanUUID(change.ActorUserID)
	if err != nil {
		return household.ErrNotFound
	}
	return t.q.InsertHouseholdRoleChange(ctx, dbgen.InsertHouseholdRoleChangeParams{
		HouseholdID: house, TargetUserID: target, ActorUserID: actor,
		PreviousRole: change.PreviousRole, NewRole: change.NewRole,
		ChangedAt: pgtype.Timestamptz{Time: change.ChangedAt, Valid: true},
	})
}

func scanUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	err := id.Scan(value)
	return id, err
}
