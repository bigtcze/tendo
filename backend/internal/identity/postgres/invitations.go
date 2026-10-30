package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type InvitationHouseholdService interface {
	RequireOwner(context.Context, string, string) error
	GetMembership(context.Context, string, string) (string, error)
	HasMembershipElsewhere(context.Context, string, string) (bool, error)
	AddInvitedMember(context.Context, string, string) error
}
type InvitationHouseholdServiceFactory func(householddb.DBTX) InvitationHouseholdService
type InvitationRepository struct {
	pool interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
		Query(context.Context, string, ...any) (pgx.Rows, error)
		QueryRow(context.Context, string, ...any) pgx.Row
	}
	factory InvitationHouseholdServiceFactory
}

func NewInvitationRepository(pool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}, factory InvitationHouseholdServiceFactory) *InvitationRepository {
	return &InvitationRepository{pool, factory}
}

func (r *InvitationRepository) WithInvitationTransaction(ctx context.Context, work func(identity.InvitationTransaction) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if r.factory == nil {
		return errors.New("invitation transaction unavailable")
	}
	wrapped := &invitationTx{q: identitydb.New(tx), h: r.factory(tx)}
	if err = work(wrapped); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *InvitationRepository) FindInvitation(ctx context.Context, digest []byte) (identity.Invitation, error) {
	return findInvitation(ctx, identitydb.New(r.pool), digest, false)
}

type invitationTx struct {
	q *identitydb.Queries
	h InvitationHouseholdService
}

func (t *invitationTx) RequireOwner(ctx context.Context, userID, householdID string) error {
	return t.h.RequireOwner(ctx, userID, householdID)
}
func (t *invitationTx) GetMembership(ctx context.Context, userID, householdID string) (string, error) {
	return t.h.GetMembership(ctx, userID, householdID)
}
func (t *invitationTx) HasOtherMembership(ctx context.Context, userID, householdID string) (bool, error) {
	return t.h.HasMembershipElsewhere(ctx, userID, householdID)
}
func (t *invitationTx) CreateInvitation(ctx context.Context, input identity.CreateInvitationInput) (identity.Invitation, bool, error) {
	var householdID, actorID pgtype.UUID
	if err := householdID.Scan(input.HouseholdID); err != nil {
		return identity.Invitation{}, false, household.ErrNotFound
	}
	if err := actorID.Scan(input.ActorID); err != nil {
		return identity.Invitation{}, false, household.ErrNotFound
	}
	params := identitydb.CreateInvitationParams{
		HouseholdID: householdID, CreatedByUserID: actorID,
		CreationKey: input.CreationKey, TokenHash: input.TokenHash,
		CreatedAt: ts(input.CreatedAt), ExpiresAt: ts(input.ExpiresAt),
	}
	row, err := t.q.CreateInvitation(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, lookupErr := t.q.FindInvitationByCreationKey(ctx, identitydb.FindInvitationByCreationKeyParams{
			HouseholdID: householdID, CreatedByUserID: actorID,
			CreationKey: input.CreationKey,
		})
		return invitationFromColumns(invitationColumns{
			id: existing.ID, householdID: existing.HouseholdID,
			creatorID: existing.CreatedByUserID, createdAt: existing.CreatedAt,
			expiresAt: existing.ExpiresAt, acceptedAt: existing.AcceptedAt,
			revokedAt: existing.RevokedAt,
		}), false, lookupErr
	}
	return invitationFromColumns(invitationColumns{
		id: row.ID, householdID: row.HouseholdID, creatorID: row.CreatedByUserID,
		createdAt: row.CreatedAt, expiresAt: row.ExpiresAt,
		acceptedAt: row.AcceptedAt, revokedAt: row.RevokedAt,
	}), err == nil, err
}

func (t *invitationTx) ListInvitations(ctx context.Context, householdValue, cursor string, limit int) ([]identity.Invitation, string, error) {
	var householdID, after pgtype.UUID
	if err := householdID.Scan(householdValue); err != nil {
		return nil, "", household.ErrNotFound
	}
	if cursor != "" {
		if err := after.Scan(cursor); err != nil {
			return nil, "", &identity.ValidationError{Field: "cursor", Code: "invalid_format"}
		}
	}
	rows, err := t.q.ListInvitations(ctx, identitydb.ListInvitationsParams{HouseholdID: householdID, Column2: after, Limit: int32(limit)})
	if err != nil {
		return nil, "", err
	}
	invitations := make([]identity.Invitation, 0, len(rows))
	for _, row := range rows {
		invitations = append(invitations, invitationFromColumns(invitationColumns{
			id: row.ID, householdID: row.HouseholdID, creatorID: row.CreatedByUserID,
			createdAt: row.CreatedAt, expiresAt: row.ExpiresAt,
			acceptedAt: row.AcceptedAt, revokedAt: row.RevokedAt,
		}))
	}
	return invitations, "", nil
}

func (t *invitationTx) RevokeInvitation(ctx context.Context, householdValue, invitationValue string, now time.Time) error {
	var householdID, invitationID pgtype.UUID
	if err := householdID.Scan(householdValue); err != nil {
		return household.ErrNotFound
	}
	if err := invitationID.Scan(invitationValue); err != nil {
		return household.ErrNotFound
	}
	rows, err := t.q.RevokeInvitation(ctx, identitydb.RevokeInvitationParams{HouseholdID: householdID, ID: invitationID, RevokedAt: ts(now)})
	if err != nil {
		return err
	}
	if rows > 0 {
		return nil
	}
	found, err := t.q.InvitationExistsInHousehold(ctx, identitydb.InvitationExistsInHouseholdParams{HouseholdID: householdID, ID: invitationID})
	if err != nil {
		return err
	}
	if !found {
		return household.ErrNotFound
	}
	return nil
}
func (t *invitationTx) FindInvitation(ctx context.Context, digest []byte, lock bool) (identity.Invitation, error) {
	return findInvitation(ctx, t.q, digest, lock)
}
func (t *invitationTx) FindLogin(ctx context.Context, login string) error {
	_, err := t.q.FindLoginForInvitation(ctx, login)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	return err
}
func (t *invitationTx) LockUser(ctx context.Context, userValue string) (string, error) {
	var userID pgtype.UUID
	if err := userID.Scan(userValue); err != nil {
		return "", identity.ErrNotFound
	}
	row, err := t.q.LockUserForInvitation(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrNotFound
	}
	return row.DefaultHouseholdID, err
}
func (t *invitationTx) CreateUser(ctx context.Context, login string) (string, error) {
	id, err := t.q.CreateUser(ctx, login)
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "user_accounts_login_key" {
		return "", identity.ErrLoginUnavailable
	}
	return id, err
}
func (t *invitationTx) CreateCredential(ctx context.Context, userValue, passwordHash string) error {
	var userID pgtype.UUID
	if err := userID.Scan(userValue); err != nil {
		return err
	}
	return t.q.CreateLocalCredential(ctx, identitydb.CreateLocalCredentialParams{UserID: userID, PasswordHash: passwordHash})
}
func (t *invitationTx) AddInvitedMember(ctx context.Context, userID, householdID string) error {
	return t.h.AddInvitedMember(ctx, userID, householdID)
}
func (t *invitationTx) SetDefaultHousehold(ctx context.Context, userValue, householdValue string) error {
	var userID, householdID pgtype.UUID
	if err := userID.Scan(userValue); err != nil {
		return err
	}
	if err := householdID.Scan(householdValue); err != nil {
		return err
	}
	return t.q.SetDefaultHousehold(ctx, identitydb.SetDefaultHouseholdParams{ID: userID, DefaultHouseholdID: householdID})
}
func (t *invitationTx) AcceptInvitation(ctx context.Context, invitationValue string, now time.Time) error {
	var invitationID pgtype.UUID
	if err := invitationID.Scan(invitationValue); err != nil {
		return identity.ErrInvalidInvitation
	}
	rows, err := t.q.AcceptInvitation(ctx, identitydb.AcceptInvitationParams{ID: invitationID, AcceptedAt: ts(now)})
	if err != nil {
		return err
	}
	if rows != 1 {
		return identity.ErrInvalidInvitation
	}
	return nil
}

func findInvitation(ctx context.Context, queries *identitydb.Queries, digest []byte, lock bool) (identity.Invitation, error) {
	var row identitydb.FindInvitationByTokenRow
	var err error
	if lock {
		lockedRow, lockErr := queries.FindInvitationByTokenForUpdate(ctx, digest)
		row = identitydb.FindInvitationByTokenRow(lockedRow)
		err = lockErr
	} else {
		row, err = queries.FindInvitationByToken(ctx, digest)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Invitation{}, identity.ErrNotFound
	}
	return invitationFromColumns(invitationColumns{
		id: row.ID, householdID: row.HouseholdID, creatorID: row.CreatedByUserID,
		createdAt: row.CreatedAt, expiresAt: row.ExpiresAt,
		acceptedAt: row.AcceptedAt, revokedAt: row.RevokedAt,
	}), err
}

type invitationColumns struct {
	id, householdID, creatorID                  string
	createdAt, expiresAt, acceptedAt, revokedAt pgtype.Timestamptz
}

func invitationFromColumns(columns invitationColumns) identity.Invitation {
	invitation := identity.Invitation{
		ID: columns.id, HouseholdID: columns.householdID, CreatedByUserID: columns.creatorID,
		CreatedAt: columns.createdAt.Time.UTC(), ExpiresAt: columns.expiresAt.Time.UTC(),
	}
	if columns.acceptedAt.Valid {
		acceptedAt := columns.acceptedAt.Time.UTC()
		invitation.AcceptedAt = &acceptedAt
	}
	if columns.revokedAt.Valid {
		revokedAt := columns.revokedAt.Time.UTC()
		invitation.RevokedAt = &revokedAt
	}
	return invitation
}

func ts(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
