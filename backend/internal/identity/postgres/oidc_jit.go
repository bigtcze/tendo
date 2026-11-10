package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// This file owns the first-login decision table and the pending continuation.
// Each decision runs in exactly one transaction; the same subject is serialized
// with an advisory transaction lock and every fact is re-read after taking it.
// A unique violation rolls the whole table back and re-runs it once from a
// fresh read, as the design requires.

func (r *Repository) ResolveLogin(ctx context.Context, req identity.OIDCLoginRequest) (identity.OIDCLoginOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		outcome, err := r.resolveLoginOnce(ctx, req)
		if err == nil {
			return outcome, nil
		}
		if !isOIDCUniqueViolation(err) {
			return identity.OIDCLoginOutcome{}, err
		}
		lastErr = err
	}
	return identity.OIDCLoginOutcome{}, fmt.Errorf("%w: %v", identity.ErrOIDCConflict, lastErr)
}

func (r *Repository) resolveLoginOnce(ctx context.Context, req identity.OIDCLoginRequest) (identity.OIDCLoginOutcome, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	defer rollbackOIDCTx(tx)
	if err = acquireIdentityLock(ctx, tx, req.Verified.Issuer, req.Verified.Subject); err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	linkedUser, linked, err := findIdentityTx(ctx, tx, req.Verified.Issuer, req.Verified.Subject)
	if err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	if linked {
		return r.completeLinkedLogin(ctx, tx, linkedUser, req)
	}
	return r.startUnlinkedLogin(ctx, tx, req)
}

// completeLinkedLogin is row 1: an existing link always wins, and email never
// chooses the account.
func (r *Repository) completeLinkedLogin(ctx context.Context, tx pgx.Tx, userID string, req identity.OIDCLoginRequest) (identity.OIDCLoginOutcome, error) {
	// Lock order (global): advisory lock, invitation row, user row.
	invitation, err := r.acceptInvitation(ctx, tx, userID, req.InviteTokenHash, req.Now)
	if err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	if err = r.recordClaimedEmail(ctx, tx, userID, req.Verified, req.Now); err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	return r.signInUser(ctx, tx, userID, req, invitation)
}

// acceptInvitation applies the existing-account invitation rule inside the
// current transaction (invitation row, then user row).
func (r *Repository) acceptInvitation(ctx context.Context, tx pgx.Tx, userID string, inviteHash []byte, now time.Time) (identity.OIDCInvitationOutcome, error) {
	return acceptInvitationFor(ctx, tx, r, userID, inviteHash, now)
}

// signInUser records the already-resolved invitation outcome and issues the
// session.
func (r *Repository) signInUser(ctx context.Context, tx pgx.Tx, userID string, req identity.OIDCLoginRequest, invitation identity.OIDCInvitationOutcome) (identity.OIDCLoginOutcome, error) {
	if err := createSessionTx(ctx, tx, req.Session, userID); err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	return commitLogin(tx, ctx, identity.OIDCLoginOutcome{Resolution: identity.OIDCLoginSignedIn, Invitation: invitation})
}

func (r *Repository) startUnlinkedLogin(ctx context.Context, tx pgx.Tx, req identity.OIDCLoginRequest) (identity.OIDCLoginOutcome, error) {
	claim := ""
	if req.Verified.EmailVerified {
		claim = req.Verified.Email
	}
	owners, owner, err := emailOwners(ctx, tx, claim, req.Verified.Issuer)
	if err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	if owners > 1 {
		// Row 4 is impossible while the partial unique index holds; if it ever
		// happens, refuse instead of attaching to an arbitrary account.
		return identity.OIDCLoginOutcome{Resolution: identity.OIDCLoginConflict}, nil
	}
	if owners == 1 {
		return r.attachVerifiedOwner(ctx, tx, owner, req)
	}
	// Rows 3 and 5: never attach on email alone; store a continuation.
	if err = insertPendingTx(ctx, tx, req); err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	return commitLogin(tx, ctx, identity.OIDCLoginOutcome{Resolution: identity.OIDCLoginNeedsPending})
}

// attachVerifiedOwner is row 2, guarded by row 2c.
func (r *Repository) attachVerifiedOwner(ctx context.Context, tx pgx.Tx, owner string, req identity.OIDCLoginRequest) (identity.OIDCLoginOutcome, error) {
	hasIssuer, err := accountUsesIssuer(ctx, tx, owner, req.Verified.Issuer)
	if err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	if hasIssuer {
		return identity.OIDCLoginOutcome{Resolution: identity.OIDCLoginConflict}, nil
	}
	// Lock order: advisory lock, invitation row, user row; the invitation is
	// taken before the first user_accounts write of this branch.
	invitation, err := r.acceptInvitation(ctx, tx, owner, req.InviteTokenHash, req.Now)
	if err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	if err = insertIdentityTx(ctx, tx, req.Verified.Issuer, req.Verified.Subject, owner, req.Now); err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	return r.signInUser(ctx, tx, owner, req, invitation)
}

func commitLogin(tx pgx.Tx, ctx context.Context, outcome identity.OIDCLoginOutcome) (identity.OIDCLoginOutcome, error) {
	if err := tx.Commit(ctx); err != nil {
		return identity.OIDCLoginOutcome{}, err
	}
	return outcome, nil
}

func acquireIdentityLock(ctx context.Context, tx pgx.Tx, issuer, subject string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, advisoryKey(issuer, subject))
	return err
}

// advisoryKey derives a stable signed 64-bit key from the identity pair.
func advisoryKey(issuer, subject string) int64 {
	sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

func findIdentityTx(ctx context.Context, tx pgx.Tx, issuer, subject string) (string, bool, error) {
	var user string
	err := tx.QueryRow(ctx, `SELECT user_id::text FROM oidc_identities WHERE issuer=$1 AND subject=$2`, issuer, subject).Scan(&user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return user, true, nil
}

func accountUsesIssuer(ctx context.Context, tx pgx.Tx, userID, issuer string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM oidc_identities WHERE user_id=$1 AND issuer=$2)`, userID, issuer).Scan(&exists)
	return exists, err
}

// emailOwners returns the number of accounts holding the canonical address with
// a proof from the same issuer, and one owner when there is exactly one. An
// empty address owns nothing, and an address proved by a different issuer is
// treated as unowned (row 3).
func emailOwners(ctx context.Context, tx pgx.Tx, email, issuer string) (int, string, error) {
	var count int
	var owner pgtype.Text
	err := tx.QueryRow(ctx, `SELECT count(*), min(id::text) FROM user_accounts
		WHERE email=$1 AND email_issuer=$2 AND $1 <> ''`, email, issuer).Scan(&count, &owner)
	if err != nil {
		return 0, "", err
	}
	return count, owner.String, nil
}

func insertIdentityTx(ctx context.Context, tx pgx.Tx, issuer, subject, userID string, now time.Time) error {
	id, err := uuidOrNull(userID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES($1,$2,$3,$4)`, issuer, subject, id, timestamptz(now))
	return err
}

// claimEmail writes a verified address to an account that has none, but only
// while no other account owns it. A concurrent claim is skipped without
// aborting the surrounding transaction: the loser of that race reports false
// and keeps its session (rows 1, 7e, 10, 11).
func (r *Repository) claimEmail(ctx context.Context, tx pgx.Tx, userID, email, source, issuer string, now time.Time) (bool, error) {
	id, err := uuidOrNull(userID)
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `SAVEPOINT oidc_email_claim`); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE user_accounts SET email=$1, email_verified_at=$2, email_source=$3, email_issuer=$4
		WHERE id=$5 AND email IS NULL AND NOT EXISTS (SELECT 1 FROM user_accounts o WHERE o.email=$1)`,
		email, timestamptz(now), source, issuer, id)
	if isEmailUniqueViolation(err) {
		_, rollbackErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT oidc_email_claim`)
		return false, rollbackErr
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `RELEASE SAVEPOINT oidc_email_claim`); err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		if r.emailClaimPause != nil {
			r.emailClaimPause()
		}
		return true, nil
	}
	return false, nil
}

// recordClaimedEmail fills an empty account email from a verified provider
// claim. It never overwrites and silently skips an owned address (row 1).
func (r *Repository) recordClaimedEmail(ctx context.Context, tx pgx.Tx, userID string, verified identity.OIDCVerifiedIdentity, now time.Time) error {
	if !verified.EmailVerified || verified.Email == "" {
		return nil
	}
	_, err := r.claimEmail(ctx, tx, userID, verified.Email, "oidc_claim", verified.Issuer, now)
	return err
}

func createSessionTx(ctx context.Context, tx pgx.Tx, session identity.NewSession, userID string) error {
	id, err := uuidOrNull(userID)
	if err != nil {
		return err
	}
	var sessionID string
	err = tx.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,$2,$3,$4) RETURNING id::text`,
		id, session.TokenHash, timestamptz(session.CreatedAt), timestamptz(session.ExpiresAt)).Scan(&sessionID)
	return err
}

func insertPendingTx(ctx context.Context, tx pgx.Tx, req identity.OIDCLoginRequest) error {
	if err := identitydb.New(tx).PruneOIDCPendingLogins(ctx, timestamptz(req.Now)); err != nil {
		return err
	}
	var email any
	verified := req.Verified.EmailVerified && req.Verified.Email != ""
	if verified {
		email = req.Verified.Email
	}
	_, err := tx.Exec(ctx, `INSERT INTO oidc_pending_logins(handle_hash,issuer,subject,email,email_verified,client_id,invite_token_hash,created_at,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		req.PendingHandleHash, req.Verified.Issuer, req.Verified.Subject, email, verified,
		req.ClientID, nullableBytes(req.InviteTokenHash), timestamptz(req.Now), timestamptz(req.PendingExpiresAt))
	return err
}

func isEmailUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "user_accounts_email_unique"
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
