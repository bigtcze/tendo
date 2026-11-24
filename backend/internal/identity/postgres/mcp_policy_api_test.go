package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/jackc/pgx/v5"
)

// Slice B1b1 of #457: the account policy API against the real schema, the
// runtime role and the ADR 0023 lock order.

func (e *mcpEnv) policyService(t *testing.T) *identity.MCPPolicyService {
	t.Helper()
	svc, err := identity.NewMCPPolicyService(NewMCPRepository(e.app), true, e.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func (e *mcpEnv) grantInstanceAdmin(t *testing.T, user string) {
	t.Helper()
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO instance_admins(user_id, granted_at, grant_source) VALUES ($1::uuid, now(), 'cli')`, user); err != nil {
		t.Fatal(err)
	}
}

type policyEventRow struct {
	policy, actor, target, enabled, scopes string
}

func (e *mcpEnv) policyEvents(t *testing.T, target string) []policyEventRow {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT policy, actor_user_id::text, target_user_id::text,
		COALESCE(enabled::text, 'null'), COALESCE(array_to_string(scopes, ','), 'null')
		FROM mcp_policy_events WHERE target_user_id=$1::uuid ORDER BY id`, target)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []policyEventRow
	for rows.Next() {
		var r policyEventRow
		if err := rows.Scan(&r.policy, &r.actor, &r.target, &r.enabled, &r.scopes); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestMCPPolicyAPIAdminWritesAuditAndEnforce: an administrator switch-off
// through the API pauses a live grant, a no-op write appends nothing, and the
// return to the default restores the same tokens. Each change is one event.
func TestMCPPolicyAPIAdminWritesAuditAndEnforce(t *testing.T) {
	env := newMCPEnv(t)
	policies := env.policyService(t)
	adminID, _ := env.newAccount(t, "mcp_api_admin")
	env.grantInstanceAdmin(t, adminID)
	user, session := env.newAccount(t, "mcp_api_target")
	tokens, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatal(err)
	}

	view, err := policies.AccountPolicy(env.ctx, adminID, user)
	if err != nil || view.Version != 1 || view.AdminEnabled != nil || view.AdminMaxScopes != nil ||
		!slices.Equal(view.Effective, []string{identity.MCPScopeRead, identity.MCPScopeWrite}) {
		t.Fatalf("initial view %+v err=%v", view, err)
	}
	off := false
	view, err = policies.UpdateAccountPolicy(env.ctx, adminID, user, identity.MCPAccountPolicyChange{Enabled: &off, MaxScopes: []string{identity.MCPScopeRead}}, 1)
	if err != nil || view.Version != 2 || len(view.Effective) != 0 {
		t.Fatalf("switch off %+v err=%v", view, err)
	}
	if _, err := env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("verify after admin switch-off err=%v", err)
	}
	if view, err = policies.UpdateAccountPolicy(env.ctx, adminID, user, identity.MCPAccountPolicyChange{Enabled: &off, MaxScopes: []string{identity.MCPScopeRead}}, 2); err != nil || view.Version != 2 {
		t.Fatalf("no-op %+v err=%v", view, err)
	}
	if _, err := policies.UpdateAccountPolicy(env.ctx, adminID, user, identity.MCPAccountPolicyChange{}, 1); !errors.Is(err, identity.ErrMCPPolicyPreconditionFailed) {
		t.Fatalf("stale write err=%v", err)
	}
	if view, err = policies.UpdateAccountPolicy(env.ctx, adminID, user, identity.MCPAccountPolicyChange{}, 2); err != nil || view.Version != 3 {
		t.Fatalf("restore default %+v err=%v", view, err)
	}
	if access, err := env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); err != nil || len(access.Scopes) != 2 {
		t.Fatalf("verify after restore %+v err=%v", access, err)
	}
	want := []policyEventRow{
		{"account", adminID, user, "false", identity.MCPScopeRead},
		{"account", adminID, user, "null", "null"},
	}
	if got := env.policyEvents(t, user); !slices.Equal(got, want) {
		t.Fatalf("events %+v want %+v", got, want)
	}
	if g := env.grantOf(t, user); g.revoked {
		t.Fatal("an administrator switch-off revoked the grant")
	}
}

// TestMCPPolicyAPIUserSettingsOnlyNarrow: the owner's settings intersect the
// administrator's maximum and are audited with the owner as actor and target.
func TestMCPPolicyAPIUserSettingsOnlyNarrow(t *testing.T) {
	env := newMCPEnv(t)
	policies := env.policyService(t)
	user, session := env.newAccount(t, "mcp_api_owner")
	tokens, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatal(err)
	}
	env.setPolicy(t, `UPDATE mcp_account_policies SET admin_max_scopes=ARRAY['tendo:read'] WHERE user_id=$1::uuid`, user)
	settings, err := policies.UpdateSettings(env.ctx, user, identity.MCPSettingsChange{Enabled: true, Scopes: []string{identity.MCPScopeWrite, identity.MCPScopeRead}}, 1)
	if err != nil || settings.Version != 1 {
		t.Fatalf("no-op settings %+v err=%v", settings, err)
	}
	if !slices.Equal(settings.Maximum, []string{identity.MCPScopeRead}) || !slices.Equal(settings.Effective, []string{identity.MCPScopeRead}) {
		t.Fatalf("settings cannot widen past the maximum: %+v", settings)
	}
	settings, err = policies.UpdateSettings(env.ctx, user, identity.MCPSettingsChange{Enabled: false, Scopes: []string{identity.MCPScopeRead}}, 1)
	if err != nil || settings.Version != 2 || len(settings.Effective) != 0 || !slices.Equal(settings.Maximum, []string{identity.MCPScopeRead}) {
		t.Fatalf("switch own access off %+v err=%v", settings, err)
	}
	if _, err := env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("verify after own switch-off err=%v", err)
	}
	if got := env.policyEvents(t, user); !slices.Equal(got, []policyEventRow{{"user", user, user, "false", identity.MCPScopeRead}}) {
		t.Fatalf("events %+v", got)
	}
}

// TestMCPPolicyAPIRefusals: authority is the live instance-administrator grant,
// a deleted target is not found, and a deleted owner is unauthenticated.
func TestMCPPolicyAPIRefusals(t *testing.T) {
	env := newMCPEnv(t)
	policies := env.policyService(t)
	adminID, _ := env.newAccount(t, "mcp_api_refuse_admin")
	env.grantInstanceAdmin(t, adminID)
	plain, _ := env.newAccount(t, "mcp_api_refuse_plain")
	target, _ := env.newAccount(t, "mcp_api_refuse_target")
	if _, err := policies.AccountPolicy(env.ctx, plain, target); !errors.Is(err, identity.ErrInstanceAdminRequired) {
		t.Fatalf("plain account read err=%v", err)
	}
	if _, err := policies.UpdateAccountPolicy(env.ctx, plain, "not-a-uuid", identity.MCPAccountPolicyChange{}, 1); !errors.Is(err, identity.ErrInstanceAdminRequired) {
		t.Fatalf("plain account write of a malformed id err=%v", err)
	}
	if _, err := policies.AccountPolicy(env.ctx, adminID, "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B61"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("uppercase id err=%v", err)
	}
	tombstoneAccount(t, env.ctx, env.admin, target, tombstoneOptions{})
	if _, err := policies.AccountPolicy(env.ctx, adminID, target); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("deleted target read err=%v", err)
	}
	if _, err := policies.UpdateAccountPolicy(env.ctx, adminID, target, identity.MCPAccountPolicyChange{}, 1); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("deleted target write err=%v", err)
	}
	if _, err := policies.Settings(env.ctx, target); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("deleted owner read err=%v", err)
	}
	if _, err := policies.UpdateSettings(env.ctx, target, identity.MCPSettingsChange{Enabled: true, Scopes: []string{identity.MCPScopeRead}}, 1); !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("deleted owner write err=%v", err)
	}
	tombstoneAccount(t, env.ctx, env.admin, adminID, tombstoneOptions{})
	if _, err := policies.AccountPolicy(env.ctx, adminID, plain); !errors.Is(err, identity.ErrInstanceAdminRequired) {
		t.Fatalf("deleted administrator read err=%v", err)
	}
	if got := env.policyEvents(t, target); len(got) != 0 {
		t.Fatalf("refused writes appended events: %+v", got)
	}
}

// TestMCPPolicyAPIRepairsMissingRow: a missing account row reads as inactive
// with version 1, and If-Match "1" repairs it with one audited write.
func TestMCPPolicyAPIRepairsMissingRow(t *testing.T) {
	env := newMCPEnv(t)
	policies := env.policyService(t)
	user, _ := env.newAccount(t, "mcp_api_repair")
	env.setPolicy(t, `DELETE FROM mcp_account_policies WHERE user_id=$1::uuid`, user)
	settings, err := policies.Settings(env.ctx, user)
	if err != nil || settings.Version != 1 || len(settings.Maximum) != 0 || len(settings.Effective) != 0 {
		t.Fatalf("missing row view %+v err=%v", settings, err)
	}
	settings, err = policies.UpdateSettings(env.ctx, user, identity.MCPSettingsChange{Enabled: true, Scopes: []string{identity.MCPScopeRead, identity.MCPScopeWrite}}, 1)
	if err != nil || settings.Version != 2 || len(settings.Effective) != 2 {
		t.Fatalf("repair %+v err=%v", settings, err)
	}
	if got := env.policyEvents(t, user); len(got) != 1 || got[0].policy != "user" {
		t.Fatalf("repair events %+v", got)
	}
}

// TestMCPPolicyAPIDatabaseRefusesInvalidScopes: the CHECK constraints refuse
// what the API validates, for the runtime role too.
func TestMCPPolicyAPIDatabaseRefusesInvalidScopes(t *testing.T) {
	env := newMCPEnv(t)
	user, _ := env.newAccount(t, "mcp_api_check")
	for _, sql := range []string{
		`UPDATE mcp_account_policies SET user_scopes='{}' WHERE user_id=$1::uuid`,
		`UPDATE mcp_account_policies SET user_scopes=ARRAY['tendo:admin'] WHERE user_id=$1::uuid`,
		`UPDATE mcp_account_policies SET admin_max_scopes='{}' WHERE user_id=$1::uuid`,
		`UPDATE mcp_account_policies SET admin_max_scopes=ARRAY['tendo:read','tendo:delete'] WHERE user_id=$1::uuid`,
		`INSERT INTO mcp_policy_events(actor_user_id, target_user_id, policy, enabled, scopes, occurred_at) VALUES ($1::uuid, $1::uuid, 'user', true, '{}', now())`,
	} {
		if _, err := env.app.Exec(env.ctx, sql, user); err == nil {
			t.Errorf("runtime role accepted %s", sql)
		}
	}
	if _, err := env.app.Exec(env.ctx, `DELETE FROM mcp_policy_events`); err == nil {
		t.Error("runtime role deleted policy events")
	}
}

// TestMCPPolicyAPILockOrder: an administrator write takes installation_state
// FOR SHARE before anything else, and an owner write queued on the policy row
// behind an administrator change sees the new version and fails closed.
func TestMCPPolicyAPILockOrder(t *testing.T) {
	env := newMCPEnv(t)
	policies := env.policyService(t)
	adminID, _ := env.newAccount(t, "mcp_api_lock_admin")
	env.grantInstanceAdmin(t, adminID)
	user, _ := env.newAccount(t, "mcp_api_lock_owner")

	tx, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(env.ctx, `SELECT 1 FROM installation_state WHERE singleton FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	adminDone := make(chan error, 1)
	go func() {
		_, err := policies.UpdateAccountPolicy(env.ctx, adminID, user, identity.MCPAccountPolicyChange{MaxScopes: []string{identity.MCPScopeRead}}, 1)
		adminDone <- err
	}()
	waitForLockWaiters(t, env.ctx, env.observer, "%installation_state%FOR SHARE%", 1)
	if err = tx.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-adminDone; err != nil {
		t.Fatalf("administrator write after the installation lock: %v", err)
	}

	held, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback(context.Background()) }()
	if _, err = held.Exec(env.ctx, `UPDATE mcp_account_policies SET admin_enabled=false, version=version+1 WHERE user_id=$1::uuid`, user); err != nil {
		t.Fatal(err)
	}
	ownerDone := make(chan error, 1)
	go func() {
		_, err := policies.UpdateSettings(env.ctx, user, identity.MCPSettingsChange{Enabled: false, Scopes: []string{identity.MCPScopeRead}}, 2)
		ownerDone <- err
	}()
	waitForLockWaiters(t, env.ctx, env.observer, "%mcp_account_policies%", 1)
	if err = held.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-ownerDone; !errors.Is(err, identity.ErrMCPPolicyPreconditionFailed) {
		t.Fatalf("owner write queued behind an administrator change err=%v, want precondition failed", err)
	}
}

// policyVersion reads the stored account row version through the admin pool.
func (e *mcpEnv) policyVersion(t *testing.T, user string) int64 {
	t.Helper()
	var version int64
	if err := e.admin.QueryRow(e.ctx, `SELECT version FROM mcp_account_policies WHERE user_id=$1::uuid`, user).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

const policyTombstoneSQL = `UPDATE user_accounts SET deleted_at = now(), login = '~deleted-' || replace(id::text, '-', '') WHERE id = $1::uuid`

// TestMCPPolicyAPIDeletionFirstRace: account deletion holds the target row;
// both policy writes queue on the live account lock, then refuse the deleted
// account without a version bump or an audit row.
func TestMCPPolicyAPIDeletionFirstRace(t *testing.T) {
	env := newMCPEnv(t)
	policies := env.policyService(t)
	adminID, _ := env.newAccount(t, "mcp_api_race_admin")
	env.grantInstanceAdmin(t, adminID)
	user, _ := env.newAccount(t, "mcp_api_race_target")

	tx, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(env.ctx, policyTombstoneSQL, user); err != nil {
		t.Fatal(err)
	}
	adminDone, ownerDone := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := policies.UpdateAccountPolicy(env.ctx, adminID, user, identity.MCPAccountPolicyChange{MaxScopes: []string{identity.MCPScopeRead}}, 1)
		adminDone <- err
	}()
	go func() {
		_, err := policies.UpdateSettings(env.ctx, user, identity.MCPSettingsChange{Enabled: false, Scopes: []string{identity.MCPScopeRead}}, 1)
		ownerDone <- err
	}()
	waitForLockWaiters(t, env.ctx, env.observer, "%user_accounts%FOR SHARE%", 2)
	if err = tx.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-adminDone; !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("administrator write queued behind deletion err=%v, want not found", err)
	}
	if err = <-ownerDone; !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("owner write queued behind deletion err=%v, want unauthenticated", err)
	}
	if got := env.policyEvents(t, user); len(got) != 0 {
		t.Fatalf("writes for a deleted account appended events: %+v", got)
	}
	if v := env.policyVersion(t, user); v != 1 {
		t.Fatalf("version after refused writes = %d, want 1", v)
	}
}

// TestMCPPolicyAPIPolicyWriteFirstRace: an administrator write that already
// holds the live account lock (and waits on the policy row) makes account
// deletion wait; the write commits with its audit row before the deletion.
// It also proves the account lock precedes the policy row lock.
func TestMCPPolicyAPIPolicyWriteFirstRace(t *testing.T) {
	env := newMCPEnv(t)
	policies := env.policyService(t)
	adminID, _ := env.newAccount(t, "mcp_api_race2_admin")
	env.grantInstanceAdmin(t, adminID)
	user, _ := env.newAccount(t, "mcp_api_race2_target")

	held, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback(context.Background()) }()
	if _, err = held.Exec(env.ctx, `SELECT 1 FROM mcp_account_policies WHERE user_id=$1::uuid FOR UPDATE`, user); err != nil {
		t.Fatal(err)
	}
	adminDone := make(chan error, 1)
	go func() {
		_, err := policies.UpdateAccountPolicy(env.ctx, adminID, user, identity.MCPAccountPolicyChange{MaxScopes: []string{identity.MCPScopeRead}}, 1)
		adminDone <- err
	}()
	waitForLockWaiters(t, env.ctx, env.observer, "%mcp_account_policies%FOR UPDATE%", 1)
	deleteDone := make(chan error, 1)
	go func() {
		_, err := env.admin.Exec(env.ctx, policyTombstoneSQL, user)
		deleteDone <- err
	}()
	// The deletion runs as the admin role, whose query text only an admin
	// session can see.
	waitForLockWaiters(t, env.ctx, env.admin, "%UPDATE user_accounts%", 1)
	if err = held.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-adminDone; err != nil {
		t.Fatalf("administrator write holding the account lock: %v", err)
	}
	if err = <-deleteDone; err != nil {
		t.Fatalf("deletion after the policy write: %v", err)
	}
	if got := env.policyEvents(t, user); len(got) != 1 || got[0].scopes != identity.MCPScopeRead {
		t.Fatalf("events %+v, want the one administrator change", got)
	}
}

// TestMCPPolicyAPIDemotedAdmin: a live account whose instance-administrator
// grant was removed is refused on read and write, and writes nothing.
func TestMCPPolicyAPIDemotedAdmin(t *testing.T) {
	env := newMCPEnv(t)
	policies := env.policyService(t)
	adminID, _ := env.newAccount(t, "mcp_api_demoted_admin")
	env.grantInstanceAdmin(t, adminID)
	user, _ := env.newAccount(t, "mcp_api_demoted_target")
	if _, err := policies.AccountPolicy(env.ctx, adminID, user); err != nil {
		t.Fatalf("read while administrator: %v", err)
	}
	if _, err := env.admin.Exec(env.ctx, `DELETE FROM instance_admins WHERE user_id=$1::uuid`, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := policies.AccountPolicy(env.ctx, adminID, user); !errors.Is(err, identity.ErrInstanceAdminRequired) {
		t.Fatalf("demoted read err=%v", err)
	}
	off := false
	if _, err := policies.UpdateAccountPolicy(env.ctx, adminID, user, identity.MCPAccountPolicyChange{Enabled: &off}, 1); !errors.Is(err, identity.ErrInstanceAdminRequired) {
		t.Fatalf("demoted write err=%v", err)
	}
	if got := env.policyEvents(t, user); len(got) != 0 {
		t.Fatalf("demoted write appended events: %+v", got)
	}
	if v := env.policyVersion(t, user); v != 1 {
		t.Fatalf("version after demoted write = %d", v)
	}
}

// TestMCPPolicyAPIIgnoresCredentialLock: a policy write does not wait on a
// password replacement holding the credential row.
func TestMCPPolicyAPIIgnoresCredentialLock(t *testing.T) {
	env := newMCPEnv(t)
	policies := env.policyService(t)
	user, _ := env.newAccount(t, "mcp_api_credential")
	held, err := env.admin.BeginTx(env.ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback(context.Background()) }()
	if _, err = held.Exec(env.ctx, `SELECT 1 FROM local_credentials WHERE user_id=$1::uuid FOR UPDATE`, user); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(env.ctx, 2*time.Second)
	defer cancel()
	if _, err := policies.UpdateSettings(ctx, user, identity.MCPSettingsChange{Enabled: false, Scopes: []string{identity.MCPScopeRead}}, 1); err != nil {
		t.Fatalf("owner write while the credential row is held: %v", err)
	}
}
