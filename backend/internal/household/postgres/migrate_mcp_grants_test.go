package postgres

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// mcpTables are the six relations migration 028 creates (ADR 0023).
var mcpTables = []string{
	"mcp_clients", "mcp_grants", "mcp_authorization_codes",
	"mcp_access_tokens", "mcp_refresh_tokens", "mcp_grant_events",
}

const (
	mcpTestClientID = "mcpfixtureclient0001"
	mcpTestResource = "https://tendo.example.test/mcp"
	mcpTestRedirect = "https://client.example.test/callback"
)

// mcpTestChallenge is a well-formed PKCE S256 challenge (43 base64url characters).
var mcpTestChallenge = strings.Repeat("A", 43)

// mcpDigest is a 32-byte stand-in for a SHA-256 digest; only uniqueness matters.
func mcpDigest(fill byte) []byte {
	digest := make([]byte, 32)
	for i := range digest {
		digest[i] = fill
	}
	return digest
}

func insertMCPClient(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO mcp_clients(client_id,client_name,redirect_uris,created_at)
		VALUES ($1,'Fixture client',ARRAY[$2],now())`, mcpTestClientID, mcpTestRedirect); err != nil {
		t.Fatal(err)
	}
}

func insertMCPGrant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO mcp_grants(user_id,client_id,scopes,resource,created_at,expires_at)
		VALUES ($1::uuid,$2,ARRAY['tendo:read','tendo:write'],$3,now(),now()+interval '1 day') RETURNING id::text`,
		userID, mcpTestClientID, mcpTestResource).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// insertMCPCode writes a used authorization code that issued grantID.
func insertMCPCode(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, grantID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO mcp_authorization_codes(code_hash,user_id,client_id,redirect_uri,code_challenge,scopes,resource,created_at,expires_at,used_at,issued_grant_id)
		VALUES ($1,$2::uuid,$3,$4,$5,ARRAY['tendo:read'],$6,now(),now()+interval '5 minutes',now(),$7::uuid)`,
		mcpDigest(0xc0), userID, mcpTestClientID, mcpTestRedirect, mcpTestChallenge, mcpTestResource, grantID); err != nil {
		t.Fatal(err)
	}
}

// insertMCPRows writes one row in every MCP table for the given user and grant:
// a used code, an access token, a refresh token and a created event.
func insertMCPRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, grantID string) {
	t.Helper()
	insertMCPCode(t, ctx, pool, userID, grantID)
	if _, err := pool.Exec(ctx, `INSERT INTO mcp_access_tokens(token_hash,grant_id,created_at,expires_at)
		VALUES ($1,$2::uuid,now(),now()+interval '30 minutes')`, mcpDigest(0xa0), grantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO mcp_refresh_tokens(token_hash,grant_id,created_at,expires_at)
		VALUES ($1,$2::uuid,now(),now()+interval '30 days')`, mcpDigest(0x70), grantID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO mcp_grant_events(grant_id,user_id,client_id,client_name,event,occurred_at)
		VALUES ($1::uuid,$2::uuid,$3,'Fixture client','created',now())`, grantID, userID, mcpTestClientID); err != nil {
		t.Fatal(err)
	}
}

// TestMigrationTwentyEightUpAndPrivileges covers migration 028 (ADR 0023): the
// six MCP relations exist and the runtime role holds exactly the privileges the
// migration grants, with none granted to PUBLIC.
func TestMigrationTwentyEightUpAndPrivileges(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}

	requireSchemaVersion(t, ctx, admin, 28)
	for _, table := range mcpTables {
		if !migrationRelationExists(t, ctx, admin, table) {
			t.Fatalf("%s missing after migrate to 28", table)
		}
	}

	for _, grant := range []struct {
		table     string
		allowed   []string // table-level privileges held by tendo
		updatable []string // columns tendo may UPDATE, in any order
	}{
		{"mcp_clients", []string{"SELECT"}, nil},
		{"mcp_grants", []string{"SELECT", "INSERT"}, []string{"revoked_at", "revoke_reason"}},
		{"mcp_authorization_codes", []string{"SELECT", "INSERT", "DELETE"}, []string{"issued_grant_id", "used_at"}},
		{"mcp_access_tokens", []string{"SELECT", "INSERT", "DELETE"}, nil},
		{"mcp_refresh_tokens", []string{"SELECT", "INSERT", "DELETE"}, []string{"consumed_at"}},
		{"mcp_grant_events", []string{"SELECT", "INSERT"}, nil},
	} {
		for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
			want := slices.Contains(grant.allowed, privilege)
			var got bool
			if err := admin.QueryRow(ctx,
				`SELECT has_table_privilege('tendo',$1,$2)`, grant.table, privilege).Scan(&got); err != nil || got != want {
				t.Errorf("tendo %s %s = %v, want %v (err=%v)", privilege, grant.table, got, want, err)
			}
		}

		rows, err := admin.Query(ctx, `SELECT c.column_name::text FROM information_schema.columns c
			WHERE c.table_schema='public' AND c.table_name=$1
			  AND has_column_privilege('tendo', c.table_name::text, c.column_name::text, 'UPDATE')`, grant.table)
		if err != nil {
			t.Fatal(err)
		}
		var updatable []string
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				t.Fatal(err)
			}
			updatable = append(updatable, column)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		slices.Sort(updatable)
		want := slices.Clone(grant.updatable)
		slices.Sort(want)
		if !slices.Equal(updatable, want) {
			t.Fatalf("tendo UPDATE columns on %s = %v want %v", grant.table, updatable, want)
		}

		// Neither the table nor any of its columns may carry a grant to PUBLIC.
		var publicGrants int
		if err := admin.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM pg_class c, aclexplode(c.relacl) a WHERE c.oid=to_regclass('public.'||$1) AND a.grantee=0)
			+ (SELECT count(*) FROM pg_attribute at, aclexplode(at.attacl) a WHERE at.attrelid=to_regclass('public.'||$1) AND a.grantee=0)`,
			grant.table).Scan(&publicGrants); err != nil || publicGrants != 0 {
			t.Fatalf("%s PUBLIC grants=%d err=%v", grant.table, publicGrants, err)
		}
	}
}

// TestMigrationTwentyEightCheckConstraints proves representative CHECK
// constraints refuse malformed rows. Well-formed rows are inserted first so a
// fixture mistake cannot make every refusal pass for the wrong reason.
func TestMigrationTwentyEightCheckConstraints(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	user := insertMigrationAccount(t, ctx, admin, "mcp_check_user")
	insertMCPClient(t, ctx, admin)
	grant := insertMCPGrant(t, ctx, admin, user)
	liveGrant := insertMCPGrant(t, ctx, admin, user)

	// Positive controls: well-formed rows are accepted.
	if _, err := admin.Exec(ctx, `UPDATE mcp_grants SET revoked_at=now(), revoke_reason='user' WHERE id=$1::uuid`, liveGrant); err != nil {
		t.Fatalf("well-formed revocation refused: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO mcp_authorization_codes(code_hash,user_id,client_id,redirect_uri,code_challenge,scopes,resource,created_at,expires_at)
		VALUES ($1,$2::uuid,$3,$4,$5,ARRAY['tendo:read'],$6,now(),now()+interval '5 minutes')`,
		mcpDigest(0x09), user, mcpTestClientID, mcpTestRedirect, mcpTestChallenge, mcpTestResource); err != nil {
		t.Fatalf("well-formed code refused: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO mcp_refresh_tokens(token_hash,grant_id,created_at,expires_at)
		VALUES ($1,$2::uuid,now(),now()+interval '30 days')`, mcpDigest(0x08), grant); err != nil {
		t.Fatalf("well-formed refresh token refused: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO mcp_grant_events(grant_id,user_id,client_id,client_name,event,occurred_at)
		VALUES ($1::uuid,$2::uuid,$3,'Fixture client','created',now())`, grant, user, mcpTestClientID); err != nil {
		t.Fatalf("well-formed created event refused: %v", err)
	}

	cases := []struct {
		name  string
		query string
		args  []any
	}{
		{"scope outside tendo", `INSERT INTO mcp_grants(user_id,client_id,scopes,resource,created_at,expires_at)
			VALUES ($1::uuid,$2,ARRAY['tendo:admin'],$3,now(),now()+interval '1 day')`,
			[]any{user, mcpTestClientID, mcpTestResource}},
		{"grant lifetime over 90 days", `INSERT INTO mcp_grants(user_id,client_id,scopes,resource,created_at,expires_at)
			VALUES ($1::uuid,$2,ARRAY['tendo:read'],$3,now(),now()+interval '91 days')`,
			[]any{user, mcpTestClientID, mcpTestResource}},
		{"grant revoked without a reason", `UPDATE mcp_grants SET revoked_at=now() WHERE id=$1::uuid`,
			[]any{grant}},
		{"grant reason without revocation", `UPDATE mcp_grants SET revoke_reason='user' WHERE id=$1::uuid`,
			[]any{grant}},
		{"refresh token consumed before it was issued", `INSERT INTO mcp_refresh_tokens(token_hash,grant_id,created_at,expires_at,consumed_at)
			VALUES ($1,$2::uuid,now(),now()+interval '30 days',now()-interval '1 hour')`,
			[]any{mcpDigest(0x01), grant}},
		{"refresh token lifetime over 90 days", `INSERT INTO mcp_refresh_tokens(token_hash,grant_id,created_at,expires_at)
			VALUES ($1,$2::uuid,now(),now()+interval '91 days')`,
			[]any{mcpDigest(0x02), grant}},
		{"code used without an issued grant", `INSERT INTO mcp_authorization_codes(code_hash,user_id,client_id,redirect_uri,code_challenge,scopes,resource,created_at,expires_at,used_at)
			VALUES ($1,$2::uuid,$3,$4,$5,ARRAY['tendo:read'],$6,now(),now()+interval '5 minutes',now())`,
			[]any{mcpDigest(0x03), user, mcpTestClientID, mcpTestRedirect, mcpTestChallenge, mcpTestResource}},
		{"code lifetime over ten minutes", `INSERT INTO mcp_authorization_codes(code_hash,user_id,client_id,redirect_uri,code_challenge,scopes,resource,created_at,expires_at)
			VALUES ($1,$2::uuid,$3,$4,$5,ARRAY['tendo:read'],$6,now(),now()+interval '11 minutes')`,
			[]any{mcpDigest(0x04), user, mcpTestClientID, mcpTestRedirect, mcpTestChallenge, mcpTestResource}},
		{"created event with a reason", `INSERT INTO mcp_grant_events(grant_id,user_id,client_id,client_name,event,reason,occurred_at)
			VALUES ($1::uuid,$2::uuid,$3,'Fixture client','created','user',now())`,
			[]any{grant, user, mcpTestClientID}},
		{"revoked event without a reason", `INSERT INTO mcp_grant_events(grant_id,user_id,client_id,client_name,event,occurred_at)
			VALUES ($1::uuid,$2::uuid,$3,'Fixture client','revoked',now())`,
			[]any{grant, user, mcpTestClientID}},
	}
	for _, tc := range cases {
		_, err := admin.Exec(ctx, tc.query, tc.args...)
		t.Run(tc.name, func(t *testing.T) { requireSQLState(t, "23514", err) })
	}
}

// TestMigrationTwentyEightLifetimeIsDSTSafe: the grant and refresh lifetime
// CHECKs use a fixed 2160-hour span, not calendar days. On a session in
// Europe/Prague, where a day can be 23 or 25 hours, a 2160-hour span must still
// be accepted and 2160 hours plus one second refused.
func TestMigrationTwentyEightLifetimeIsDSTSafe(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	user := insertMigrationAccount(t, ctx, admin, "mcp_dst_user")
	insertMCPClient(t, ctx, admin)

	// A dedicated pooled connection pinned to a DST zone, the setting the
	// production sessions could inherit.
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `RESET TimeZone`)
		conn.Release()
	}()
	if _, err = conn.Exec(ctx, `SET TimeZone='Europe/Prague'`); err != nil {
		t.Fatal(err)
	}
	// 2026-01-01 12:00Z is far from a transition, but the 90-day span crosses
	// the 2026-03-29 switch, so calendar-day arithmetic would lose an hour.
	const created = `timestamptz '2026-01-01 12:00:00+00'`

	var grant string
	if err = conn.QueryRow(ctx, `INSERT INTO mcp_grants(user_id,client_id,scopes,resource,created_at,expires_at)
		VALUES ($1::uuid,$2,ARRAY['tendo:read'],$3,`+created+`,`+created+`+interval '2160 hours') RETURNING id::text`,
		user, mcpTestClientID, mcpTestResource).Scan(&grant); err != nil {
		t.Fatalf("2160h grant refused in Europe/Prague: %v", err)
	}
	if _, err = conn.Exec(ctx, `INSERT INTO mcp_refresh_tokens(token_hash,grant_id,created_at,expires_at)
		VALUES ($1,$2::uuid,`+created+`,`+created+`+interval '2160 hours')`, mcpDigest(0x51), grant); err != nil {
		t.Fatalf("2160h refresh token refused in Europe/Prague: %v", err)
	}

	_, err = conn.Exec(ctx, `INSERT INTO mcp_grants(user_id,client_id,scopes,resource,created_at,expires_at)
		VALUES ($1::uuid,$2,ARRAY['tendo:read'],$3,`+created+`,`+created+`+interval '2160 hours 1 second')`,
		user, mcpTestClientID, mcpTestResource)
	requireSQLState(t, "23514", err)
	_, err = conn.Exec(ctx, `INSERT INTO mcp_refresh_tokens(token_hash,grant_id,created_at,expires_at)
		VALUES ($1,$2::uuid,`+created+`,`+created+`+interval '2160 hours 1 second')`, mcpDigest(0x52), grant)
	requireSQLState(t, "23514", err)
}

// TestMigrationTwentyEightUpgradePreservesRows migrates a populated version 27
// database: existing rows survive and the MCP tables are created empty.
func TestMigrationTwentyEightUpgradePreservesRows(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion28SQL); err != nil {
		t.Fatalf("reverse 028: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 27)
	for _, table := range mcpTables {
		if migrationRelationExists(t, ctx, admin, table) {
			t.Fatalf("%s exists at version 27", table)
		}
	}

	house := insertMigrationHousehold(t, ctx, admin, "mcp upgrade house")
	account := insertMigrationAccount(t, ctx, admin, "mcp_upgrade_user")
	households := migrationInt(t, ctx, admin, `SELECT count(*) FROM households`)
	accounts := migrationInt(t, ctx, admin, `SELECT count(*) FROM user_accounts`)

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("upgrade to 028: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 28)
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM households`); got != households {
		t.Fatalf("households after upgrade=%d want %d", got, households)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM user_accounts`); got != accounts {
		t.Fatalf("user_accounts after upgrade=%d want %d", got, accounts)
	}
	if got := migrationString(t, ctx, admin, `SELECT name FROM households WHERE id=$1::uuid`, house); got != "mcp upgrade house" {
		t.Fatalf("household name after upgrade=%q", got)
	}
	if got := migrationString(t, ctx, admin, `SELECT login FROM user_accounts WHERE id=$1::uuid`, account); got != "mcp_upgrade_user" {
		t.Fatalf("account login after upgrade=%q", got)
	}
	for _, table := range mcpTables {
		if !migrationRelationExists(t, ctx, admin, table) {
			t.Fatalf("%s missing after upgrade", table)
		}
		if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM `+table); got != 0 {
			t.Fatalf("%s rows after upgrade=%d want 0", table, got)
		}
	}
}

// TestMigrationTwentyEightDownDropsGrantsAndReapplies reverses 028 while MCP
// rows exist. The Down is documented as lossy: it drops every MCP table and
// returns to version 27 without touching account data, and Up re-applies.
func TestMigrationTwentyEightDownDropsGrantsAndReapplies(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	user := insertMigrationAccount(t, ctx, admin, "mcp_down_user")
	insertMCPClient(t, ctx, admin)
	grant := insertMCPGrant(t, ctx, admin, user)
	revoked := insertMCPGrant(t, ctx, admin, user)
	insertMCPRows(t, ctx, admin, user, grant)
	if _, err := admin.Exec(ctx, `UPDATE mcp_grants SET revoked_at=now(), revoke_reason='user' WHERE id=$1::uuid`, revoked); err != nil {
		t.Fatalf("revoke grant: %v", err)
	}
	for _, table := range mcpTables {
		if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM `+table); got == 0 {
			t.Fatalf("%s has no rows before Down; the reversal would not cover data", table)
		}
	}

	if _, err := admin.Exec(ctx, testpostgres.RevertVersion28SQL); err != nil {
		t.Fatalf("reverse 028 with MCP rows: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 27)
	for _, table := range mcpTables {
		if migrationRelationExists(t, ctx, admin, table) {
			t.Fatalf("%s survived Down", table)
		}
	}
	if got := migrationString(t, ctx, admin, `SELECT login FROM user_accounts WHERE id=$1::uuid`, user); got != "mcp_down_user" {
		t.Fatalf("account changed by Down: login=%q", got)
	}

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-apply 028: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 28)
	for _, table := range mcpTables {
		if !migrationRelationExists(t, ctx, admin, table) {
			t.Fatalf("%s missing after re-apply", table)
		}
		if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM `+table); got != 0 {
			t.Fatalf("%s rows after lossy Down/Up=%d want 0", table, got)
		}
	}
}
