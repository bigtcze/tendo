package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// pendingRow is one stored continuation, re-read inside the decision
// transaction. The handle itself is never stored.
type pendingRow struct {
	Issuer, Subject, ClientID string
	Email                     string
	EmailVerified             bool
	InviteTokenHash           []byte
	CreatedAt, ExpiresAt      time.Time
	Attempts                  int
}

func (r *Repository) ReadPendingLogin(ctx context.Context, handleHash []byte, now time.Time, issuer, client string) (identity.OIDCPendingStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	var status identity.OIDCPendingStatus
	err := r.pool.QueryRow(ctx, `SELECT expires_at, email_verified, invite_token_hash IS NOT NULL
		FROM oidc_pending_logins WHERE handle_hash=$1 AND expires_at>$2 AND issuer=$3 AND client_id=$4`,
		handleHash, timestamptz(now), issuer, client).Scan(&status.ExpiresAt, &status.EmailVerified, &status.Invitation)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.OIDCPendingStatus{}, identity.ErrOIDCPendingNotFound
	}
	if err != nil {
		return identity.OIDCPendingStatus{}, err
	}
	return status, nil
}

// consumePendingTx deletes and returns the pending row in the caller's
// transaction. A second use of the same handle finds nothing.
func consumePendingTx(ctx context.Context, tx pgx.Tx, handleHash []byte, now time.Time, issuer, client string) (pendingRow, error) {
	var row pendingRow
	var email pgtype.Text
	err := tx.QueryRow(ctx, `DELETE FROM oidc_pending_logins
		WHERE handle_hash=$1 AND expires_at>$2 AND issuer=$3 AND client_id=$4
		RETURNING issuer,subject,email,email_verified,client_id,invite_token_hash,created_at,expires_at,attempts`,
		handleHash, timestamptz(now), issuer, client).
		Scan(&row.Issuer, &row.Subject, &email, &row.EmailVerified, &row.ClientID, &row.InviteTokenHash, &row.CreatedAt, &row.ExpiresAt, &row.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return pendingRow{}, identity.ErrOIDCPendingNotFound
	}
	if err != nil {
		return pendingRow{}, err
	}
	if email.Valid {
		row.Email = email.String
	}
	return row, nil
}

func (r *Repository) CreatePendingAccount(ctx context.Context, params identity.OIDCPendingAccountParams) (identity.OIDCPendingAccountOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	}
	defer rollbackOIDCTx(tx)
	pending, err := consumePendingTx(ctx, tx, params.HandleHash, params.Now, params.Issuer, params.ClientID)
	if err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	}
	if err = acquireIdentityLock(ctx, tx, pending.Issuer, pending.Subject); err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	}
	if _, linked, err := findIdentityTx(ctx, tx, pending.Issuer, pending.Subject); err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	} else if linked {
		// Row 1 now wins for this subject. The continuation must not survive a
		// conflict, otherwise a client retry would loop on the same decision.
		return identity.OIDCPendingAccountOutcome{}, commitAccountConflict(ctx, tx)
	}
	userID, conflict, err := createJITAccount(ctx, tx, pending, params)
	if err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	}
	if conflict {
		return identity.OIDCPendingAccountOutcome{}, commitAccountConflict(ctx, tx)
	}
	emailSource := "oidc_claim"
	if err = r.recordPendingEmail(ctx, tx, userID, pending, params.Now, emailSource); err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	}
	invitation, err := acceptInvitationFor(ctx, tx, r, userID, pending.InviteTokenHash, params.Now)
	if err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	}
	if err = createSessionTx(ctx, tx, params.Session, userID); err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	}
	login, defaultHousehold, instanceAdmin, err := readLoginPrincipal(ctx, tx, userID)
	if err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return identity.OIDCPendingAccountOutcome{}, err
	}
	return identity.OIDCPendingAccountOutcome{UserID: userID, Login: login, DefaultHouseholdID: defaultHousehold, InstanceAdmin: instanceAdmin, Invitation: invitation}, nil
}

// createJITAccount creates the account and its identity under a savepoint. A
// unique identity conflict reports conflict=true after undoing only the
// half-created account, so the caller can commit the consumed continuation.
func createJITAccount(ctx context.Context, tx pgx.Tx, pending pendingRow, params identity.OIDCPendingAccountParams) (string, bool, error) {
	if _, err := tx.Exec(ctx, `SAVEPOINT oidc_account_create`); err != nil {
		return "", false, err
	}
	userID, err := createOIDCUser(ctx, tx, params.Login)
	if err != nil {
		return "", false, err
	}
	if err = insertIdentityTx(ctx, tx, pending.Issuer, pending.Subject, userID, params.Now); err != nil {
		if !isOIDCUniqueViolation(err) {
			return "", false, err
		}
		if _, rollbackErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT oidc_account_create`); rollbackErr != nil {
			return "", false, rollbackErr
		}
		return "", true, nil
	}
	if _, err = tx.Exec(ctx, `RELEASE SAVEPOINT oidc_account_create`); err != nil {
		return "", false, err
	}
	return userID, false, nil
}

func commitAccountConflict(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return identity.ErrOIDCConflict
}

func createOIDCUser(ctx context.Context, tx pgx.Tx, login string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, login).Scan(&id)
	if isLoginUniqueViolation(err) {
		return "", identity.ErrLoginUnavailable
	}
	return id, err
}

func isLoginUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "user_accounts_login_key"
}

// recordPendingEmail stores the pending address only if verified, still unowned
// and the account has none yet. It never overwrites (row 1, row 7e, row 11).
func (r *Repository) recordPendingEmail(ctx context.Context, tx pgx.Tx, userID string, pending pendingRow, now time.Time, source string) error {
	if !pending.EmailVerified || pending.Email == "" {
		return nil
	}
	_, err := r.claimEmail(ctx, tx, userID, pending.Email, source, pending.Issuer, now)
	return err
}

func readLoginPrincipal(ctx context.Context, tx pgx.Tx, userID string) (login, defaultHousehold string, instanceAdmin bool, err error) {
	id, err := uuidOrNull(userID)
	if err != nil {
		return "", "", false, err
	}
	err = tx.QueryRow(ctx, `SELECT login, COALESCE(default_household_id::text,''),
		EXISTS(SELECT 1 FROM instance_admins a WHERE a.user_id=user_accounts.id)
		FROM user_accounts WHERE id=$1`, id).Scan(&login, &defaultHousehold, &instanceAdmin)
	return login, defaultHousehold, instanceAdmin, err
}

func (r *Repository) LinkPendingAccount(ctx context.Context, params identity.OIDCPendingLinkParams) (identity.OIDCPendingLinkOutcome, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	defer rollbackOIDCTx(tx)
	pending, err := consumePendingTx(ctx, tx, params.HandleHash, params.Now, params.Issuer, params.ClientID)
	if err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	if err = acquireIdentityLock(ctx, tx, pending.Issuer, pending.Subject); err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	conflict, err := linkTargetConflict(ctx, tx, params, pending)
	if err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	if conflict {
		return commitLinkConflict(ctx, tx)
	}
	return r.bindPendingAccount(ctx, tx, params, pending)
}

// linkTargetConflict re-reads every fact the row 7 proof depends on. The
// credential row stays share-locked so a concurrent reset cannot let an
// old-password proof bind an identity.
func linkTargetConflict(ctx context.Context, tx pgx.Tx, params identity.OIDCPendingLinkParams, pending pendingRow) (bool, error) {
	var storedHash string
	err := tx.QueryRow(ctx, `SELECT password_hash FROM local_credentials WHERE user_id=$1 FOR SHARE`, params.UserID).Scan(&storedHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, identity.ErrInvalidCredentials
	}
	if err != nil {
		return false, err
	}
	if storedHash != params.VerifiedPasswordHash {
		return false, identity.ErrInvalidCredentials
	}
	if _, linked, err := findIdentityTx(ctx, tx, pending.Issuer, pending.Subject); err != nil {
		return false, err
	} else if linked {
		return true, nil
	}
	return accountUsesIssuer(ctx, tx, params.UserID, pending.Issuer)
}

// bindPendingAccount writes the verified binding, the account_confirmed email
// (row 3 only), the invitation and the session in the caller's transaction.
func (r *Repository) bindPendingAccount(ctx context.Context, tx pgx.Tx, params identity.OIDCPendingLinkParams, pending pendingRow) (identity.OIDCPendingLinkOutcome, error) {
	// Lock order (global): advisory lock, invitation row, user row. The
	// savepoint starts before the invitation acceptance so a later binding
	// conflict can undo the accepted invitation, membership and default
	// household together; the continuation deletion predates it and stays.
	if _, err := tx.Exec(ctx, `SAVEPOINT oidc_identity_bind`); err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	if r.beforeIdentityBind != nil {
		r.beforeIdentityBind()
	}
	invitation, err := acceptInvitationFor(ctx, tx, r, params.UserID, pending.InviteTokenHash, params.Now)
	if err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	if err := insertIdentityTx(ctx, tx, pending.Issuer, pending.Subject, params.UserID, params.Now); err != nil {
		if isOIDCUniqueViolation(err) {
			// A concurrent auto-attach claimed (user_id, issuer) first. Undo
			// every binding write (including the invitation acceptance) so the
			// consumed continuation can still be committed; row 7a writes
			// nothing else.
			if _, rollbackErr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT oidc_identity_bind`); rollbackErr != nil {
				return identity.OIDCPendingLinkOutcome{}, rollbackErr
			}
			return commitLinkConflict(ctx, tx)
		}
		return identity.OIDCPendingLinkOutcome{}, err
	}
	emailSaved, err := r.saveConfirmedEmail(ctx, tx, params.UserID, pending, params.Now)
	if err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	if err = createSessionTx(ctx, tx, params.Session, params.UserID); err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	login, defaultHousehold, instanceAdmin, err := readLoginPrincipal(ctx, tx, params.UserID)
	if err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	return identity.OIDCPendingLinkOutcome{
		UserID: params.UserID, Login: login, DefaultHouseholdID: defaultHousehold, InstanceAdmin: instanceAdmin,
		Invitation: invitation, EmailSaved: emailSaved,
	}, nil
}

// commitLinkConflict consumes the pending row (row 7a) but writes no binding.
func commitLinkConflict(ctx context.Context, tx pgx.Tx) (identity.OIDCPendingLinkOutcome, error) {
	if err := tx.Commit(ctx); err != nil {
		return identity.OIDCPendingLinkOutcome{}, err
	}
	return identity.OIDCPendingLinkOutcome{Conflict: true}, nil
}

// saveConfirmedEmail records row 7's account_confirmed claim. It reports false
// when the account already has an address or another account claimed it (7e).
func (r *Repository) saveConfirmedEmail(ctx context.Context, tx pgx.Tx, userID string, pending pendingRow, now time.Time) (bool, error) {
	if !pending.EmailVerified || pending.Email == "" {
		return false, nil
	}
	return r.claimEmail(ctx, tx, userID, pending.Email, "account_confirmed", pending.Issuer, now)
}

func (r *Repository) RecordPendingFailure(ctx context.Context, handleHash []byte, now time.Time) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	var attempts int
	err := r.pool.QueryRow(ctx, `UPDATE oidc_pending_logins SET attempts=attempts+1 WHERE handle_hash=$1 AND expires_at>$2 RETURNING attempts`, handleHash, timestamptz(now)).Scan(&attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if attempts < 5 {
		return false, nil
	}
	if _, err = r.pool.Exec(ctx, `DELETE FROM oidc_pending_logins WHERE handle_hash=$1`, handleHash); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) CancelPendingLogin(ctx context.Context, handleHash []byte) error {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	_, err := r.pool.Exec(ctx, `DELETE FROM oidc_pending_logins WHERE handle_hash=$1`, handleHash)
	return err
}

func (r *Repository) FindPendingInvitation(ctx context.Context, digest []byte, now time.Time) (bool, error) {
	var pending bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_invitations
		WHERE token_hash=$1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>$2)`, digest, timestamptz(now)).Scan(&pending)
	return pending, err
}

func (r *Repository) DiscardOIDCAccount(ctx context.Context, params identity.OIDCDiscardParams) (identity.OIDCPendingStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, identity.OIDCOperationBudget)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.OIDCPendingStatus{}, err
	}
	defer rollbackOIDCTx(tx)
	// Lock order (global): advisory lock, invitation row, user row. Read the
	// identity without locks only to learn the subject, then serialize on that
	// subject before locking the account and re-validating.
	subject, err := discardableSubject(ctx, tx, params.UserID, params.Issuer)
	if err != nil {
		return identity.OIDCPendingStatus{}, err
	}
	if err = acquireIdentityLock(ctx, tx, params.Issuer, subject); err != nil {
		return identity.OIDCPendingStatus{}, err
	}
	email, verified, err := lockDiscardableAccount(ctx, tx, params.UserID, params.Issuer, subject)
	if err != nil {
		return identity.OIDCPendingStatus{}, err
	}
	var emailValue any
	if verified {
		emailValue = email
	}
	if _, err = tx.Exec(ctx, `INSERT INTO oidc_pending_logins(handle_hash,issuer,subject,email,email_verified,client_id,created_at,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		params.HandleHash, params.Issuer, subject, emailValue, verified, params.ClientID,
		timestamptz(params.Now), timestamptz(params.ExpiresAt)); err != nil {
		return identity.OIDCPendingStatus{}, err
	}
	id, err := uuidOrNull(params.UserID)
	if err != nil {
		return identity.OIDCPendingStatus{}, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM user_accounts WHERE id=$1`, id)
	if err != nil {
		if isStillReferenced(err) {
			return identity.OIDCPendingStatus{}, identity.ErrOIDCAccountNotDiscardable
		}
		return identity.OIDCPendingStatus{}, err
	}
	if tag.RowsAffected() != 1 {
		return identity.OIDCPendingStatus{}, identity.ErrOIDCAccountNotDiscardable
	}
	if err = tx.Commit(ctx); err != nil {
		return identity.OIDCPendingStatus{}, err
	}
	return identity.OIDCPendingStatus{ExpiresAt: params.ExpiresAt, EmailVerified: verified, Invitation: false}, nil
}

// discardableSubject reads the single configured-issuer identity without locks;
// it only identifies the advisory-lock key.
func discardableSubject(ctx context.Context, tx pgx.Tx, userValue, issuer string) (string, error) {
	id, err := uuidOrNull(userValue)
	if err != nil {
		return "", identity.ErrOIDCAccountNotDiscardable
	}
	var count int
	var subject string
	if err = tx.QueryRow(ctx, `SELECT count(*), COALESCE(min(subject),'') FROM oidc_identities WHERE user_id=$1 AND issuer=$2`, id, issuer).Scan(&count, &subject); err != nil {
		return "", err
	}
	if count != 1 {
		return "", identity.ErrOIDCAccountNotDiscardable
	}
	return subject, nil
}

// lockDiscardableAccount re-validates row 8 under the account row lock: no
// household membership, no local credential and the same single identity.
func lockDiscardableAccount(ctx context.Context, tx pgx.Tx, userValue, issuer, wantedSubject string) (string, bool, error) {
	id, err := uuidOrNull(userValue)
	if err != nil {
		return "", false, identity.ErrOIDCAccountNotDiscardable
	}
	var email pgtype.Text
	if err = tx.QueryRow(ctx, `SELECT email FROM user_accounts WHERE id=$1 FOR UPDATE`, id).Scan(&email); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, identity.ErrOIDCAccountNotDiscardable
		}
		return "", false, err
	}
	var hasMembership, hasCredential bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM household_memberships WHERE user_id=$1),
		EXISTS(SELECT 1 FROM local_credentials WHERE user_id=$1)`, id).Scan(&hasMembership, &hasCredential); err != nil {
		return "", false, err
	}
	if hasMembership || hasCredential {
		return "", false, identity.ErrOIDCAccountNotDiscardable
	}
	var count int
	var subject string
	if err = tx.QueryRow(ctx, `SELECT count(*), COALESCE(min(subject),'') FROM oidc_identities WHERE user_id=$1 AND issuer=$2`, id, issuer).Scan(&count, &subject); err != nil {
		return "", false, err
	}
	if count != 1 || subject != wantedSubject {
		return "", false, identity.ErrOIDCAccountNotDiscardable
	}
	if !email.Valid {
		return "", false, nil
	}
	return email.String, true, nil
}

// isStillReferenced covers both the deleting-side FK failure (23503) and
// PostgreSQL 18's ON DELETE RESTRICT restrict_violation (23001). Both mean the
// account row is still referenced and must not be discarded.
func isStillReferenced(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && (pg.Code == "23503" || pg.Code == "23001")
}
