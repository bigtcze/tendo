package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// PasswordChangeRepository implements every local-credential replacement path:
// the self-service change, admin reset redemption (through passwordResetTx) and
// the operator CLI reset. They share credentialTx so "the password changed, sign
// the account out everywhere" cannot drift between flows (ADR 0018).
type PasswordChangeRepository struct {
	pool interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
		Query(context.Context, string, ...any) (pgx.Rows, error)
		QueryRow(context.Context, string, ...any) pgx.Row
	}
}

var (
	_ identity.PasswordChangeRepository = (*PasswordChangeRepository)(nil)
	_ identity.AdminPasswordRepository  = (*PasswordChangeRepository)(nil)
)

func NewPasswordChangeRepository(pool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}) *PasswordChangeRepository {
	return &PasswordChangeRepository{pool: pool}
}

func (r *PasswordChangeRepository) ReadLocalCredential(ctx context.Context, userValue string) (identity.LocalCredential, error) {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return identity.LocalCredential{}, identity.ErrNotFound
	}
	row, err := identitydb.New(r.pool).ReadLocalCredential(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.LocalCredential{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.LocalCredential{}, err
	}
	return identity.LocalCredential{PasswordHash: row.PasswordHash, Version: row.CredentialVersion}, nil
}

func (r *PasswordChangeRepository) FindUserIDByLogin(ctx context.Context, login string) (string, error) {
	userID, err := identitydb.New(r.pool).FindUserAccountByLogin(ctx, login)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrNotFound
	}
	return userID, err
}

func (r *PasswordChangeRepository) WithPasswordChangeTransaction(ctx context.Context, work func(identity.PasswordChangeTransaction) error) error {
	return runCredentialTransaction(ctx, r.pool, func(tx *credentialTx) error {
		return work(&passwordChangeTx{credentialTx: tx})
	})
}

func (r *PasswordChangeRepository) WithCredentialReplacement(ctx context.Context, work func(identity.CredentialReplacementTransaction) error) error {
	return runCredentialTransaction(ctx, r.pool, func(tx *credentialTx) error { return work(tx) })
}

// runCredentialTransaction opens the shared transaction, rolls it back on any
// return, and commits only a nil error.
func runCredentialTransaction(ctx context.Context, pool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, work func(*credentialTx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	if err = work(&credentialTx{q: identitydb.New(tx)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// credentialTx is the single implementation of the shared replacement
// protocol. Callers must invoke the methods in the documented order.
type credentialTx struct {
	q *identitydb.Queries
}

func (t *credentialTx) RevokeOutstandingPasswordResets(ctx context.Context, userValue string, now time.Time) error {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return identity.ErrNotFound
	}
	return ignoreRows(t.q.RevokeOutstandingPasswordResetsForTarget(ctx, identitydb.RevokeOutstandingPasswordResetsForTargetParams{TargetUserID: user, RevokedAt: ts(now)}))
}

func (t *credentialTx) LockLocalCredential(ctx context.Context, userValue string) (identity.LocalCredential, error) {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return identity.LocalCredential{}, identity.ErrNotFound
	}
	row, err := t.q.LockLocalCredential(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.LocalCredential{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.LocalCredential{}, err
	}
	return identity.LocalCredential{PasswordHash: row.PasswordHash, Version: row.CredentialVersion}, nil
}

func (t *credentialTx) ReplaceLocalPassword(ctx context.Context, userValue, passwordHash string, now time.Time) (identity.Revocation, error) {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return identity.Revocation{}, identity.ErrNotFound
	}
	rows, err := t.q.ReplaceLocalPasswordHash(ctx, identitydb.ReplaceLocalPasswordHashParams{UserID: user, PasswordHash: passwordHash})
	if err != nil {
		return identity.Revocation{}, err
	}
	if rows != 1 {
		return identity.Revocation{}, identity.ErrNotFound
	}
	revoked, err := t.q.DeleteAllUserSessions(ctx, user)
	if err != nil {
		return identity.Revocation{}, err
	}
	live := make([][]byte, 0, len(revoked))
	for _, row := range revoked {
		if row.ExpiresAt.Time.After(now) {
			live = append(live, row.TokenHash)
		}
	}
	return identity.NewRevocation(live), nil
}

func (t *credentialTx) ActiveSessionExists(ctx context.Context, userValue string, tokenHash []byte, now time.Time) (bool, error) {
	var user pgtype.UUID
	if err := user.Scan(userValue); err != nil {
		return false, nil
	}
	return t.q.UserSessionActive(ctx, identitydb.UserSessionActiveParams{UserID: user, TokenHash: tokenHash, ExpiresAt: ts(now)})
}

func (t *credentialTx) CreateSession(ctx context.Context, s identity.NewSession) (string, error) {
	method, err := signInMethodParam(s.Method)
	if err != nil {
		return "", err
	}
	var user pgtype.UUID
	if err := user.Scan(s.UserID); err != nil {
		return "", err
	}
	return t.q.CreateSession(ctx, identitydb.CreateSessionParams{
		UserID: user, TokenHash: s.TokenHash, CreatedAt: ts(s.CreatedAt), ExpiresAt: ts(s.ExpiresAt), SignInMethod: method,
	})
}

// passwordChangeTx adds the caller's own session operations to the shared
// credential replacement protocol.
type passwordChangeTx struct {
	*credentialTx
}

// signInMethodParam validates the session kind before any write. A zero value
// is a programming fault and must never reach the database, where the column
// has no default and only accepts the known kinds.
func signInMethodParam(method identity.SignInMethod) (string, error) {
	if !method.Valid() {
		return "", identity.ErrInvalidSignInMethod
	}
	return string(method), nil
}
