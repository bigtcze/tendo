package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// AccountEraser implements the identity-owned side of a global account
// deletion (ADR 0022). It is constructed with the household deletion
// transaction, so every identity write commits or rolls back with the
// membership removals.
type AccountEraser struct {
	db identitydb.DBTX
	q  *identitydb.Queries
}

func NewAccountEraser(db identitydb.DBTX) *AccountEraser {
	return &AccountEraser{db: db, q: identitydb.New(db)}
}

// FindDeletableAccount is the unlocked pre-read of the login. The account is
// only validated here; the row is locked later in the ADR 0019 order.
func (e *AccountEraser) FindDeletableAccount(ctx context.Context, userID string) (string, error) {
	var user pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return "", household.ErrNotFound
	}
	login, err := e.q.FindDeletableAccount(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", household.ErrNotFound
	}
	return login, err
}

// LockAccountIdentities takes the OIDC advisory lock of every identity linked to
// the account, in ascending key order. The key derivation and the lock mode are
// exactly the sign-in path's, so a concurrent sign-in of one of these identities
// either finishes first or observes the unlinked rows.
func (e *AccountEraser) LockAccountIdentities(ctx context.Context, userID string) error {
	var user pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return household.ErrNotFound
	}
	rows, err := e.q.ListAccountIdentityKeys(ctx, user)
	if err != nil {
		return err
	}
	keys := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		key := advisoryKey(row.Issuer, row.Subject)
		if _, duplicate := seen[key]; duplicate {
			// Two distinct pairs can hash to one key; the lock is idempotent
			// for the transaction, so taking it once is sufficient.
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if _, err = e.db.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return err
		}
	}
	return nil
}

// LockDeletableAccount locks the live account row FOR NO KEY UPDATE and returns
// its login. A row deleted by a concurrent transaction is not found.
func (e *AccountEraser) LockDeletableAccount(ctx context.Context, userID string) (string, error) {
	var user pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return "", household.ErrNotFound
	}
	login, err := e.q.LockDeletableAccount(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", household.ErrNotFound
	}
	return login, err
}

// RevokeAccountPasswordResets revokes every outstanding reset link the account
// is party to, as target or issuer, in any household.
func (e *AccountEraser) RevokeAccountPasswordResets(ctx context.Context, userID string, now time.Time) error {
	var user pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return household.ErrNotFound
	}
	return e.q.RevokeAccountPasswordResets(ctx, identitydb.RevokeAccountPasswordResetsParams{
		TargetUserID: user, RevokedAt: ts(now.UTC()),
	})
}

// EraseAccountAccess drops everything that could still authenticate the
// account, in the ADR 0018/0023 order: the local credential first, then the
// sessions, then every MCP code and token (each grant revoked with an audited
// account_deletion cause and the deleting administrator), then the link flows
// and the identity links.
func (e *AccountEraser) EraseAccountAccess(ctx context.Context, userID, actorID string, now time.Time) error {
	var user pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return household.ErrNotFound
	}
	revokeMCP := func(ctx context.Context, user pgtype.UUID) error {
		return revokeAccountMCPAccess(ctx, e.q, user, now, identity.MCPRevocation{
			Reason: identity.MCPRevokeAccountDeletion, ActorUserID: actorID,
		})
	}
	for _, erase := range []func(context.Context, pgtype.UUID) error{
		e.q.DeleteAccountCredentials,
		e.q.DeleteAccountSessions,
		revokeMCP,
		e.q.DeleteAccountOIDCFlows,
		e.q.DeleteAccountIdentities,
	} {
		if err := erase(ctx, user); err != nil {
			return err
		}
	}
	return nil
}

// AnonymizeAccount deletes sessions one final time (an OIDC sign-in may have
// committed one while the deletion held the account lock) and then writes the
// tombstone. Exactly one live row must change.
func (e *AccountEraser) AnonymizeAccount(ctx context.Context, userID string, now time.Time) error {
	var user pgtype.UUID
	if err := user.Scan(userID); err != nil {
		return household.ErrNotFound
	}
	if err := e.q.DeleteAccountSessions(ctx, user); err != nil {
		return err
	}
	updated, err := e.q.AnonymizeAccount(ctx, identitydb.AnonymizeAccountParams{ID: user, DeletedAt: ts(now.UTC())})
	if err != nil {
		return err
	}
	if updated != 1 {
		return fmt.Errorf("account %s was already deleted before its tombstone", userID)
	}
	return nil
}
