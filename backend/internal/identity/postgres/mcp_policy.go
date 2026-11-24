package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	_ identity.MCPPolicyRepository  = (*MCPRepository)(nil)
	_ identity.MCPPolicyTransaction = (*mcpPolicyTx)(nil)
)

// WithMCPPolicyTransaction runs one account policy command at READ COMMITTED
// with the explicit row locks of ADR 0023 "Slice B1b1".
func (r *MCPRepository) WithMCPPolicyTransaction(ctx context.Context, work func(identity.MCPPolicyTransaction) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	q := identitydb.New(tx)
	if err = work(&mcpPolicyTx{mcpTx: mcpTx{q: q}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type mcpPolicyTx struct{ mcpTx }

func (t *mcpPolicyTx) RequireInstanceAdmin(ctx context.Context, actorID string) error {
	if _, err := t.q.ShareInstallationForMCPPolicy(ctx); err != nil {
		return err
	}
	actor, err := scanUUID(actorID)
	if err != nil {
		return identity.ErrInstanceAdminRequired
	}
	granted, err := t.q.IsMCPPolicyInstanceAdmin(ctx, actor)
	if err != nil {
		return err
	}
	if !granted {
		return identity.ErrInstanceAdminRequired
	}
	return nil
}

// LockLiveAccount takes only the live user_accounts row FOR SHARE. Unlike the
// issuance lock it skips local_credentials: a policy write has nothing to
// serialize with a password replacement.
func (t *mcpPolicyTx) LockLiveAccount(ctx context.Context, userID string) error {
	user, err := scanUUID(userID)
	if err != nil {
		return err
	}
	_, err = t.q.LockLiveMCPAccount(ctx, user)
	return notFound(err)
}

func (t *mcpPolicyTx) ReadLiveAccountPolicy(ctx context.Context, userID string) (identity.MCPPolicyRecord, int64, error) {
	user, err := scanUUID(userID)
	if err != nil {
		return identity.MCPPolicyRecord{}, 0, err
	}
	row, err := t.q.ReadLiveMCPAccountPolicy(ctx, user)
	if err != nil {
		return identity.MCPPolicyRecord{}, 0, notFound(err)
	}
	record := mcpPolicyRecord(identitydb.ReadMCPPolicyRow{
		InstallationEnabled: row.InstallationEnabled, DefaultAccountEnabled: row.DefaultAccountEnabled,
		DefaultMaxScopes: row.DefaultMaxScopes, PolicyUserID: row.PolicyUserID, AdminEnabled: row.AdminEnabled,
		AdminMaxScopes: row.AdminMaxScopes, UserEnabled: row.UserEnabled, UserScopes: row.UserScopes,
	})
	return record, row.PolicyVersion.Int64, nil
}

func (t *mcpPolicyTx) LockAccountPolicy(ctx context.Context, userID string) (identity.MCPPolicyRow, bool, error) {
	user, err := scanUUID(userID)
	if err != nil {
		return identity.MCPPolicyRow{}, false, err
	}
	inserted, err := t.q.EnsureMCPAccountPolicy(ctx, user)
	if err != nil {
		return identity.MCPPolicyRow{}, false, err
	}
	row, err := t.q.LockMCPAccountPolicy(ctx, user)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.MCPPolicyRow{}, false, identity.ErrNotFound
	}
	if err != nil {
		return identity.MCPPolicyRow{}, false, err
	}
	out := identity.MCPPolicyRow{
		AdminMaxScopes: row.AdminMaxScopes, UserEnabled: row.UserEnabled,
		UserScopes: row.UserScopes, Version: row.Version,
	}
	if row.AdminEnabled.Valid {
		enabled := row.AdminEnabled.Bool
		out.AdminEnabled = &enabled
	}
	return out, inserted == 1, nil
}

func (t *mcpPolicyTx) UpdateAccountPolicy(ctx context.Context, userID string, row identity.MCPPolicyRow, now time.Time) error {
	user, err := scanUUID(userID)
	if err != nil {
		return err
	}
	rows, err := t.q.UpdateMCPAccountPolicy(ctx, identitydb.UpdateMCPAccountPolicyParams{
		AdminEnabled: optionalBool(row.AdminEnabled), AdminMaxScopes: row.AdminMaxScopes,
		UserEnabled: row.UserEnabled, UserScopes: row.UserScopes,
		Version: row.Version, UpdatedAt: ts(now), UserID: user,
	})
	if err == nil && rows != 1 {
		return errors.New("locked MCP account policy was not updated")
	}
	return err
}

func (t *mcpPolicyTx) AppendPolicyEvent(ctx context.Context, event identity.MCPPolicyEvent) error {
	actor, err := scanUUID(event.ActorUserID)
	if err != nil {
		return err
	}
	target, err := scanUUID(event.TargetUserID)
	if err != nil {
		return err
	}
	return t.q.AppendMCPPolicyEvent(ctx, identitydb.AppendMCPPolicyEventParams{
		ActorUserID: actor, TargetUserID: target, Policy: string(event.Kind),
		Enabled: optionalBool(event.Enabled), Scopes: event.Scopes, OccurredAt: ts(event.OccurredAt),
	})
}

func optionalBool(v *bool) pgtype.Bool {
	if v == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *v, Valid: true}
}
