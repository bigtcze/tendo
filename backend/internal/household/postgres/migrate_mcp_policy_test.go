package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// mcpPolicyTables are the three relations migration 032 creates (#457).
var mcpPolicyTables = []string{"mcp_installation_policy", "mcp_account_policies", "mcp_policy_events"}

// mcpPolicyPrivileges is the exact runtime privilege set per table: the
// installation row is updated, never inserted or deleted; account rows are
// inserted and updated, never deleted; events are append-only.
var mcpPolicyPrivileges = []struct {
	table                          string
	selectOK, insertOK, updateOK   bool
	deleteOK, truncateOK, columnOK bool
}{
	{table: "mcp_installation_policy", selectOK: true, columnOK: true},
	{table: "mcp_account_policies", selectOK: true, insertOK: true, columnOK: true},
	{table: "mcp_policy_events", selectOK: true, insertOK: true},
}

// asRuntimeRole runs work in one committed transaction as the restricted
// runtime login, connected to the fixture's own database.
func asRuntimeRole(t *testing.T, ctx context.Context, admin *pgxpool.Pool, work func(pgx.Tx) error) error {
	t.Helper()
	appURL := os.Getenv("TEST_DATABASE_URL")
	if appURL == "" {
		t.Fatal("TEST_DATABASE_URL is required for the runtime role")
	}
	conn, err := pgx.ConnectConfig(ctx, testpostgres.PoolConfigForDatabase(t, appURL, migrationString(t, ctx, admin, `SELECT current_database()`)).ConnConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var role string
	if err := conn.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil || role != testpostgres.MustRuntimeRole(t) {
		t.Fatalf("runtime connection user=%q err=%v", role, err)
	}
	return pgx.BeginFunc(ctx, conn, work)
}

// execWithArgs passes the arguments only to a statement that names $1.
func execWithArgs(ctx context.Context, admin *pgxpool.Pool, sql string, args ...any) error {
	if !strings.Contains(sql, "$1") {
		args = nil
	}
	_, err := admin.Exec(ctx, sql, args...)
	return err
}

func requireMCPPolicyAbsent(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, name := range mcpPolicyTables {
		if migrationRelationExists(t, ctx, pool, name) {
			t.Fatalf("%s exists at version 31", name)
		}
	}
	if got := migrationInt(t, ctx, pool, `SELECT count(*) FROM pg_proc WHERE proname='tendo_mcp_account_policy_default'`); got != 0 {
		t.Fatal("trigger function survived Down")
	}
}

// requireMCPReasonAdmin asserts whether both reason CHECKs admit 'admin'.
func requireMCPReasonAdmin(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want bool) {
	t.Helper()
	for _, c := range mcpReasonConstraints {
		def := migrationString(t, ctx, pool, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, c.table, c.name)
		if strings.Contains(def, "'admin'") != want || !strings.Contains(def, "'client'") {
			t.Fatalf("%s admits admin=%v want %v (client kept): %s", c.name, !want, want, def)
		}
	}
}

// TestMigrationThirtyTwoUpAndPrivileges covers migration 032: the singleton
// installation row with the permissive defaults, one default account row per
// account (backfilled and inserted by the trigger for the runtime role), the
// runtime privileges, no PUBLIC access, the CHECKs and the 'admin' reason.
func TestMigrationThirtyTwoUpAndPrivileges(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	requireSchemaVersion(t, ctx, admin, 32)
	if got := migrationString(t, ctx, admin, `SELECT enabled::text||'|'||default_account_enabled::text||'|'||array_to_string(default_max_scopes,',')||'|'||version::text||'|'||(updated_at IS NULL)::text FROM mcp_installation_policy`); got != "true|true|tendo:read,tendo:write|1|true" {
		t.Fatalf("installation row=%s", got)
	}
	for _, p := range mcpPolicyPrivileges {
		var sel, ins, upd, del, trunc, col bool
		if err := admin.QueryRow(ctx, `SELECT has_table_privilege('tendo',$1,'SELECT'), has_table_privilege('tendo',$1,'INSERT'),
			has_table_privilege('tendo',$1,'UPDATE'), has_table_privilege('tendo',$1,'DELETE'), has_table_privilege('tendo',$1,'TRUNCATE'),
			has_any_column_privilege('tendo',$1,'UPDATE')`, p.table).Scan(&sel, &ins, &upd, &del, &trunc, &col); err != nil {
			t.Fatal(err)
		}
		if sel != p.selectOK || ins != p.insertOK || upd != p.updateOK || del != p.deleteOK || trunc != p.truncateOK || col != p.columnOK {
			t.Fatalf("%s privileges select=%v insert=%v update=%v delete=%v truncate=%v column-update=%v", p.table, sel, ins, upd, del, trunc, col)
		}
		if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM pg_class c, aclexplode(c.relacl) a WHERE c.oid=$1::regclass AND a.grantee=0`, p.table); got != 0 {
			t.Fatalf("PUBLIC holds %d privileges on %s", got, p.table)
		}
	}
	var userIDUpdate bool
	if err := admin.QueryRow(ctx, `SELECT has_column_privilege('tendo','mcp_account_policies','user_id','UPDATE')`).Scan(&userIDUpdate); err != nil {
		t.Fatal(err)
	}
	if userIDUpdate {
		t.Fatal("runtime role may move a policy to another account")
	}
	requireMCPReasonAdmin(t, ctx, admin, true)

	// The runtime role's account insert gets its default policy row from the trigger.
	var user string
	if err := asRuntimeRole(t, ctx, admin, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('mcp_policy_trigger') RETURNING id::text`).Scan(&user)
	}); err != nil {
		t.Fatalf("runtime account insert: %v", err)
	}
	if got := migrationString(t, ctx, admin, `SELECT (admin_enabled IS NULL)::text||'|'||(admin_max_scopes IS NULL)::text||'|'||user_enabled::text||'|'||array_to_string(user_scopes,',')||'|'||version::text FROM mcp_account_policies WHERE user_id=$1::uuid`, user); got != "true|true|true|tendo:read,tendo:write|1" {
		t.Fatalf("default account row=%s", got)
	}

	for _, tc := range []struct{ name, sql string }{
		{"second installation row", `INSERT INTO mcp_installation_policy(singleton) VALUES (false)`},
		{"empty default maximum", `UPDATE mcp_installation_policy SET default_max_scopes='{}'`},
		{"unknown default scope", `UPDATE mcp_installation_policy SET default_max_scopes=ARRAY['tendo:admin']`},
		{"zero installation version", `UPDATE mcp_installation_policy SET version=0`},
		{"empty admin maximum", `UPDATE mcp_account_policies SET admin_max_scopes='{}'`},
		{"unknown admin scope", `UPDATE mcp_account_policies SET admin_max_scopes=ARRAY['tendo:read','tendo:admin']`},
		{"empty user scopes", `UPDATE mcp_account_policies SET user_scopes='{}'`},
		{"null user scopes", `UPDATE mcp_account_policies SET user_scopes=NULL`},
		{"duplicate account row", `INSERT INTO mcp_account_policies(user_id) SELECT user_id FROM mcp_account_policies LIMIT 1`},
		{"installation event with target", `INSERT INTO mcp_policy_events(actor_user_id,target_user_id,policy,enabled,default_account_enabled,scopes,occurred_at) VALUES ($1::uuid,$1::uuid,'installation',true,true,ARRAY['tendo:read'],now())`},
		{"installation event without default", `INSERT INTO mcp_policy_events(actor_user_id,policy,enabled,scopes,occurred_at) VALUES ($1::uuid,'installation',true,ARRAY['tendo:read'],now())`},
		{"account event without target", `INSERT INTO mcp_policy_events(actor_user_id,policy,occurred_at) VALUES ($1::uuid,'account',now())`},
		{"account event with default", `INSERT INTO mcp_policy_events(actor_user_id,target_user_id,policy,default_account_enabled,occurred_at) VALUES ($1::uuid,$1::uuid,'account',true,now())`},
		{"user event for another account", `INSERT INTO mcp_policy_events(actor_user_id,target_user_id,policy,enabled,scopes,occurred_at) SELECT $1::uuid,user_id,'user',true,ARRAY['tendo:read'],now() FROM mcp_account_policies WHERE user_id<>$1::uuid LIMIT 1`},
		{"user event without scopes", `INSERT INTO mcp_policy_events(actor_user_id,target_user_id,policy,enabled,occurred_at) VALUES ($1::uuid,$1::uuid,'user',true,now())`},
		{"event with unknown scope", `INSERT INTO mcp_policy_events(actor_user_id,target_user_id,policy,enabled,scopes,occurred_at) VALUES ($1::uuid,$1::uuid,'user',true,ARRAY['tendo:admin'],now())`},
		{"event without actor", `INSERT INTO mcp_policy_events(target_user_id,policy,enabled,scopes,occurred_at) VALUES ($1::uuid,'account',true,ARRAY['tendo:read'],now())`},
		{"unknown event policy", `INSERT INTO mcp_policy_events(actor_user_id,target_user_id,policy,enabled,scopes,occurred_at) VALUES ($1::uuid,$1::uuid,'household',true,ARRAY['tendo:read'],now())`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			insertMigrationAccount(t, ctx, admin, "mcp_policy_other_"+strings.ReplaceAll(tc.name, " ", "_"))
			var args []any
			if strings.Contains(tc.sql, "$1") {
				args = append(args, user)
			}
			tag, err := admin.Exec(ctx, tc.sql, args...)
			if err == nil {
				t.Fatalf("accepted (%d rows); a CHECK or key must refuse it", tag.RowsAffected())
			}
		})
	}
	for _, sql := range []string{
		`INSERT INTO mcp_policy_events(actor_user_id,policy,enabled,default_account_enabled,scopes,occurred_at) VALUES ($1::uuid,'installation',false,true,ARRAY['tendo:read'],now())`,
		`INSERT INTO mcp_policy_events(actor_user_id,target_user_id,policy,occurred_at) VALUES ($1::uuid,$1::uuid,'account',now())`,
		`INSERT INTO mcp_policy_events(actor_user_id,target_user_id,policy,enabled,scopes,occurred_at) VALUES ($1::uuid,$1::uuid,'user',false,ARRAY['tendo:write'],now())`,
	} {
		if err := asRuntimeRole(t, ctx, admin, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, sql, user); return e }); err != nil {
			t.Fatalf("valid event refused: %v\n%s", err, sql)
		}
	}
	for _, sql := range []string{
		`UPDATE mcp_policy_events SET enabled=true`,
		`DELETE FROM mcp_policy_events`,
		`DELETE FROM mcp_account_policies`,
		`DELETE FROM mcp_installation_policy`,
		`INSERT INTO mcp_installation_policy(singleton) VALUES (true) ON CONFLICT DO NOTHING`,
		`UPDATE mcp_account_policies SET user_id=user_id`,
	} {
		if err := asRuntimeRole(t, ctx, admin, func(tx pgx.Tx) error { _, e := tx.Exec(ctx, sql); return e }); err == nil {
			t.Fatalf("runtime role allowed: %s", sql)
		}
	}
}

// TestMigrationThirtyTwoUpgradeKeepsEffectiveScopes presents a version 31
// database with live and revoked grants, migrates to 32 and proves every
// existing account gets the default (unrestricted) account row, no grant or
// event changes, and a new account still gets its row.
func TestMigrationThirtyTwoUpgradeKeepsEffectiveScopes(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion32SQL); err != nil {
		t.Fatalf("reverse 032: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 31)
	requireMCPPolicyAbsent(t, ctx, admin)
	requireMCPReasonAdmin(t, ctx, admin, false)

	insertMCPClient(t, ctx, admin)
	live := insertMigrationAccount(t, ctx, admin, "mcp_policy_upgrade_live")
	revoked := insertMigrationAccount(t, ctx, admin, "mcp_policy_upgrade_revoked")
	insertMigrationAccount(t, ctx, admin, "mcp_policy_upgrade_none")
	liveGrant := insertMCPGrant(t, ctx, admin, live)
	insertMCPRows(t, ctx, admin, live, liveGrant)
	revokedGrant := insertMCPGrant(t, ctx, admin, revoked)
	if _, err := admin.Exec(ctx, `UPDATE mcp_grants SET revoked_at=now(), revoke_reason='client' WHERE id=$1::uuid`, revokedGrant); err != nil {
		t.Fatal(err)
	}
	grantsQuery := `SELECT string_agg(id::text||'|'||array_to_string(scopes,',')||'|'||coalesce(revoke_reason,'-'), ',' ORDER BY id) FROM mcp_grants`
	grants := migrationString(t, ctx, admin, grantsQuery)
	events := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_grant_events`)
	accounts := migrationInt(t, ctx, admin, `SELECT count(*) FROM user_accounts`)

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate 31 to 32: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 32)
	if got := migrationString(t, ctx, admin, grantsQuery); got != grants {
		t.Fatalf("grants changed by 032:\nbefore=%s\nafter=%s", grants, got)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_grant_events`); got != events {
		t.Fatalf("grant events=%d want %d", got, events)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_account_policies p JOIN user_accounts u ON u.id=p.user_id
		WHERE p.admin_enabled IS NULL AND p.admin_max_scopes IS NULL AND p.user_enabled
		AND p.user_scopes @> ARRAY['tendo:read','tendo:write'] AND p.version=1`); got != accounts || accounts != 3 {
		t.Fatalf("default account rows=%d want one per account (%d)", got, accounts)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_policy_events`); got != 0 {
		t.Fatalf("policy events after upgrade=%d", got)
	}
	// The effective scopes of the live grant are its consented scopes.
	if got := migrationString(t, ctx, admin, `SELECT array_to_string(ARRAY(SELECT s FROM unnest(g.scopes) s
		WHERE i.enabled AND coalesce(p.admin_enabled,i.default_account_enabled) AND p.user_enabled
		AND s = ANY(coalesce(p.admin_max_scopes,i.default_max_scopes)) AND s = ANY(p.user_scopes) ORDER BY s), ',')
		FROM mcp_grants g JOIN mcp_account_policies p ON p.user_id=g.user_id CROSS JOIN mcp_installation_policy i WHERE g.id=$1::uuid`, liveGrant); got != "tendo:read,tendo:write" {
		t.Fatalf("effective scopes after upgrade=%s", got)
	}
	fresh := insertMigrationAccount(t, ctx, admin, "mcp_policy_upgrade_fresh")
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_account_policies WHERE user_id=$1::uuid`, fresh); got != 1 {
		t.Fatalf("new account policy rows=%d", got)
	}
}

// TestMigrationThirtyTwoDownRefusesOnceUsed proves Down refuses while any
// administrator revocation, policy event or non-default policy exists (a
// rollback would widen access or falsify the audited cause), and succeeds and
// re-applies on an unused policy.
func TestMigrationThirtyTwoDownRefusesOnceUsed(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	insertMCPClient(t, ctx, admin)
	user := insertMigrationAccount(t, ctx, admin, "mcp_policy_down_user")
	grant := insertMCPGrant(t, ctx, admin, user)

	for _, tc := range []struct{ name, set, reset string }{
		{"admin revocation", `UPDATE mcp_grants SET revoked_at=now(), revoke_reason='admin' WHERE id=$1::uuid`,
			`UPDATE mcp_grants SET revoked_at=NULL, revoke_reason=NULL WHERE id=$1::uuid`},
		{"admin revocation event", `INSERT INTO mcp_grant_events(grant_id,user_id,client_id,client_name,event,reason,occurred_at) SELECT id,user_id,client_id,'Fixture client','revoked','admin',now() FROM mcp_grants WHERE id=$1::uuid`,
			`DELETE FROM mcp_grant_events WHERE grant_id=$1::uuid`},
		{"policy event", `INSERT INTO mcp_policy_events(actor_user_id,target_user_id,policy,occurred_at) SELECT user_id,user_id,'account',now() FROM mcp_grants WHERE id=$1::uuid`,
			`DELETE FROM mcp_policy_events WHERE $1::uuid IS NOT NULL`},
		{"installation off", `UPDATE mcp_installation_policy SET enabled=false WHERE $1::uuid IS NOT NULL`, `UPDATE mcp_installation_policy SET enabled=true WHERE $1::uuid IS NOT NULL`},
		{"accounts off by default", `UPDATE mcp_installation_policy SET default_account_enabled=false WHERE $1::uuid IS NOT NULL`, `UPDATE mcp_installation_policy SET default_account_enabled=true WHERE $1::uuid IS NOT NULL`},
		{"default maximum read", `UPDATE mcp_installation_policy SET default_max_scopes=ARRAY['tendo:read'] WHERE $1::uuid IS NOT NULL`, `UPDATE mcp_installation_policy SET default_max_scopes=ARRAY['tendo:read','tendo:write'] WHERE $1::uuid IS NOT NULL`},
		{"admin enabled set", `UPDATE mcp_account_policies SET admin_enabled=true WHERE user_id=(SELECT user_id FROM mcp_grants WHERE id=$1::uuid)`, `UPDATE mcp_account_policies SET admin_enabled=NULL`},
		{"admin maximum set", `UPDATE mcp_account_policies SET admin_max_scopes=ARRAY['tendo:read','tendo:write'] WHERE user_id=(SELECT user_id FROM mcp_grants WHERE id=$1::uuid)`, `UPDATE mcp_account_policies SET admin_max_scopes=NULL`},
		{"user off", `UPDATE mcp_account_policies SET user_enabled=false WHERE user_id=(SELECT user_id FROM mcp_grants WHERE id=$1::uuid)`, `UPDATE mcp_account_policies SET user_enabled=true`},
		{"user narrowed", `UPDATE mcp_account_policies SET user_scopes=ARRAY['tendo:write'] WHERE user_id=(SELECT user_id FROM mcp_grants WHERE id=$1::uuid)`, `UPDATE mcp_account_policies SET user_scopes=ARRAY['tendo:read','tendo:write']`},
		{"account row missing", `DELETE FROM mcp_account_policies WHERE user_id=(SELECT user_id FROM mcp_grants WHERE id=$1::uuid)`,
			`INSERT INTO mcp_account_policies(user_id) SELECT user_id FROM mcp_grants WHERE id=$1::uuid`},
		{"installation row missing", `DELETE FROM mcp_installation_policy WHERE $1::uuid IS NOT NULL`, `INSERT INTO mcp_installation_policy(singleton) SELECT true WHERE $1::uuid IS NOT NULL`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := execWithArgs(ctx, admin, tc.set, grant); err != nil {
				t.Fatalf("set: %v", err)
			}
			_, err := admin.Exec(ctx, testpostgres.RevertVersion32SQL)
			if err == nil || !strings.Contains(err.Error(), "migration 032 cannot be reversed") {
				t.Fatalf("Down err=%v want refusal", err)
			}
			requireSchemaVersion(t, ctx, admin, 32)
			if err := execWithArgs(ctx, admin, tc.reset, grant); err != nil {
				t.Fatalf("reset: %v", err)
			}
		})
	}

	if _, err := admin.Exec(ctx, testpostgres.RevertVersion32SQL); err != nil {
		t.Fatalf("Down on an unused policy: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 31)
	requireMCPPolicyAbsent(t, ctx, admin)
	requireMCPReasonAdmin(t, ctx, admin, false)
	if _, err := admin.Exec(ctx, `UPDATE mcp_grants SET revoked_at=now(), revoke_reason='admin' WHERE id=$1::uuid`, grant); err == nil {
		t.Fatal("reason 'admin' accepted after Down")
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-apply 032: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 32)
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_account_policies WHERE user_id=$1::uuid`, user); got != 1 {
		t.Fatalf("account row after re-apply=%d", got)
	}
}
