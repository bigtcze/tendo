package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func migrationFixture(t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("TEST_DATABASE_ADMIN_URL")
	if adminURL == "" {
		t.Fatal("TEST_DATABASE_ADMIN_URL is required for real PostgreSQL migration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	base, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(base.Close)
	dbName := testpostgres.CreateDatabase(t, ctx, base)
	pool, err := pgxpool.NewWithConfig(ctx, testpostgres.PoolConfigForDatabase(t, adminURL, dbName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestMigrationSeventeenUpgradesOwnerRolesAndPreservesData(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion17SQL); err != nil {
		t.Fatalf("rewind to version 16: %v", err)
	}
	var householdID, ownerID, memberID string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ('v16 home', 'Europe/Prague') RETURNING id::text`).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('v16_owner') RETURNING id::text`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('v16_member') RETURNING id::text`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid, $3::uuid, 'owner'), ($2::uuid, $3::uuid, 'member')`, ownerID, memberID, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, ownerID, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id, token_hash, created_at, expires_at) VALUES ($1::uuid, decode(repeat('ab',32),'hex'), now(), now() + interval '1 day')`, ownerID); err != nil {
		t.Fatal(err)
	}

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate 17: %v", err)
	}
	var version int64
	var dirty bool
	if err := admin.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil || version != 17 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", version, dirty, err)
	}
	var admins, members, versionOne int
	if err := admin.QueryRow(ctx, `SELECT count(*) FILTER (WHERE role='admin'), count(*) FILTER (WHERE role='member'), count(*) FILTER (WHERE version=1) FROM household_memberships`).Scan(&admins, &members, &versionOne); err != nil || admins != 1 || members != 1 || versionOne != 2 {
		t.Fatalf("admins=%d members=%d version1=%d err=%v", admins, members, versionOne, err)
	}
	var ownerRole, memberRole string
	if err := admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1::uuid`, ownerID).Scan(&ownerRole); err != nil || ownerRole != "admin" {
		t.Fatalf("owner role=%q err=%v", ownerRole, err)
	}
	if err := admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1::uuid`, memberID).Scan(&memberRole); err != nil || memberRole != "member" {
		t.Fatalf("member role=%q err=%v", memberRole, err)
	}
	var defaultHousehold string
	if err := admin.QueryRow(ctx, `SELECT default_household_id::text FROM user_accounts WHERE id=$1::uuid`, ownerID).Scan(&defaultHousehold); err != nil || defaultHousehold != householdID {
		t.Fatalf("default household=%q want %q err=%v", defaultHousehold, householdID, err)
	}
	var sessions int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions`).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("sessions=%d err=%v", sessions, err)
	}
	var auditRows int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_role_changes`).Scan(&auditRows); err != nil || auditRows != 0 {
		t.Fatalf("fabricated audit rows=%d err=%v", auditRows, err)
	}

	var ledgerRows int
	if err := admin.QueryRow(ctx, `SELECT count(*), max(version) FROM tendo_schema_migrations`).Scan(&ledgerRows, &version); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("repeat migrate: %v", err)
	}
	var ledgerRowsAfter int
	var versionAfter int64
	if err := admin.QueryRow(ctx, `SELECT count(*), max(version) FROM tendo_schema_migrations`).Scan(&ledgerRowsAfter, &versionAfter); err != nil {
		t.Fatal(err)
	}
	if ledgerRowsAfter != ledgerRows || versionAfter != version {
		t.Fatalf("repeat migrate changed ledger: rows %d->%d version %d->%d", ledgerRows, ledgerRowsAfter, version, versionAfter)
	}
}

func TestMigrationSeventeenDownReversesPopulatedSchema(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var householdID, adminID, memberID string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ('down home', 'UTC') RETURNING id::text`).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('down_admin') RETURNING id::text`).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('down_member') RETURNING id::text`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid, $3::uuid, 'admin'), ($2::uuid, $3::uuid, 'member')`, adminID, memberID, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_role_changes(household_id, target_user_id, actor_user_id, previous_role, new_role, changed_at) VALUES ($1::uuid, $2::uuid, $2::uuid, 'member', 'admin', now())`, householdID, memberID); err != nil {
		t.Fatal(err)
	}

	down, err := database.MigrationDownSQL(17)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(testpostgres.RevertVersion17SQL, down) || !strings.HasSuffix(testpostgres.RevertVersion17SQL, "DELETE FROM tendo_schema_migrations WHERE version=17") {
		t.Fatalf("RevertVersion17SQL does not derive from migration 017 Down section")
	}
	if _, err := admin.Exec(ctx, down); err != nil {
		t.Fatalf("execute migration 017 Down on populated schema: %v", err)
	}

	var adminRole, memberRole, constraint string
	if err := admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1::uuid`, adminID).Scan(&adminRole); err != nil || adminRole != "owner" {
		t.Fatalf("admin role after Down=%q err=%v", adminRole, err)
	}
	if err := admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1::uuid`, memberID).Scan(&memberRole); err != nil || memberRole != "member" {
		t.Fatalf("member role after Down=%q err=%v", memberRole, err)
	}
	if err := admin.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='household_memberships_role_check'`).Scan(&constraint); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(constraint, "owner") || strings.Contains(constraint, "admin") {
		t.Fatalf("constraint after Down=%q", constraint)
	}
	var versionColumn, auditTable bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='household_memberships' AND column_name='version')`).Scan(&versionColumn); err != nil || versionColumn {
		t.Fatalf("membership version column after Down=%v err=%v", versionColumn, err)
	}
	if err := admin.QueryRow(ctx, `SELECT to_regclass('public.household_role_changes') IS NOT NULL`).Scan(&auditTable); err != nil || auditTable {
		t.Fatalf("audit table after Down=%v err=%v", auditTable, err)
	}

	// Round trip: remove the 17 ledger row (as an upgrade fixture does) and
	// migrate forward again over the populated version 16 schema.
	if _, err := admin.Exec(ctx, `DELETE FROM tendo_schema_migrations WHERE version=17`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-migrate after Down: %v", err)
	}
	var version int64
	var dirty bool
	if err := admin.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil || version != 17 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", version, dirty, err)
	}
	if err := admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1::uuid`, adminID).Scan(&adminRole); err != nil || adminRole != "admin" {
		t.Fatalf("admin role after round trip=%q err=%v", adminRole, err)
	}
	var membershipVersion int64
	if err := admin.QueryRow(ctx, `SELECT version FROM household_memberships WHERE user_id=$1::uuid`, adminID).Scan(&membershipVersion); err != nil || membershipVersion != 1 {
		t.Fatalf("membership version after round trip=%d err=%v", membershipVersion, err)
	}
}

func TestMigrationSeventeenFailsClosedWithoutAdmin(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion17SQL); err != nil {
		t.Fatalf("rewind to version 16: %v", err)
	}
	var householdID, memberID string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ('orphan home', 'UTC') RETURNING id::text`).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('orphan_member') RETURNING id::text`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid, $2::uuid, 'member')`, memberID, householdID); err != nil {
		t.Fatal(err)
	}

	if err := database.Migrate(ctx, admin); err == nil {
		t.Fatal("migration accepted a household with members but no admin")
	}
	var version int64
	var dirty bool
	if err := admin.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil || version != 16 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", version, dirty, err)
	}
	var auditTable bool
	if err := admin.QueryRow(ctx, `SELECT to_regclass('public.household_role_changes') IS NOT NULL`).Scan(&auditTable); err != nil || auditTable {
		t.Fatalf("audit table exists after failed migration: %v err=%v", auditTable, err)
	}
	var versionColumn int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='household_memberships' AND column_name='version'`).Scan(&versionColumn); err != nil || versionColumn != 0 {
		t.Fatalf("membership version column after failed migration=%d err=%v", versionColumn, err)
	}
	var role, constraint string
	if err := admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1::uuid`, memberID).Scan(&role); err != nil || role != "member" {
		t.Fatalf("role=%q err=%v", role, err)
	}
	if err := admin.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname='household_memberships_role_check'`).Scan(&constraint); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(constraint, "owner") || strings.Contains(constraint, "admin") {
		t.Fatalf("constraint after failed migration=%q", constraint)
	}

	// The failure left no dirty state or partial objects; repairing the data and
	// re-running the migration succeeds.
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='owner' WHERE user_id=$1::uuid`, memberID); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate after repair: %v", err)
	}
	if err := admin.QueryRow(ctx, `SELECT max(version), bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil || version != 17 || dirty {
		t.Fatalf("after repair version=%d dirty=%v err=%v", version, dirty, err)
	}
}
