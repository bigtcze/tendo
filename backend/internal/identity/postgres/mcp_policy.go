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
	_ identity.MCPPolicyRepository = (*MCPRepository)(nil)
	// The gate reads the installation switch from this repository. A missing
	// method is a build error here, not a silent fail-closed in production.
	_ identity.MCPInstallationStore = (*MCPRepository)(nil)
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

// Slice B1b3: the administrator grant list and revocations. FindGrantOwner,
// LockGrant and RevokeGrant are the connected-apps methods of the embedded
// mcpTx.

func (t *mcpPolicyTx) ListLiveGrants(ctx context.Context, userID string, now time.Time, afterID string, limit int) ([]identity.MCPGrantItem, error) {
	return listLiveGrants(ctx, t.q, userID, now, afterID, limit)
}

// LockAccountForGrants is the issuance account lock: the live user_accounts row
// FOR SHARE, then its local credential FOR SHARE, so an administrator's
// revocation serializes with deletion and password replacement.
func (t *mcpPolicyTx) LockAccountForGrants(ctx context.Context, userID string) error {
	return t.mcpTx.LockLiveAccount(ctx, userID)
}

// RevokeAccountGrants is the account-wide revocation a password replacement
// runs: codes, then grants (one audited event each), then token rows.
func (t *mcpPolicyTx) RevokeAccountGrants(ctx context.Context, userID string, now time.Time, revocation identity.MCPRevocation) error {
	user, err := scanUUID(userID)
	if err != nil {
		return err
	}
	return revokeAccountMCPAccess(ctx, t.q, user, now, revocation)
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

// AppendPolicyEvent writes one audit row. An empty TargetUserID is the
// installation level, stored as NULL.
func (t *mcpPolicyTx) AppendPolicyEvent(ctx context.Context, event identity.MCPPolicyEvent) error {
	actor, err := scanUUID(event.ActorUserID)
	if err != nil {
		return err
	}
	var target pgtype.UUID
	if event.TargetUserID != "" {
		if target, err = scanUUID(event.TargetUserID); err != nil {
			return err
		}
	}
	return t.q.AppendMCPPolicyEvent(ctx, identitydb.AppendMCPPolicyEventParams{
		ActorUserID: actor, TargetUserID: target, Policy: string(event.Kind),
		Enabled: optionalBool(event.Enabled), DefaultAccountEnabled: optionalBool(event.DefaultAccountEnabled),
		Scopes: event.Scopes, OccurredAt: ts(event.OccurredAt),
	})
}

// ReadInstallationPolicy reads the installation row without a lock. No row is
// identity.ErrMCPInstallationMissing; nothing is defaulted.
func (t *mcpPolicyTx) ReadInstallationPolicy(ctx context.Context) (identity.MCPInstallationRow, error) {
	row, err := t.q.ReadMCPInstallationPolicy(ctx)
	if err != nil {
		return identity.MCPInstallationRow{}, installationMissing(err)
	}
	return identity.MCPInstallationRow{
		Enabled: row.Enabled, DefaultAccountEnabled: row.DefaultAccountEnabled,
		DefaultMaxScopes: row.DefaultMaxScopes, Version: row.Version,
	}, nil
}

// LockInstallationPolicy takes the installation row FOR UPDATE. It runs after
// RequireInstanceAdmin, so the ADR 0023 order holds.
func (t *mcpPolicyTx) LockInstallationPolicy(ctx context.Context) (identity.MCPInstallationRow, error) {
	row, err := t.q.LockMCPInstallationPolicy(ctx)
	if err != nil {
		return identity.MCPInstallationRow{}, installationMissing(err)
	}
	return identity.MCPInstallationRow{
		Enabled: row.Enabled, DefaultAccountEnabled: row.DefaultAccountEnabled,
		DefaultMaxScopes: row.DefaultMaxScopes, Version: row.Version,
	}, nil
}

// UpdateInstallationPolicy stores the locked row's terms and version. A lost
// lock is a zero-row update, which is refused.
func (t *mcpPolicyTx) UpdateInstallationPolicy(ctx context.Context, row identity.MCPInstallationRow, now time.Time) error {
	rows, err := t.q.UpdateMCPInstallationPolicy(ctx, identitydb.UpdateMCPInstallationPolicyParams{
		Enabled: row.Enabled, DefaultAccountEnabled: row.DefaultAccountEnabled,
		DefaultMaxScopes: row.DefaultMaxScopes, Version: row.Version, UpdatedAt: ts(now),
	})
	if err == nil && rows != 1 {
		return errors.New("locked MCP installation policy was not updated")
	}
	return err
}

// ListPolicyEvents returns one page of the audit list, newest first, across all
// three levels. afterID is empty for the first page.
func (t *mcpPolicyTx) ListPolicyEvents(ctx context.Context, afterID string, limit int) ([]identity.MCPPolicyEvent, error) {
	return listPolicyEvents(ctx, t.q, afterID, limit)
}

func listPolicyEvents(ctx context.Context, q *identitydb.Queries, afterID string, limit int) ([]identity.MCPPolicyEvent, error) {
	var after pgtype.UUID
	if afterID != "" {
		var err error
		if after, err = scanUUID(afterID); err != nil {
			return nil, err
		}
	}
	rows, err := q.ListMCPPolicyEvents(ctx, identitydb.ListMCPPolicyEventsParams{AfterID: after, PageLimit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]identity.MCPPolicyEvent, 0, len(rows))
	for _, row := range rows {
		event := identity.MCPPolicyEvent{
			ID: row.ID.String(), Kind: identity.MCPPolicyEventKind(row.Policy),
			ActorUserID: row.ActorUserID.String(), Scopes: row.Scopes, OccurredAt: row.OccurredAt.Time,
		}
		if row.ActorLogin.Valid {
			event.ActorLogin = row.ActorLogin.String
		}
		if row.TargetLogin.Valid {
			event.TargetLogin = row.TargetLogin.String
		}
		if row.TargetUserID.Valid {
			event.TargetUserID = row.TargetUserID.String()
		}
		if row.Enabled.Valid {
			enabled := row.Enabled.Bool
			event.Enabled = &enabled
		}
		if row.DefaultAccountEnabled.Valid {
			defaultOn := row.DefaultAccountEnabled.Bool
			event.DefaultAccountEnabled = &defaultOn
		}
		out = append(out, event)
	}
	return out, nil
}

// ReadInstallationSwitch is the unlocked installation switch the gate reads on
// every MCP route. No row is identity.ErrMCPInstallationMissing.
func (r *MCPRepository) ReadInstallationSwitch(ctx context.Context) (bool, error) {
	enabled, err := identitydb.New(r.pool).ReadMCPInstallationSwitch(ctx)
	if err != nil {
		return false, installationMissing(err)
	}
	return enabled, nil
}

// installationMissing maps an absent installation row to its own error. Other
// errors pass through; the caller records them as faults.
func installationMissing(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrMCPInstallationMissing
	}
	return err
}

func optionalBool(v *bool) pgtype.Bool {
	if v == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *v, Valid: true}
}
