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
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return e
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
	if e = work(wrapped); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (r *InvitationRepository) FindInvitation(ctx context.Context, d []byte) (identity.Invitation, error) {
	return findInvitation(ctx, identitydb.New(r.pool), d, false)
}

type invitationTx struct {
	q *identitydb.Queries
	h InvitationHouseholdService
}

func (t *invitationTx) RequireOwner(c context.Context, u, h string) error {
	return t.h.RequireOwner(c, u, h)
}
func (t *invitationTx) GetMembership(c context.Context, u, h string) (string, error) {
	return t.h.GetMembership(c, u, h)
}
func (t *invitationTx) HasOtherMembership(c context.Context, u, h string) (bool, error) {
	return t.h.HasMembershipElsewhere(c, u, h)
}
func (t *invitationTx) CreateInvitation(c context.Context, in identity.CreateInvitationInput) (identity.Invitation, bool, error) {
	var h, u pgtype.UUID
	if e := h.Scan(in.HouseholdID); e != nil {
		return identity.Invitation{}, false, household.ErrNotFound
	}
	if e := u.Scan(in.ActorID); e != nil {
		return identity.Invitation{}, false, household.ErrNotFound
	}
	row, e := t.q.CreateInvitation(c, identitydb.CreateInvitationParams{HouseholdID: h, CreatedByUserID: u, CreationKey: in.CreationKey, TokenHash: in.TokenHash, CreatedAt: ts(in.CreatedAt), ExpiresAt: ts(in.ExpiresAt)})
	if errors.Is(e, pgx.ErrNoRows) {
		existing, x := t.q.FindInvitationByCreationKey(c, identitydb.FindInvitationByCreationKeyParams{HouseholdID: h, CreatedByUserID: u, CreationKey: in.CreationKey})
		return invite(existing.ID, existing.HouseholdID, existing.CreatedByUserID, existing.CreatedAt, existing.ExpiresAt, existing.AcceptedAt, existing.RevokedAt), false, x
	}
	return invite(row.ID, row.HouseholdID, row.CreatedByUserID, row.CreatedAt, row.ExpiresAt, row.AcceptedAt, row.RevokedAt), e == nil, e
}
func (t *invitationTx) ListInvitations(c context.Context, hid, cursor string, limit int) ([]identity.Invitation, string, error) {
	var h, after pgtype.UUID
	if e := h.Scan(hid); e != nil {
		return nil, "", household.ErrNotFound
	}
	if cursor != "" {
		if e := after.Scan(cursor); e != nil {
			return nil, "", &identity.ValidationError{Field: "cursor", Code: "invalid_format"}
		}
	}
	rows, e := t.q.ListInvitations(c, identitydb.ListInvitationsParams{HouseholdID: h, Column2: after, Limit: int32(limit)})
	if e != nil {
		return nil, "", e
	}
	out := make([]identity.Invitation, 0, len(rows))
	for _, row := range rows {
		out = append(out, invite(row.ID, row.HouseholdID, row.CreatedByUserID, row.CreatedAt, row.ExpiresAt, row.AcceptedAt, row.RevokedAt))
	}
	return out, "", nil
}
func (t *invitationTx) RevokeInvitation(c context.Context, hid, id string, now time.Time) error {
	var h, i pgtype.UUID
	if e := h.Scan(hid); e != nil {
		return household.ErrNotFound
	}
	if e := i.Scan(id); e != nil {
		return household.ErrNotFound
	}
	rows, e := t.q.RevokeInvitation(c, identitydb.RevokeInvitationParams{HouseholdID: h, ID: i, RevokedAt: ts(now)})
	if e != nil {
		return e
	}
	if rows > 0 {
		return nil
	}
	found, e := t.q.InvitationExistsInHousehold(c, identitydb.InvitationExistsInHouseholdParams{HouseholdID: h, ID: i})
	if e != nil {
		return e
	}
	if !found {
		return household.ErrNotFound
	}
	return nil
}
func (t *invitationTx) FindInvitation(c context.Context, d []byte, lock bool) (identity.Invitation, error) {
	return findInvitation(c, t.q, d, lock)
}
func (t *invitationTx) FindLogin(c context.Context, l string) error {
	_, e := t.q.FindLoginForInvitation(c, l)
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.ErrNotFound
	}
	return e
}
func (t *invitationTx) LockUser(c context.Context, id string) (string, error) {
	var uid pgtype.UUID
	if e := uid.Scan(id); e != nil {
		return "", identity.ErrNotFound
	}
	row, e := t.q.LockUserForInvitation(c, uid)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", identity.ErrNotFound
	}
	return row.DefaultHouseholdID, e
}

func (t *invitationTx) CreateUser(c context.Context, l string) (string, error) {
	id, e := t.q.CreateUser(c, l)
	var pe *pgconn.PgError
	if errors.As(e, &pe) && pe.Code == "23505" && pe.ConstraintName == "user_accounts_login_key" {
		return "", identity.ErrLoginUnavailable
	}
	return id, e
}
func (t *invitationTx) CreateCredential(c context.Context, u, h string) error {
	var id pgtype.UUID
	if e := id.Scan(u); e != nil {
		return e
	}
	return t.q.CreateLocalCredential(c, identitydb.CreateLocalCredentialParams{UserID: id, PasswordHash: h})
}
func (t *invitationTx) AddInvitedMember(c context.Context, u, h string) error {
	return t.h.AddInvitedMember(c, u, h)
}
func (t *invitationTx) SetDefaultHousehold(c context.Context, u, h string) error {
	var uid, hid pgtype.UUID
	if e := uid.Scan(u); e != nil {
		return e
	}
	if e := hid.Scan(h); e != nil {
		return e
	}
	return t.q.SetDefaultHousehold(c, identitydb.SetDefaultHouseholdParams{ID: uid, DefaultHouseholdID: hid})
}
func (t *invitationTx) AcceptInvitation(c context.Context, id string, now time.Time) error {
	var uid pgtype.UUID
	if e := uid.Scan(id); e != nil {
		return identity.ErrInvalidInvitation
	}
	rows, e := t.q.AcceptInvitation(c, identitydb.AcceptInvitationParams{ID: uid, AcceptedAt: ts(now)})
	if e != nil {
		return e
	}
	if rows != 1 {
		return identity.ErrInvalidInvitation
	}
	return nil
}
func findInvitation(c context.Context, q *identitydb.Queries, d []byte, lock bool) (identity.Invitation, error) {
	var row identitydb.FindInvitationByTokenRow
	var e error
	if lock {
		v, x := q.FindInvitationByTokenForUpdate(c, d)
		row = identitydb.FindInvitationByTokenRow(v)
		e = x
	} else {
		row, e = q.FindInvitationByToken(c, d)
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return identity.Invitation{}, identity.ErrNotFound
	}
	return invite(row.ID, row.HouseholdID, row.CreatedByUserID, row.CreatedAt, row.ExpiresAt, row.AcceptedAt, row.RevokedAt), e
}
func invite(id, h, creator string, created, expires, accepted, revoked pgtype.Timestamptz) identity.Invitation {
	i := identity.Invitation{ID: id, HouseholdID: h, CreatedByUserID: creator, CreatedAt: created.Time.UTC(), ExpiresAt: expires.Time.UTC()}
	if accepted.Valid {
		v := accepted.Time.UTC()
		i.AcceptedAt = &v
	}
	if revoked.Valid {
		v := revoked.Time.UTC()
		i.RevokedAt = &v
	}
	return i
}
func ts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }
