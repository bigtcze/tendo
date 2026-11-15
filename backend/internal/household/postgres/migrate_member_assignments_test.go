package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrationTwentyFiveUpDownAndPrivileges covers migration 025 (ADR 0020):
// the assignment audit table, the actor-authority columns with their CHECK
// constraints and self-leave backfill, the directory index and the runtime
// role privileges. The fixture is its own database, so the shared test schema
// is untouched and left at the current version.
func TestMigrationTwentyFiveUpDownAndPrivileges(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}

	requireSchemaVersion(t, ctx, admin, 27)
	for _, relation := range []string{"household_member_assignments", "user_accounts_lower_login_id_idx"} {
		if !migrationRelationExists(t, ctx, admin, relation) {
			t.Fatalf("%s missing after migrate to the current version", relation)
		}
	}

	for _, check := range []struct {
		privilege string
		want      bool
	}{
		{"SELECT", true},
		{"INSERT", true},
		{"UPDATE", false},
		{"DELETE", false},
		{"TRUNCATE", false},
	} {
		var got bool
		if err := admin.QueryRow(ctx,
			`SELECT has_table_privilege('tendo','household_member_assignments',$1)`, check.privilege).Scan(&got); err != nil || got != check.want {
			t.Errorf("tendo %s household_member_assignments = %v, want %v (err=%v)", check.privilege, got, check.want, err)
		}
	}
	var publicGrants int
	if err := admin.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.role_table_grants WHERE table_name='household_member_assignments' AND grantee='PUBLIC'`).Scan(&publicGrants); err != nil || publicGrants != 0 {
		t.Fatalf("PUBLIC table grants=%d err=%v", publicGrants, err)
	}
	for _, table := range []string{"household_role_changes", "household_member_removals"} {
		var tableExists bool
		if err := admin.QueryRow(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, table).Scan(&tableExists); err != nil || !tableExists {
			t.Fatalf("%s exists=%v err=%v", table, tableExists, err)
		}
	}

	for _, table := range []string{"household_role_changes", "household_member_removals"} {
		var columnDefault, nullable string
		err := admin.QueryRow(ctx, `SELECT column_default, is_nullable FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1 AND column_name='actor_authority'`, table).Scan(&columnDefault, &nullable)
		if err != nil || !strings.Contains(columnDefault, "household_admin") || nullable != "NO" {
			t.Fatalf("%s.actor_authority default=%q nullable=%q err=%v", table, columnDefault, nullable, err)
		}
	}

	house := insertMigrationHousehold(t, ctx, admin, "migration 025 house")
	probe := insertMigrationHousehold(t, ctx, admin, "migration 025 probe")
	adminUser := insertMigrationAccount(t, ctx, admin, "migration_025_admin")
	memberUser := insertMigrationAccount(t, ctx, admin, "migration_025_member")
	actorUser := insertMigrationAccount(t, ctx, admin, "migration_025_actor")
	if _, err := admin.Exec(ctx,
		`INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid,$2::uuid,'admin'),($3::uuid,$2::uuid,'member')`,
		adminUser, house, memberUser); err != nil {
		t.Fatal(err)
	}

	// Rejected authorities on all three audited tables.
	_, err := admin.Exec(ctx, `INSERT INTO household_member_assignments
		(household_id,target_user_id,actor_user_id,role,actor_authority,assigned_at)
		VALUES ($1::uuid,$2::uuid,$3::uuid,'member','household_admin',now())`, house, memberUser, adminUser)
	requireSQLState(t, "23514", err)
	_, err = admin.Exec(ctx, `INSERT INTO household_role_changes
		(household_id,target_user_id,actor_user_id,previous_role,new_role,changed_at,actor_authority)
		VALUES ($1::uuid,$2::uuid,$3::uuid,'member','admin',now(),'bogus')`, probe, memberUser, actorUser)
	requireSQLState(t, "23514", err)
	_, err = admin.Exec(ctx, `INSERT INTO household_member_removals
		(household_id,target_user_id,actor_user_id,previous_role,removed_at,actor_authority)
		VALUES ($1::uuid,$2::uuid,$3::uuid,'member',now(),'bogus')`, probe, memberUser, actorUser)
	requireSQLState(t, "23514", err)

	// The accepted authority values still insert.
	if _, err := admin.Exec(ctx, `INSERT INTO household_member_assignments
		(household_id,target_user_id,actor_user_id,role,actor_authority,assigned_at)
		VALUES ($1::uuid,$2::uuid,$3::uuid,'admin','instance_admin',now())`, house, memberUser, actorUser); err != nil {
		t.Fatalf("valid assignment row: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_role_changes
		(household_id,target_user_id,actor_user_id,previous_role,new_role,changed_at,actor_authority)
		VALUES ($1::uuid,$2::uuid,$3::uuid,'member','admin',now(),'instance_admin')`, probe, memberUser, actorUser); err != nil {
		t.Fatalf("valid role change row: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_member_removals
		(household_id,target_user_id,actor_user_id,previous_role,removed_at,actor_authority)
		VALUES ($1::uuid,$2::uuid,$3::uuid,'member',now(),'household_member')`, probe, memberUser, actorUser); err != nil {
		t.Fatalf("valid removal row: %v", err)
	}
	// The probe rows must not take part in the backfill assertions.
	if _, err := admin.Exec(ctx, `DELETE FROM household_role_changes WHERE household_id=$1::uuid`, probe); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `DELETE FROM household_member_removals WHERE household_id=$1::uuid`, probe); err != nil {
		t.Fatal(err)
	}

	// Down reverses the schema, including the index and both authority columns.
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion25SQL); err != nil {
		t.Fatalf("reverse 025: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 24)
	for _, relation := range []string{"household_member_assignments", "user_accounts_lower_login_id_idx"} {
		if migrationRelationExists(t, ctx, admin, relation) {
			t.Fatalf("%s survived Down", relation)
		}
	}
	for _, table := range []string{"household_role_changes", "household_member_removals"} {
		if migrationColumnExists(t, ctx, admin, table, "actor_authority") {
			t.Fatalf("%s.actor_authority survived Down", table)
		}
	}

	// Rows written before the upgrade: the removed member is the actor in a
	// self-leave (member), an admin removed another member, an admin left by
	// themselves, and a role change was made by a household admin.
	selfLeaveMember := insertMigrationRemoval(t, ctx, admin, house, memberUser, memberUser, "member", "2026-01-01T10:00:00Z")
	adminRemoval := insertMigrationRemoval(t, ctx, admin, house, actorUser, adminUser, "member", "2026-01-01T11:00:00Z")
	selfLeaveAdmin := insertMigrationRemoval(t, ctx, admin, house, adminUser, adminUser, "admin", "2026-01-01T12:00:00Z")
	roleChange := insertMigrationRoleChange(t, ctx, admin, house, memberUser, adminUser, "member", "admin")

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-apply 025 over populated version 24 schema: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 27)
	requireRemovalAuthority(t, ctx, admin, selfLeaveMember, "household_member")
	requireRemovalAuthority(t, ctx, admin, adminRemoval, "household_admin")
	requireRemovalAuthority(t, ctx, admin, selfLeaveAdmin, "household_admin")
	if got := migrationString(t, ctx, admin,
		`SELECT actor_authority FROM household_role_changes WHERE id=$1::uuid`, roleChange); got != "household_admin" {
		t.Fatalf("role change authority=%q want household_admin", got)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM household_member_assignments`); got != 0 {
		t.Fatalf("assignments backfilled=%d want 0 (the table starts empty)", got)
	}

	// Down is lossy but keeps the removal and role-change rows.
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion25SQL); err != nil {
		t.Fatalf("second reverse 025: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 24)
	if migrationRelationExists(t, ctx, admin, "household_member_assignments") {
		t.Fatal("assignment table survived the second Down")
	}
	if migrationRelationExists(t, ctx, admin, "user_accounts_lower_login_id_idx") {
		t.Fatal("directory index survived the second Down")
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM household_member_removals WHERE household_id=$1::uuid`, house); got != 3 {
		t.Fatalf("removal rows after Down=%d want 3 (lossy Down keeps rows)", got)
	}

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("third apply of 025: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 27)
	requireRemovalAuthority(t, ctx, admin, selfLeaveMember, "household_member")
	requireRemovalAuthority(t, ctx, admin, adminRemoval, "household_admin")
	requireRemovalAuthority(t, ctx, admin, selfLeaveAdmin, "household_admin")
	if got := migrationString(t, ctx, admin,
		`SELECT actor_authority FROM household_role_changes WHERE id=$1::uuid`, roleChange); got != "household_admin" {
		t.Fatalf("role change authority after round trip=%q", got)
	}

	// A repeated Up on an already current ledger changes nothing.
	before := migrationInt(t, ctx, admin, `SELECT count(*) FROM tendo_schema_migrations`)
	beforeVersion := migrationInt(t, ctx, admin, `SELECT max(version) FROM tendo_schema_migrations`)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("idempotent migrate: %v", err)
	}
	if after := migrationInt(t, ctx, admin, `SELECT count(*) FROM tendo_schema_migrations`); after != before {
		t.Fatalf("ledger rows %d->%d after repeated Up", before, after)
	}
	if after := migrationInt(t, ctx, admin, `SELECT max(version) FROM tendo_schema_migrations`); after != beforeVersion || after != 27 {
		t.Fatalf("ledger version=%d want 27", after)
	}
}

func requireSchemaVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, want int) {
	t.Helper()
	var version int
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if version != want || dirty {
		t.Fatalf("schema version=%d dirty=%t want version %d", version, dirty, want)
	}
}

func migrationRelationExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists
}

func migrationColumnExists(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1 AND column_name=$2`, table, column).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count > 0
}

func migrationInt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var value int
	if err := pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return value
}

func migrationString(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, query, args...).Scan(&value); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return value
}

func insertMigrationHousehold(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ($1, 'UTC') RETURNING id::text`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertMigrationAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, login string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ($1) RETURNING id::text`, login).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// insertMigrationRemoval writes one household_member_removals row in the
// pre-025 shape (no actor_authority) and returns its ID.
func insertMigrationRemoval(t *testing.T, ctx context.Context, pool *pgxpool.Pool, householdID, targetID, actorID, previousRole, removedAt string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO household_member_removals
		(household_id,target_user_id,actor_user_id,previous_role,removed_at)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5::timestamptz) RETURNING id::text`,
		householdID, targetID, actorID, previousRole, removedAt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// insertMigrationRoleChange writes one household_role_changes row in the
// pre-025 shape (no actor_authority) and returns its ID.
func insertMigrationRoleChange(t *testing.T, ctx context.Context, pool *pgxpool.Pool, householdID, targetID, actorID, previousRole, newRole string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `INSERT INTO household_role_changes
		(household_id,target_user_id,actor_user_id,previous_role,new_role,changed_at)
		VALUES ($1::uuid,$2::uuid,$3::uuid,$4,$5,'2026-01-01T13:00:00Z') RETURNING id::text`,
		householdID, targetID, actorID, previousRole, newRole).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func requireRemovalAuthority(t *testing.T, ctx context.Context, pool *pgxpool.Pool, removalID, want string) {
	t.Helper()
	if got := migrationString(t, ctx, pool, `SELECT actor_authority FROM household_member_removals WHERE id=$1::uuid`, removalID); got != want {
		t.Fatalf("removal %s authority=%q want %q", removalID, got, want)
	}
}

func requireSQLState(t *testing.T, want string, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != want {
		t.Fatalf("error=%v want SQLSTATE %s", err, want)
	}
}
