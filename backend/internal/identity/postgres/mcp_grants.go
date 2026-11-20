package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
)

var errInvalidMCPRevocation = errors.New("invalid MCP revocation")

// revokeAccountMCPAccess revokes every live MCP grant of the account with an
// audited cause and deletes every authorization code and every access and
// refresh token of the account (ADR 0023). The caller already holds the
// account-side locks, so the statements run in the ADR 0023 order: codes, then
// grants, then token rows. A revoked grant keeps no secret.
func revokeAccountMCPAccess(ctx context.Context, q *identitydb.Queries, user pgtype.UUID, now time.Time, revocation identity.MCPRevocation) error {
	if !revocation.Reason.Valid() {
		return errInvalidMCPRevocation
	}
	var actor pgtype.UUID
	if revocation.ActorUserID != "" {
		if err := actor.Scan(revocation.ActorUserID); err != nil {
			return errInvalidMCPRevocation
		}
	}
	if err := q.DeleteAccountMCPCodes(ctx, user); err != nil {
		return err
	}
	if _, err := q.RevokeAccountMCPGrants(ctx, identitydb.RevokeAccountMCPGrantsParams{
		UserID:       user,
		RevokedAt:    ts(now.UTC()),
		RevokeReason: pgtype.Text{String: string(revocation.Reason), Valid: true},
		ActorUserID:  actor,
	}); err != nil {
		return err
	}
	if err := q.DeleteAccountMCPAccessTokens(ctx, user); err != nil {
		return err
	}
	return q.DeleteAccountMCPRefreshTokens(ctx, user)
}
