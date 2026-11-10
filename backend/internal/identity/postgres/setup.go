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

// MemberPersonProvisionerFactory binds the subject provisioner to the same
// transaction that writes the membership. The process composition root supplies
// the concrete adapter; identity itself never imports the subject module.
type MemberPersonProvisionerFactory func(householddb.DBTX) identity.MemberPersonProvisioner

type Repository struct {
	pool                *pgxpool.Pool
	factory             HouseholdRepositoryFactory
	memberPersonFactory MemberPersonProvisionerFactory
	// invitationHousehold binds invitation acceptance to the OIDC first-login
	// transaction. It is injected at construction and required whenever a
	// carried invitation must be honoured.
	invitationHousehold InvitationHouseholdServiceFactory
	// emailClaimPause is a test-only barrier that runs after a successful email
	// claim (still uncommitted), so concurrency tests can make the loser hit the
	// unique index deterministically. Production wiring leaves it nil.
	emailClaimPause func()
	// beforeIdentityBind is a test-only barrier that runs after the row-7
	// re-read and inside the binding savepoint, so tests can commit a concurrent
	// (user_id, issuer) binding before the invitation acceptance and identity
	// insert. Production leaves it nil.
	beforeIdentityBind func()
}

// New builds the identity repository. The member-person factory is required for
// setup transactions; omitting it fails closed before any membership write.
// Read-only adapters that never open a setup transaction may still pass none.
func New(pool *pgxpool.Pool, factory HouseholdRepositoryFactory, memberPersonFactory ...MemberPersonProvisionerFactory) *Repository {
	repository := &Repository{pool: pool, factory: factory}
	if len(memberPersonFactory) > 0 {
		repository.memberPersonFactory = memberPersonFactory[0]
	}
	return repository
}

// NewWithInvitations builds the production repository with the invitation
// acceptance adapter the OIDC first-login transaction needs. The dependency is
// fixed at construction so no request can observe a half-wired repository.
func NewWithInvitations(pool *pgxpool.Pool, factory HouseholdRepositoryFactory, memberPersonFactory MemberPersonProvisionerFactory, invitationFactory InvitationHouseholdServiceFactory) *Repository {
	return &Repository{pool: pool, factory: factory, memberPersonFactory: memberPersonFactory, invitationHousehold: invitationFactory}
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
	if r.factory == nil || r.memberPersonFactory == nil {
		return errors.New("setup transaction unavailable")
	}
	provisioner := r.memberPersonFactory(tx)
	if err = work(&setupTransaction{queries: queries, memberPersons: provisioner}, r.factory(householdQueries)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return errors.New("setup persistence failed")
	}
	return nil
}

type setupTransaction struct {
	queries       *identitydb.Queries
	memberPersons identity.MemberPersonProvisioner
}

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
func (t *setupTransaction) EnsureMemberPerson(ctx context.Context, householdID, userID, name string) error {
	return t.memberPersons.EnsureMemberPerson(ctx, householdID, userID, name)
}
func (t *setupTransaction) CompleteSetup(ctx context.Context) error {
	return t.queries.CompleteSetup(ctx)
}
