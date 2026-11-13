package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var _ identity.SessionRepository = (*Repository)(nil)
var _ household.LoginLookup = (*Repository)(nil)
var _ household.SignInMethodsLookup = (*Repository)(nil)

func timestamptz(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func (r *Repository) LoginsByUserIDs(ctx context.Context, ids []string) (map[string]string, error) {
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		var u pgtype.UUID
		if e := u.Scan(id); e != nil {
			return nil, e
		}
		uuids = append(uuids, u)
	}
	rows, e := identitydb.New(r.pool).MemberLogins(ctx, uuids)
	if e != nil {
		return nil, e
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.UserID] = row.Login
	}
	return out, nil
}

func (r *Repository) SignInMethodsByUserIDs(ctx context.Context, ids []string) (map[string]household.SignInMethods, error) {
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		var u pgtype.UUID
		if e := u.Scan(id); e != nil {
			return nil, e
		}
		uuids = append(uuids, u)
	}
	rows, e := identitydb.New(r.pool).SignInMethodsByUserIDs(ctx, uuids)
	if e != nil {
		return nil, e
	}
	out := make(map[string]household.SignInMethods, len(rows))
	for _, row := range rows {
		out[row.UserID] = household.SignInMethods{Password: row.Password, OIDC: row.Oidc}
	}
	return out, nil
}

func (r *Repository) FindLogin(ctx context.Context, login string) (identity.LoginRecord, error) {
	row, err := identitydb.New(r.pool).FindLogin(ctx, login)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.LoginRecord{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.LoginRecord{}, err
	}
	return identity.LoginRecord{Principal: identity.Principal{UserID: row.UserID, Login: row.Login, DefaultHouseholdID: row.DefaultHouseholdID}, PasswordHash: row.PasswordHash, CredentialVersion: row.CredentialVersion}, nil
}

func (r *Repository) PruneExpired(ctx context.Context, userID string, now time.Time) error {
	var id pgtype.UUID
	if err := id.Scan(userID); err != nil {
		return err
	}
	return identitydb.New(r.pool).DeleteExpiredSessions(ctx, identitydb.DeleteExpiredSessionsParams{UserID: id, ExpiresAt: timestamptz(now)})
}

func (r *Repository) CreateVerifiedSession(ctx context.Context, s identity.VerifiedSession) (string, error) {
	var user pgtype.UUID
	if err := user.Scan(s.Session.UserID); err != nil {
		return "", err
	}
	if r.beforeSessionInsert != nil {
		r.beforeSessionInsert()
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	queries := identitydb.New(tx)
	// The credential row stays share-locked until the session is committed, so
	// a concurrent password change either waits for this commit (and then
	// deletes the session) or makes the revalidation below fail.
	locked, err := queries.LockCredentialForLogin(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if locked.PasswordHash != s.PasswordHash || locked.CredentialVersion != s.CredentialVersion {
		return "", identity.ErrInvalidCredentials
	}
	id, err := queries.CreateSession(ctx, identitydb.CreateSessionParams{UserID: user, TokenHash: s.Session.TokenHash, CreatedAt: timestamptz(s.Session.CreatedAt), ExpiresAt: timestamptz(s.Session.ExpiresAt)})
	if err != nil {
		return "", err
	}
	if r.beforeSessionCommit != nil {
		r.beforeSessionCommit()
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (r *Repository) FindActive(ctx context.Context, tokenHash []byte, now time.Time) (identity.SessionInfo, error) {
	row, err := identitydb.New(r.pool).FindActiveSession(ctx, identitydb.FindActiveSessionParams{TokenHash: tokenHash, ExpiresAt: timestamptz(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.SessionInfo{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.SessionInfo{}, err
	}
	return identity.SessionInfo{ID: row.ID, ExpiresAt: row.ExpiresAt.Time.UTC(), Principal: identity.Principal{UserID: row.UserID, Login: row.Login, DefaultHouseholdID: row.DefaultHouseholdID}}, nil
}

func (r *Repository) DeleteByTokenHash(ctx context.Context, tokenHash []byte) error {
	return identitydb.New(r.pool).DeleteSession(ctx, tokenHash)
}

// SetDefaultHousehold selects an existing membership as the account-wide default
// in one UPDATE whose EXISTS clause is the only authorization. A zero-row update
// then distinguishes a vanished user (401) from a missing membership (422).
func (r *Repository) SetDefaultHousehold(ctx context.Context, userID, householdID string) error {
	var user, house pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return identity.ErrUnauthenticated
	}
	if err := house.Scan(householdID); err != nil {
		return identity.ErrInvalidReference
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	tag, err := tx.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2
		WHERE id=$1 AND EXISTS (SELECT 1 FROM household_memberships m WHERE m.user_id=$1 AND m.household_id=$2)`, user, house)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return tx.Commit(ctx)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_accounts WHERE id=$1)`, user).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return identity.ErrUnauthenticated
	}
	return identity.ErrInvalidReference
}
