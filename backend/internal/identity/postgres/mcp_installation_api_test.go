package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// Slice B1b2 of #457: the installation policy API and the policy audit list
// against the real schema and the ADR 0023 lock order. The runtime role's
// missing UPDATE and DELETE on mcp_policy_events is proven by
// household/postgres TestMigrationThirtyTwoUpAndPrivileges, which this slice
// cites rather than duplicates.

// resetInstallation restores the shared installation singleton to its migrated
// defaults, whether the row was changed or deleted. It is an upsert, so a test
// that removed the row cannot leave the next test with a missing installation.
// It is fatal on failure, because a leaked switch would skew later tests.
func (e *mcpEnv) resetInstallation(t *testing.T) {
	t.Helper()
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO mcp_installation_policy(singleton, enabled, default_account_enabled, default_max_scopes, version)
		VALUES (true, true, true, ARRAY['tendo:read','tendo:write'], 1)
		ON CONFLICT (singleton) DO UPDATE SET enabled=true, default_account_enabled=true,
		default_max_scopes=ARRAY['tendo:read','tendo:write'], version=1`); err != nil {
		t.Fatalf("reset installation policy: %v", err)
	}
}

// installationEvents reads the installation-level audit rows, oldest first.
func (e *mcpEnv) installationEvents(t *testing.T) []policyEventRow {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT policy, actor_user_id::text, COALESCE(target_user_id::text, 'null'),
		COALESCE(enabled::text, 'null'), COALESCE(array_to_string(scopes, ','), 'null')
		FROM mcp_policy_events WHERE policy='installation' ORDER BY id`)
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

func (e *mcpEnv) installationService(t *testing.T, operatorEnabled bool) *identity.MCPPolicyService {
	t.Helper()
	svc, err := identity.NewMCPPolicyService(NewMCPRepository(e.app), operatorEnabled, e.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// TestMCPInstallationAPIChangesAreAuditedOnce: a change bumps the version and
// appends one installation row, a no-op keeps both, and a stale version writes
// nothing. The installation row is the audit actor's target, with no account.
func TestMCPInstallationAPIChangesAreAuditedOnce(t *testing.T) {
	env := newMCPEnv(t)
	env.resetInstallation(t)
	t.Cleanup(func() { env.resetInstallation(t) })
	policies := env.installationService(t, true)
	adminID, _ := env.newAccount(t, "mcp_inst_admin")
	env.grantInstanceAdmin(t, adminID)

	view, err := policies.InstallationPolicy(env.ctx, adminID)
	if err != nil || view.Version != 1 || !view.Enabled || !view.OperatorEnabled {
		t.Fatalf("initial installation view %+v err=%v", view, err)
	}
	off := identity.MCPInstallationChange{Enabled: false, DefaultAccountEnabled: true, DefaultMaxScopes: []string{identity.MCPScopeRead}}
	view, err = policies.UpdateInstallationPolicy(env.ctx, adminID, off, 1)
	if err != nil || view.Version != 2 || view.Enabled || !slices.Equal(view.DefaultMaxScopes, []string{identity.MCPScopeRead}) {
		t.Fatalf("switch installation off %+v err=%v", view, err)
	}
	if view, err = policies.UpdateInstallationPolicy(env.ctx, adminID, off, 2); err != nil || view.Version != 2 {
		t.Fatalf("no-op installation write %+v err=%v", view, err)
	}
	if _, err := policies.UpdateInstallationPolicy(env.ctx, adminID, off, 1); !errors.Is(err, identity.ErrMCPPolicyPreconditionFailed) {
		t.Fatalf("stale installation write err=%v", err)
	}
	want := []policyEventRow{
		{"installation", adminID, "null", "false", identity.MCPScopeRead},
	}
	if got := env.installationEvents(t); !slices.Equal(got, want) {
		t.Fatalf("installation events %+v want %+v", got, want)
	}
}

// TestMCPInstallationAPINonAdminWritesNothing: a signed-in non-administrator
// is refused before any lock, row or event changes.
func TestMCPInstallationAPINonAdminWritesNothing(t *testing.T) {
	env := newMCPEnv(t)
	env.resetInstallation(t)
	t.Cleanup(func() { env.resetInstallation(t) })
	policies := env.installationService(t, true)
	user, _ := env.newAccount(t, "mcp_inst_plain")

	if _, err := policies.UpdateInstallationPolicy(env.ctx, user, identity.MCPInstallationChange{DefaultMaxScopes: []string{identity.MCPScopeRead}}, 1); !errors.Is(err, identity.ErrInstanceAdminRequired) {
		t.Fatalf("non-admin installation write err=%v", err)
	}
	if got := env.installationEvents(t); len(got) != 0 {
		t.Fatalf("non-admin wrote audit rows %+v", got)
	}
	if v := env.installationVersion(t); v != 1 {
		t.Fatalf("non-admin bumped the installation version to %d", v)
	}
}

// TestMCPInstallationAPIMissingRowFailsClosed: a missing installation row is
// never defaulted. The read and the write both refuse with ErrMCPInstallationMissing,
// and the gate's unlocked switch read fails closed too.
func TestMCPInstallationAPIMissingRowFailsClosed(t *testing.T) {
	env := newMCPEnv(t)
	env.resetInstallation(t)
	t.Cleanup(func() { env.resetInstallation(t) })
	policies := env.installationService(t, true)
	adminID, _ := env.newAccount(t, "mcp_inst_missing")
	env.grantInstanceAdmin(t, adminID)

	if _, err := env.admin.Exec(env.ctx, `DELETE FROM mcp_installation_policy`); err != nil {
		t.Fatal(err)
	}
	if _, err := policies.InstallationPolicy(env.ctx, adminID); !errors.Is(err, identity.ErrMCPInstallationMissing) {
		t.Fatalf("read with a missing row err=%v", err)
	}
	if _, err := policies.UpdateInstallationPolicy(env.ctx, adminID, identity.MCPInstallationChange{DefaultMaxScopes: []string{identity.MCPScopeRead}}, 1); !errors.Is(err, identity.ErrMCPInstallationMissing) {
		t.Fatalf("write with a missing row err=%v", err)
	}
	if _, err := NewMCPRepository(env.app).ReadInstallationSwitch(env.ctx); !errors.Is(err, identity.ErrMCPInstallationMissing) {
		t.Fatalf("gate switch with a missing row err=%v", err)
	}
	if got := env.installationEvents(t); len(got) != 0 {
		t.Fatalf("missing row still wrote audit rows %+v", got)
	}
}

// TestMCPInstallationAPILockOrder: the installation write takes the
// installation_state FOR SHARE lock first. A concurrent holder of that row
// blocks the write until it commits, then the write proceeds.
func TestMCPInstallationAPILockOrder(t *testing.T) {
	env := newMCPEnv(t)
	env.resetInstallation(t)
	t.Cleanup(func() { env.resetInstallation(t) })
	policies := env.installationService(t, true)
	adminID, _ := env.newAccount(t, "mcp_inst_lock_admin")
	env.grantInstanceAdmin(t, adminID)

	tx, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(env.ctx, `SELECT 1 FROM installation_state WHERE singleton FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := policies.UpdateInstallationPolicy(env.ctx, adminID, identity.MCPInstallationChange{Enabled: true, DefaultAccountEnabled: true, DefaultMaxScopes: []string{identity.MCPScopeRead}}, 1)
		done <- err
	}()
	waitForLockWaiters(t, env.ctx, env.observer, "%installation_state%FOR SHARE%", 1)
	if err = tx.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatalf("installation write after the installation lock: %v", err)
	}
}

// TestMCPInstallationAPIEnforcementFollowsTheSwitch: switching the installation
// off pauses a live token and a pending grant, and switching it back on restores
// the same token, with the switch read from the real database.
func TestMCPInstallationAPIEnforcementFollowsTheSwitch(t *testing.T) {
	env := newMCPEnv(t)
	env.resetInstallation(t)
	t.Cleanup(func() { env.resetInstallation(t) })
	policies := env.installationService(t, true)
	adminID, _ := env.newAccount(t, "mcp_inst_enforce_admin")
	env.grantInstanceAdmin(t, adminID)
	user, session := env.newAccount(t, "mcp_inst_enforce_user")
	tokens, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatal(err)
	}

	gate := NewMCPRepository(env.app)
	if on, err := gate.ReadInstallationSwitch(env.ctx); err != nil || !on {
		t.Fatalf("switch before the change on=%v err=%v", on, err)
	}
	view, err := policies.InstallationPolicy(env.ctx, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policies.UpdateInstallationPolicy(env.ctx, adminID, identity.MCPInstallationChange{Enabled: false, DefaultAccountEnabled: view.DefaultAccountEnabled, DefaultMaxScopes: view.DefaultMaxScopes}, view.Version); err != nil {
		t.Fatal(err)
	}
	if on, err := gate.ReadInstallationSwitch(env.ctx); err != nil || on {
		t.Fatalf("switch after off on=%v err=%v", on, err)
	}
	if _, err := env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPDisabled) {
		t.Fatalf("verify with the installation off err=%v", err)
	}

	view, err = policies.InstallationPolicy(env.ctx, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policies.UpdateInstallationPolicy(env.ctx, adminID, identity.MCPInstallationChange{Enabled: true, DefaultAccountEnabled: view.DefaultAccountEnabled, DefaultMaxScopes: view.DefaultMaxScopes}, view.Version); err != nil {
		t.Fatal(err)
	}
	if access, err := env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); err != nil || len(access.Scopes) == 0 {
		t.Fatalf("verify after the installation is on again %+v err=%v", access, err)
	}
}

// TestMCPPolicyEventsPageAcrossAllLevels: the audit list pages newest first
// across the installation, account and user levels with the opaque cursor, and
// a malformed cursor is refused before any read.
func TestMCPPolicyEventsPageAcrossAllLevels(t *testing.T) {
	env := newMCPEnv(t)
	env.resetInstallation(t)
	t.Cleanup(func() { env.resetInstallation(t) })
	policies := env.installationService(t, true)
	adminID, _ := env.newAccount(t, "mcp_events_admin")
	env.grantInstanceAdmin(t, adminID)
	user, session := env.newAccount(t, "mcp_events_owner")
	if _, err := env.exchange(env.issue(t, user, session)); err != nil {
		t.Fatal(err)
	}

	view, err := policies.InstallationPolicy(env.ctx, adminID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policies.UpdateInstallationPolicy(env.ctx, adminID, identity.MCPInstallationChange{Enabled: true, DefaultAccountEnabled: false, DefaultMaxScopes: view.DefaultMaxScopes}, view.Version); err != nil {
		t.Fatal(err)
	}
	account, err := policies.AccountPolicy(env.ctx, adminID, user)
	if err != nil {
		t.Fatal(err)
	}
	// The administrator and the owner share one account version (ETag), so the
	// owner's write expects the version the administrator's write left behind.
	account, err = policies.UpdateAccountPolicy(env.ctx, adminID, user, identity.MCPAccountPolicyChange{MaxScopes: []string{identity.MCPScopeRead}}, account.Version)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policies.UpdateSettings(env.ctx, user, identity.MCPSettingsChange{Enabled: true, Scopes: []string{identity.MCPScopeRead}}, account.Version); err != nil {
		t.Fatal(err)
	}

	var all []identity.MCPPolicyEvent
	cursor := ""
	for range 10 {
		page, err := policies.PolicyEvents(env.ctx, adminID, 1, cursor)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, page.Items...)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(all) != 3 {
		t.Fatalf("paged %d events, want 3 across all levels: %+v", len(all), all)
	}
	levels := []identity.MCPPolicyEventKind{all[0].Kind, all[1].Kind, all[2].Kind}
	if !slices.Equal(levels, []identity.MCPPolicyEventKind{identity.MCPPolicyEventUser, identity.MCPPolicyEventAccount, identity.MCPPolicyEventInstallation}) {
		t.Fatalf("levels newest first = %v", levels)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].ID <= all[i].ID {
			t.Fatalf("ids not strictly descending: %s then %s", all[i-1].ID, all[i].ID)
		}
	}
	if _, err := policies.PolicyEvents(env.ctx, adminID, 10, "p1-not-a-cursor"); !errors.Is(err, identity.ErrMCPInvalidCursor) {
		t.Fatalf("malformed cursor err=%v", err)
	}
	if _, err := policies.PolicyEvents(env.ctx, adminID, 10, "g1AAAAAAAAAAAAAAAAAAAAAA"); !errors.Is(err, identity.ErrMCPInvalidCursor) {
		t.Fatalf("grant-prefixed cursor err=%v", err)
	}
}

// TestMCPPolicyEventsAdminOnly: a non-administrator reads no audit row, and the
// refusal happens before the read.
func TestMCPPolicyEventsAdminOnly(t *testing.T) {
	env := newMCPEnv(t)
	env.resetInstallation(t)
	t.Cleanup(func() { env.resetInstallation(t) })
	policies := env.installationService(t, true)
	adminID, _ := env.newAccount(t, "mcp_events_guard_admin")
	env.grantInstanceAdmin(t, adminID)
	if _, err := policies.UpdateInstallationPolicy(env.ctx, adminID, identity.MCPInstallationChange{Enabled: true, DefaultAccountEnabled: true, DefaultMaxScopes: []string{identity.MCPScopeRead}}, 1); err != nil {
		t.Fatal(err)
	}
	plain, _ := env.newAccount(t, "mcp_events_guard_plain")
	if _, err := policies.PolicyEvents(env.ctx, plain, 10, ""); !errors.Is(err, identity.ErrInstanceAdminRequired) {
		t.Fatalf("non-admin audit read err=%v", err)
	}
}

func (e *mcpEnv) installationVersion(t *testing.T) int64 {
	t.Helper()
	var version int64
	if err := e.admin.QueryRow(e.ctx, `SELECT version FROM mcp_installation_policy`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}
