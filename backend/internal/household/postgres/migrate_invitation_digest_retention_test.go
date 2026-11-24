package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertV30Invitation writes one invitation in the version 30 shape (digest
// NOT NULL) and returns its ID. state selects pending, expired, accepted or
// revoked; the digest is derived from the creation key.
func insertV30Invitation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, householdID, creatorID, key, state string) string {
	t.Helper()
	accepted, revoked, expires := "NULL", "NULL", "now()+interval '6 days'"
	switch state {
	case "expired":
		expires = "now()-interval '1 hour'"
	case "accepted":
		accepted = "now()-interval '1 day'"
	case "revoked":
		revoked = "now()-interval '1 day'"
	case "pending":
	default:
		t.Fatalf("unknown invitation state %q", state)
	}
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at,accepted_at,revoked_at)
		VALUES ($1::uuid,$2::uuid,$3,sha256(convert_to($3,'UTF8')),now()-interval '2 days',`+expires+`,`+accepted+`,`+revoked+`) RETURNING id::text`,
		householdID, creatorID, key).Scan(&id); err != nil {
		t.Fatalf("insert %s invitation: %v", state, err)
	}
	return id
}

// TestMigrationThirtyOneClearsV4DigestsAndKeepsHistory presents a populated
// version 30 database (the v4.0.0 invitation shape) with pending, expired,
// accepted and revoked invitations, migrates to 31, and proves: every row is
// kept with its timestamps, only the still-pending invitation keeps its digest,
// the storage guards and runtime privileges are in place, and Down restores
// NOT NULL without making any cleared row usable again.
func TestMigrationThirtyOneClearsV4DigestsAndKeepsHistory(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion31SQL); err != nil {
		t.Fatalf("reverse 031: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 30)
	if got := migrationString(t, ctx, admin, `SELECT is_nullable FROM information_schema.columns
		WHERE table_schema='public' AND table_name='household_invitations' AND column_name='token_hash'`); got != "NO" {
		t.Fatalf("version 30 token_hash nullable=%s", got)
	}
	house := insertMigrationHousehold(t, ctx, admin, "Invitation retention")
	owner := insertMigrationAccount(t, ctx, admin, "invitation_retention_owner")
	ids := map[string]string{}
	for _, state := range []string{"pending", "expired", "accepted", "revoked"} {
		ids[state] = insertV30Invitation(t, ctx, admin, house, owner, "v4-"+state, state)
	}
	historyQuery := `SELECT string_agg(creation_key||'|'||created_at::text||'|'||expires_at::text||'|'||coalesce(accepted_at::text,'-')||'|'||coalesce(revoked_at::text,'-'), ',' ORDER BY creation_key) FROM household_invitations`
	history := migrationString(t, ctx, admin, historyQuery)

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate 30 to 31: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 32)
	if got := migrationString(t, ctx, admin, historyQuery); got != history {
		t.Fatalf("invitation history changed by 031:\nbefore=%s\nafter=%s", history, got)
	}
	if got := migrationString(t, ctx, admin, `SELECT string_agg(creation_key||':'||(token_hash IS NOT NULL)::text, ',' ORDER BY creation_key) FROM household_invitations`); got != "v4-accepted:false,v4-expired:false,v4-pending:true,v4-revoked:false" {
		t.Fatalf("digests after 031=%s", got)
	}
	if !migrationRelationExists(t, ctx, admin, "household_invitations_digest_expires_at_idx") {
		t.Fatal("digest expiry index missing after 031")
	}
	var clearOK, deleteOK, publicOK bool
	if err := admin.QueryRow(ctx, `SELECT has_column_privilege('tendo','household_invitations','token_hash','UPDATE'),
		has_table_privilege('tendo','household_invitations','DELETE'),
		has_function_privilege('public','public.tendo_invitation_digest_clear_only()','EXECUTE')`).Scan(&clearOK, &deleteOK, &publicOK); err != nil {
		t.Fatal(err)
	}
	if !clearOK || deleteOK || publicOK {
		t.Fatalf("privileges: clear=%t delete=%t public-execute=%t", clearOK, deleteOK, publicOK)
	}
	_, err := admin.Exec(ctx, `UPDATE household_invitations SET revoked_at=now() WHERE id=$1::uuid`, ids["pending"])
	requireSQLState(t, "23514", err)
	_, err = admin.Exec(ctx, `UPDATE household_invitations SET token_hash=decode(repeat('cd',32),'hex') WHERE id=$1::uuid`, ids["expired"])
	requireSQLState(t, "42501", err)

	if _, err := admin.Exec(ctx, testpostgres.RevertVersion31SQL); err != nil {
		t.Fatalf("reverse 031 with cleared digests: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 30)
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM household_invitations WHERE token_hash IS NULL`); got != 0 {
		t.Fatalf("rows without digest after Down=%d", got)
	}
	if got := migrationString(t, ctx, admin, historyQuery); got != history {
		t.Fatalf("invitation history changed by Down: %s", got)
	}
	// Down never restores a usable token: the cleared rows get digests nobody
	// holds, and the pending row keeps its original digest.
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM household_invitations WHERE token_hash = sha256(convert_to(creation_key,'UTF8'))`); got != 1 {
		t.Fatalf("rows with their original digest after Down=%d want only the pending one", got)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-apply 031: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 32)
	if got := migrationInt(t, ctx, admin, `SELECT count(token_hash) FROM household_invitations`); got != 1 {
		t.Fatalf("digests after re-apply=%d want 1", got)
	}
}
