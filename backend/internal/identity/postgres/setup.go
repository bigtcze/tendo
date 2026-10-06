package postgres

import (
	"context"
	"errors"
	"time"

	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HouseholdRepositoryFactory func(*householddb.Queries) identity.OwnerHouseholdService

type Repository struct {
	pool    *pgxpool.Pool
	factory HouseholdRepositoryFactory
}

func New(pool *pgxpool.Pool, factory HouseholdRepositoryFactory) *Repository {
	return &Repository{pool: pool, factory: factory}
}

func (r *Repository) IsRequired(ctx context.Context) (bool, error) {
	var required bool
	err := r.pool.QueryRow(ctx, `SELECT setup_required FROM installation_state WHERE singleton = true`).Scan(&required)
	return required, err
}

func (r *Repository) WithSetupTransaction(ctx context.Context, work func(identity.SetupTransaction, identity.OwnerHouseholdService) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return errors.New("setup transaction failed")
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	queries := identitydb.New(tx)
	householdQueries := householddb.New(tx)
	if r.factory == nil {
		return errors.New("setup transaction unavailable")
	}
	if err = work(&setupTransaction{queries: queries}, r.factory(householdQueries)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("setup persistence failed")
	}
	return nil
}

type setupTransaction struct{ queries *identitydb.Queries }

func (t *setupTransaction) LockRequired(ctx context.Context) (bool, error) {
	return t.queries.LockSetupState(ctx)
}
func (t *setupTransaction) CreateUser(ctx context.Context, login string) (string, error) {
	return t.queries.CreateUser(ctx, login)
}
func (t *setupTransaction) CreateCredential(ctx context.Context, userID, hash string) error {
	var id pgtype.UUID
	if err := id.Scan(userID); err != nil {
		return err
	}
	return t.queries.CreateLocalCredential(ctx, identitydb.CreateLocalCredentialParams{UserID: id, PasswordHash: hash})
}
func (t *setupTransaction) SetDefaultHousehold(ctx context.Context, userID, householdID string) error {
	var user, household pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return err
	}
	if err := household.Scan(householdID); err != nil {
		return err
	}
	return t.queries.SetDefaultHousehold(ctx, identitydb.SetDefaultHouseholdParams{ID: user, DefaultHouseholdID: household})
}
func (t *setupTransaction) CompleteSetup(ctx context.Context) error {
	return t.queries.CompleteSetup(ctx)
}
