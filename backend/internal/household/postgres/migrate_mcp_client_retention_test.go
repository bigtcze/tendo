package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
)

// mcpRetentionIndexes are the six indexes migration 029 creates for the
// registration cap, the retention sweep and the expiry deletes (ADR 0023).
var mcpRetentionIndexes = []string{
	"mcp_clients_created_at_idx",
	"mcp_authorization_codes_client_id_idx",
	"mcp_access_tokens_expires_at_idx",
	"mcp_refresh_tokens_expires_at_idx",
	"mcp_authorization_codes_expires_at_idx",
	"mcp_grants_expires_at_idx",
}

// TestMigrationTwentyNineRetentionPrivilegesAndIndexes covers migration 029:
// the runtime role may INSERT and DELETE client rows and UPDATE only
// last_used_at, and the six retention indexes exist.
func TestMigrationTwentyNineRetentionPrivilegesAndIndexes(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	requireSchemaVersion(t, ctx, admin, 31)
	for _, name := range mcpRetentionIndexes {
		if !migrationRelationExists(t, ctx, admin, name) {
			t.Fatalf("index %s missing after migrate to 31", name)
		}
	}
	for _, privilege := range []string{"SELECT", "INSERT", "DELETE"} {
		var got bool
		if err := admin.QueryRow(ctx, `SELECT has_table_privilege('tendo','mcp_clients',$1)`, privilege).Scan(&got); err != nil || !got {
			t.Fatalf("tendo %s mcp_clients = %v (err=%v), want true", privilege, got, err)
		}
	}
	var updatableLastUsed, updatableName bool
	if err := admin.QueryRow(ctx, `SELECT has_column_privilege('tendo','mcp_clients','last_used_at','UPDATE'),
		has_column_privilege('tendo','mcp_clients','client_name','UPDATE')`).Scan(&updatableLastUsed, &updatableName); err != nil {
		t.Fatal(err)
	}
	if !updatableLastUsed || updatableName {
		t.Fatalf("tendo UPDATE on mcp_clients: last_used_at=%v client_name=%v, want true/false", updatableLastUsed, updatableName)
	}
}

// TestMigrationTwentyNineDownRevokesAndKeepsRows reverses 029 while client,
// grant and token rows exist. The Down removes only its own privileges and
// indexes; no registration or grant row is lost, and Up re-applies cleanly.
func TestMigrationTwentyNineDownRevokesAndKeepsRows(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	user := insertMigrationAccount(t, ctx, admin, "mcp_retention_user")
	insertMCPClient(t, ctx, admin)
	grant := insertMCPGrant(t, ctx, admin, user)
	insertMCPRows(t, ctx, admin, user, grant)
	clients := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_clients`)

	if _, err := admin.Exec(ctx, testpostgres.RevertVersion29SQL); err != nil {
		t.Fatalf("reverse 029: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 28)
	for _, name := range mcpRetentionIndexes {
		if migrationRelationExists(t, ctx, admin, name) {
			t.Fatalf("index %s survived Down", name)
		}
	}
	var insertOrDelete, updateLastUsed bool
	if err := admin.QueryRow(ctx, `SELECT has_table_privilege('tendo','mcp_clients','INSERT') OR has_table_privilege('tendo','mcp_clients','DELETE'),
		has_column_privilege('tendo','mcp_clients','last_used_at','UPDATE')`).Scan(&insertOrDelete, &updateLastUsed); err != nil || insertOrDelete || updateLastUsed {
		t.Fatalf("tendo still holds INSERT/DELETE or UPDATE(last_used_at) on mcp_clients after Down: insertOrDelete=%v updateLastUsed=%v err=%v", insertOrDelete, updateLastUsed, err)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_clients`); got != clients {
		t.Fatalf("mcp_clients after Down=%d want %d", got, clients)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM mcp_grants`); got != 1 {
		t.Fatalf("mcp_grants after Down=%d want 1", got)
	}

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-apply 029: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 31)
	for _, name := range mcpRetentionIndexes {
		if !migrationRelationExists(t, ctx, admin, name) {
			t.Fatalf("index %s missing after re-apply", name)
		}
	}
}
