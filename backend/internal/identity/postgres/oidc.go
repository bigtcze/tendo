package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	db "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var _ identity.OIDCRepository = (*Repository)(nil)

func rollbackOIDCTx(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func uuidOrNull(value string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if value == "" {
		return u, nil
	}
	return u, u.Scan(value)
}
func (r *Repository) InsertFlow(ctx context.Context, f identity.OIDCFlow) error {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	var user, session pgtype.UUID
	var err error
	if user, err = uuidOrNull(f.UserID); err != nil {
		return err
	}
	if session, err = uuidOrNull(f.SessionID); err != nil {
		return err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer rollbackOIDCTx(tx)
	q := db.New(tx)
	if err = q.PruneOIDCFlows(ctx, timestamptz(f.CreatedAt)); err != nil {
		return err
	}
	if len(f.PreviousBrowserTokenHash) == 32 {
		if err = q.DeleteOIDCFlowForPreviousBrowser(ctx, f.PreviousBrowserTokenHash); err != nil {
			return err
		}
	}

	if err = q.InsertOIDCFlow(ctx, db.InsertOIDCFlowParams{StateHash: f.StateHash, BrowserTokenHash: f.BrowserTokenHash, Issuer: f.Issuer, ClientID: f.ClientID, Nonce: f.Nonce, PkceVerifier: f.PKCEVerifier, Purpose: string(f.Purpose), UserID: user, SessionID: session, CreatedAt: timestamptz(f.CreatedAt), ExpiresAt: timestamptz(f.ExpiresAt)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (r *Repository) ConsumeFlow(ctx context.Context, state, browser []byte, now time.Time, issuer, client string) (identity.OIDCFlow, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	row, err := db.New(r.pool).ConsumeOIDCFlow(ctx, db.ConsumeOIDCFlowParams{StateHash: state, BrowserTokenHash: browser, ExpiresAt: timestamptz(now), Issuer: issuer, ClientID: client})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.OIDCFlow{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.OIDCFlow{}, err
	}
	return identity.OIDCFlow{Issuer: row.Issuer, ClientID: row.ClientID, Nonce: row.Nonce, PKCEVerifier: row.PkceVerifier, Purpose: identity.OIDCPurpose(row.Purpose), UserID: row.UserID, SessionID: row.SessionID, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time}, nil
}
func (r *Repository) FindIdentity(ctx context.Context, issuer, subject string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	id, err := db.New(r.pool).FindOIDCIdentity(ctx, db.FindOIDCIdentityParams{Issuer: issuer, Subject: subject})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrNotFound
	}
	return id, err
}
func (r *Repository) FindCredential(ctx context.Context, user string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	var id pgtype.UUID
	if err := id.Scan(user); err != nil {
		return "", err
	}
	v, err := db.New(r.pool).FindOIDCCredential(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrNotFound
	}
	return v, err
}
func (r *Repository) Linked(ctx context.Context, user, issuer string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	var id pgtype.UUID
	if err := id.Scan(user); err != nil {
		return false, err
	}
	return db.New(r.pool).LinkedOIDCIdentity(ctx, db.LinkedOIDCIdentityParams{UserID: id, Issuer: issuer})
}
func (r *Repository) FindSession(ctx context.Context, tokenHash []byte, now time.Time) (identity.SessionInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	var info identity.SessionInfo
	var expires pgtype.Timestamptz
	row, err := db.New(r.pool).FindActiveSession(ctx, db.FindActiveSessionParams{TokenHash: tokenHash, ExpiresAt: timestamptz(now)})
	if err == nil {
		info.ID = row.ID
		info.Principal.UserID = row.UserID
		info.Principal.Login = row.Login
		info.Principal.DefaultHouseholdID = row.DefaultHouseholdID
		expires = row.ExpiresAt
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.SessionInfo{}, identity.ErrNotFound
	}
	if err != nil {
		return identity.SessionInfo{}, err
	}
	info.ExpiresAt = expires.Time.UTC()
	return info, nil
}
func (r *Repository) CreateOIDCLoginSession(ctx context.Context, s identity.NewSession) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	var id pgtype.UUID
	if err := id.Scan(s.UserID); err != nil {
		return "", err
	}
	return db.New(r.pool).CreateSession(ctx, db.CreateSessionParams{UserID: id, TokenHash: s.TokenHash, CreatedAt: timestamptz(s.CreatedAt), ExpiresAt: timestamptz(s.ExpiresAt)})
}
func (r *Repository) LinkAndCreateSession(ctx context.Context, f identity.OIDCFlow, v identity.OIDCVerifiedIdentity, s identity.NewSession, revoke []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	var user, session pgtype.UUID
	if err := user.Scan(f.UserID); err != nil {
		return "", err
	}
	if err := session.Scan(f.SessionID); err != nil {
		return "", err
	}
	newUser, err := uuidOrNull(s.UserID)
	if err != nil {
		return "", err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer rollbackOIDCTx(tx)
	// Revoke the initiating session first: if it was logged out or expired while the
	// browser was at the provider, nothing is linked and no session is issued.
	revoked, err := tx.Exec(ctx, `DELETE FROM user_sessions WHERE id=$1 AND user_id=$2 AND expires_at > $3`, session, user, timestamptz(s.CreatedAt))
	if err != nil {
		return "", err
	}
	if revoked.RowsAffected() != 1 {
		return "", identity.ErrOIDCSessionChanged
	}
	// The (issuer, subject) primary key and the (user_id, issuer) unique key decide
	// conflicts atomically, including concurrent links. Re-linking the same identity
	// to the same account is an idempotent no-op.
	var linkedUser string
	err = tx.QueryRow(ctx, `SELECT user_id::text FROM oidc_identities WHERE issuer=$1 AND subject=$2`, v.Issuer, v.Subject).Scan(&linkedUser)
	switch {
	case err == nil && linkedUser != f.UserID:
		return "", identity.ErrOIDCConflict
	case errors.Is(err, pgx.ErrNoRows):
		if _, err = tx.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES($1,$2,$3,$4)`, v.Issuer, v.Subject, user, timestamptz(s.CreatedAt)); err != nil {
			if isOIDCUniqueViolation(err) {
				return "", identity.ErrOIDCConflict
			}
			return "", err
		}
	case err != nil:
		return "", err
	}
	id, err := db.New(tx).CreateSession(ctx, db.CreateSessionParams{UserID: newUser, TokenHash: s.TokenHash, CreatedAt: timestamptz(s.CreatedAt), ExpiresAt: timestamptz(s.ExpiresAt)})
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}
func isOIDCUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && (pg.ConstraintName == "oidc_identities_pkey" || pg.ConstraintName == "oidc_identities_user_id_issuer_key")
}
