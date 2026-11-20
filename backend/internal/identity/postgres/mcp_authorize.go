package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgconn"
)

// Slice A3a persistence (migration 030, ADR 0023): pending consents and the
// token-to-grant pre-read used by revocation.

// CreateAuthorizationRequest inserts a pending consent. A missing client is a
// foreign-key failure (23503): the client was deleted after the caller's
// lookup, so it is reported as an unknown client rather than a storage fault.
func (t *mcpTx) CreateAuthorizationRequest(ctx context.Context, request identity.MCPAuthorizationRequestRecord) error {
	err := t.q.CreateMCPAuthorizationRequest(ctx, identitydb.CreateMCPAuthorizationRequestParams{
		RequestHash: request.RequestHash, BindingHash: request.BindingHash, ClientID: request.ClientID,
		RedirectUri: request.RedirectURI, CodeChallenge: request.CodeChallenge, Scopes: request.Scopes,
		Resource: request.Resource, State: request.State, CreatedAt: ts(request.CreatedAt.UTC()), ExpiresAt: ts(request.ExpiresAt.UTC()),
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return identity.ErrMCPInvalidClient
	}
	return err
}

// FindAuthorizationRequest is the unlocked read of an unexpired pending consent.
func (t *mcpTx) FindAuthorizationRequest(ctx context.Context, requestHash, bindingHash []byte, now time.Time) (identity.MCPAuthorizationRequestRecord, error) {
	row, err := t.q.FindMCPAuthorizationRequest(ctx, identitydb.FindMCPAuthorizationRequestParams{
		RequestHash: requestHash, BindingHash: bindingHash, ExpiresAt: ts(now.UTC()),
	})
	if err != nil {
		return identity.MCPAuthorizationRequestRecord{}, notFound(err)
	}
	return identity.MCPAuthorizationRequestRecord{
		ClientID: row.ClientID, ClientName: row.ClientName, RedirectURI: row.RedirectUri,
		CodeChallenge: row.CodeChallenge, Scopes: row.Scopes, Resource: row.Resource, State: row.State.String,
		CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
	}, nil
}

// ConsumeAuthorizationRequest deletes and returns an unexpired pending consent.
// A wrong binding or an expired row matches nothing and deletes nothing.
func (t *mcpTx) ConsumeAuthorizationRequest(ctx context.Context, requestHash, bindingHash []byte, now time.Time) (identity.MCPAuthorizationRequestRecord, error) {
	row, err := t.q.ConsumeMCPAuthorizationRequest(ctx, identitydb.ConsumeMCPAuthorizationRequestParams{
		RequestHash: requestHash, BindingHash: bindingHash, ExpiresAt: ts(now.UTC()),
	})
	if err != nil {
		return identity.MCPAuthorizationRequestRecord{}, notFound(err)
	}
	return identity.MCPAuthorizationRequestRecord{
		ClientID: row.ClientID, RedirectURI: row.RedirectUri, CodeChallenge: row.CodeChallenge,
		Scopes: row.Scopes, Resource: row.Resource, State: row.State.String,
		CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
	}, nil
}

// FindTokenGrantOwner returns the account and grant of an access or refresh
// token digest, consumed or not. The lookup is unlocked: the caller locks the
// account and then the grant before deciding anything.
func (t *mcpTx) FindTokenGrantOwner(ctx context.Context, tokenHash []byte) (string, string, error) {
	row, err := t.q.FindMCPTokenGrantOwner(ctx, tokenHash)
	if err != nil {
		return "", "", notFound(err)
	}
	return row.UserID.String(), row.GrantID.String(), nil
}
