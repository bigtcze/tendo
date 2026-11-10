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

// PasswordResetHouseholdService is the transaction-scoped slice of the
// household membership service the reset flow needs. Authorization always runs
// through the shared RequireAdmin lock protocol.
type PasswordResetHouseholdService interface {
	RequireAdmin(context.Context, string, string) error
	GetMembership(context.Context, string, string) (string, error)
}
type PasswordResetHouseholdServiceFactory func(householddb.DBTX) PasswordResetHouseholdService

type PasswordResetRepository struct {
	pool interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
		Query(context.Context, string, ...any) (pgx.Rows, error)
		QueryRow(context.Context, string, ...any) pgx.Row
	}
	factory PasswordResetHouseholdServiceFactory
	clock   func() time.Time
}

var _ household.PasswordResetLookup = (*PasswordResetRepository)(nil)

func NewPasswordResetRepository(pool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}, factory PasswordResetHouseholdServiceFactory, clock ...func() time.Time) *PasswordResetRepository {
	now := time.Now
	if len(clock) > 0 && clock[0] != nil {
		now = clock[0]
	}
	return &PasswordResetRepository{pool: pool, factory: factory, clock: now}
}

func (r *PasswordResetRepository) WithPasswordResetTransaction(ctx context.Context, work func(identity.PasswordResetTransaction) error) error {
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
		return errors.New("password reset transaction unavailable")
	}
	if err = work(&passwordResetTx{q: identitydb.New(tx), h: r.factory(tx)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *PasswordResetRepository) FindPasswordReset(ctx context.Context, digest []byte) (identity.PasswordResetRecord, error) {
	return findPasswordReset(ctx, identitydb.New(r.pool), digest, false)
}

// PasswordResetsByUserIDs returns the usable outstanding reset facts for the
// Admin center. A user ID missing from the map means "no outstanding reset":
// only rows that exist and are usable are returned.
func (r *PasswordResetRepository) PasswordResetsByUserIDs(ctx context.Context, householdID string, userIDs []string) (map[string]household.PasswordReset, error) {
	var house pgtype.UUID
	if err := house.Scan(householdID); err != nil {
		return nil, err
	}
	uuids := make([]pgtype.UUID, 0, len(userIDs))
	for _, id := range userIDs {
		var user pgtype.UUID
		if err := user.Scan(id); err != nil {
			return nil, err
		}
		uuids = append(uuids, user)
	}
	rows, err := identitydb.New(r.pool).OutstandingPasswordResetsByUserIDs(ctx, identitydb.OutstandingPasswordResetsByUserIDsParams{
		HouseholdID: house, Column2: uuids, ExpiresAt: timestamptz(r.clock().UTC()),
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]household.PasswordReset, len(rows))
	for _, row := range rows {
		out[row.UserID] = household.PasswordReset{CreatedAt: row.CreatedAt.Time.UTC(), ExpiresAt: row.ExpiresAt.Time.UTC()}
	}
	return out, nil
}

type passwordResetTx struct {
	q *identitydb.Queries
	h PasswordResetHouseholdService
}

func (t *passwordResetTx) RequireAdmin(ctx context.Context, userID, householdID string) error {
	return t.h.RequireAdmin(ctx, userID, householdID)
}
func (t *passwordResetTx) GetMembership(ctx context.Context, userID, householdID string) (string, error) {
	return t.h.GetMembership(ctx, userID, householdID)
}

func (t *passwordResetTx) FindUserLogin(ctx context.Context, userValue string) (string, error) {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return "", identity.ErrNotFound
	}
	login, err := t.q.FindUserLogin(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrNotFound
	}
	return login, err
}

func (t *passwordResetTx) LockPasswordResetTarget(ctx context.Context, userValue string) (string, error) {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return "", identity.ErrNotFound
	}
	row, err := t.q.LockPasswordResetTarget(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrNotFound
	}
	return row.Login, err
}

func (t *passwordResetTx) HasLocalCredential(ctx context.Context, userValue string) (bool, error) {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return false, identity.ErrNotFound
	}
	return t.q.HasLocalCredential(ctx, user)
}

func (t *passwordResetTx) RevokeOutstandingPasswordResetsForTarget(ctx context.Context, userValue string, now time.Time) error {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return identity.ErrNotFound
	}
	return ignoreRows(t.q.RevokeOutstandingPasswordResetsForTarget(ctx, identitydb.RevokeOutstandingPasswordResetsForTargetParams{TargetUserID: user, RevokedAt: ts(now)}))
}

func (t *passwordResetTx) RevokeOutstandingPasswordResetsInHousehold(ctx context.Context, householdValue, userValue string, now time.Time) error {
	var user, house pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return identity.ErrNotFound
	}
	if err := house.Scan(householdValue); err != nil {
		return household.ErrNotFound
	}
	return ignoreRows(t.q.RevokeOutstandingPasswordResetsInHousehold(ctx, identitydb.RevokeOutstandingPasswordResetsInHouseholdParams{
		HouseholdID: house, TargetUserID: user, RevokedAt: ts(now),
	}))
}

func ignoreRows(_ int64, err error) error { return err }

func (t *passwordResetTx) CreatePasswordReset(ctx context.Context, input identity.CreatePasswordResetInput) (identity.PasswordResetRecord, error) {
	var house, target, actor pgtype.UUID
	if err := house.Scan(input.HouseholdID); err != nil {
		return identity.PasswordResetRecord{}, household.ErrNotFound
	}
	if err := target.Scan(input.TargetUserID); err != nil {
		return identity.PasswordResetRecord{}, identity.ErrNotFound
	}
	if err := actor.Scan(input.ActorID); err != nil {
		return identity.PasswordResetRecord{}, identity.ErrNotFound
	}
	row, err := t.q.CreatePasswordReset(ctx, identitydb.CreatePasswordResetParams{
		HouseholdID: house, TargetUserID: target, CreatedByUserID: actor, TokenHash: input.TokenHash,
		CreatedAt: ts(input.CreatedAt), ExpiresAt: ts(input.ExpiresAt),
	})
	if err != nil {
		return identity.PasswordResetRecord{}, err
	}
	return passwordResetFromColumns(row.ID, row.HouseholdID, row.TargetUserID, row.CreatedByUserID, row.CreatedAt, row.ExpiresAt, row.UsedAt, row.RevokedAt), nil
}

func (t *passwordResetTx) FindPasswordReset(ctx context.Context, digest []byte, lock bool) (identity.PasswordResetRecord, error) {
	return findPasswordReset(ctx, t.q, digest, lock)
}

func (t *passwordResetTx) CompletePasswordReset(ctx context.Context, id string, now time.Time) (int64, error) {
	var resetID pgtype.UUID
	if err := resetID.Scan(id); err != nil {
		return 0, identity.ErrNotFound
	}
	return t.q.CompletePasswordReset(ctx, identitydb.CompletePasswordResetParams{ID: resetID, UsedAt: ts(now)})
}

func (t *passwordResetTx) LockLocalCredential(ctx context.Context, userValue string) (int64, error) {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return 0, identity.ErrNotFound
	}
	version, err := t.q.LockLocalCredential(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, identity.ErrNotFound
	}
	return version, err
}

func (t *passwordResetTx) UpdateLocalCredential(ctx context.Context, userValue, passwordHash string) (int64, error) {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return 0, identity.ErrNotFound
	}
	return t.q.UpdateLocalCredential(ctx, identitydb.UpdateLocalCredentialParams{UserID: user, PasswordHash: passwordHash})
}

func (t *passwordResetTx) DeleteUserSessions(ctx context.Context, userValue string) error {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return identity.ErrNotFound
	}
	return t.q.DeleteUserSessions(ctx, user)
}

func findPasswordReset(ctx context.Context, queries *identitydb.Queries, digest []byte, lock bool) (identity.PasswordResetRecord, error) {
	var row identitydb.FindPasswordResetByTokenRow
	var err error
	if lock {
		locked, lockErr := queries.FindPasswordResetByTokenForUpdate(ctx, digest)
		row = identitydb.FindPasswordResetByTokenRow(locked)
		err = lockErr
	} else {
		row, err = queries.FindPasswordResetByToken(ctx, digest)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.PasswordResetRecord{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.PasswordResetRecord{}, err
	}
	return passwordResetFromColumns(row.ID, row.HouseholdID, row.TargetUserID, row.CreatedByUserID, row.CreatedAt, row.ExpiresAt, row.UsedAt, row.RevokedAt), nil
}

func passwordResetFromColumns(id, householdID, targetID, creatorID string, createdAt, expiresAt, usedAt, revokedAt pgtype.Timestamptz) identity.PasswordResetRecord {
	record := identity.PasswordResetRecord{
		ID: id, HouseholdID: householdID, TargetUserID: targetID, CreatedByUserID: creatorID,
		CreatedAt: createdAt.Time.UTC(), ExpiresAt: expiresAt.Time.UTC(),
	}
	if usedAt.Valid {
		value := usedAt.Time.UTC()
		record.UsedAt = &value
	}
	if revokedAt.Valid {
		value := revokedAt.Time.UTC()
		record.RevokedAt = &value
	}
	return record
}
