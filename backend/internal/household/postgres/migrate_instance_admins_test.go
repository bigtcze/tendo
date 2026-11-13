package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// v4BootstrapAt is the single transaction timestamp the released setup path
// used for the account, credential, household and membership rows.
var v4BootstrapAt = time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

// rewindToV21 migrates a fresh upgrade fixture and reverses 022, so the fixture
// presents the exact version 21 shape before a v21-era upgrade scenario.
func rewindToV21(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion22SQL); err != nil {
		t.Fatalf("rewind to version 21: %v", err)
	}
	var version int64
	if err := admin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&version); err != nil || version != 21 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	return admin, ctx
}

// insertV4SetupGraph writes the released setup transaction shape: one
// transaction with one timestamp for the account, its credential, the household
// and its admin membership, then completes setup.
func insertV4SetupGraph(t *testing.T, ctx context.Context, pool *pgxpool.Pool, login string) (userID, householdID string) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = tx.QueryRow(ctx, `INSERT INTO households(name, timezone, created_at) VALUES ('bootstrap home', 'UTC', $1) RETURNING id::text`, v4BootstrapAt).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO user_accounts(login, created_at) VALUES ($1, $2) RETURNING id::text`, login, v4BootstrapAt).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO local_credentials(user_id, password_hash, created_at) VALUES ($1, 'test-hash', $2)`, userID, v4BootstrapAt); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1, $2, 'admin')`, userID, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2 WHERE id=$1`, userID, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE installation_state SET setup_required=false`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return userID, householdID
}

// addDataToPreserve writes unrelated rows whose values must survive the 022
// upgrade untouched.
func addDataToPreserve(t *testing.T, ctx context.Context, pool *pgxpool.Pool, householdID string) {
	t.Helper()
	var subjectID string
	if err := pool.QueryRow(ctx, `INSERT INTO subjects(household_id, type, name) VALUES ($1, 'person', 'Existing person') RETURNING id::text`, householdID).Scan(&subjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO items(household_id, subject_id, title) VALUES ($1, $2, 'Existing task')`, householdID, subjectID); err != nil {
		t.Fatal(err)
	}
}

// dataSnapshot pins the pre-upgrade rows the migration must not change.
type dataSnapshot struct {
	Users, Households, Memberships, Credentials, Subjects, Items int
	ItemTitle                                                    string
}

func snapshotData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) dataSnapshot {
	t.Helper()
	var snapshot dataSnapshot
	err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM user_accounts),
		(SELECT count(*) FROM households),
		(SELECT count(*) FROM household_memberships),
		(SELECT count(*) FROM local_credentials),
		(SELECT count(*) FROM subjects),
		(SELECT count(*) FROM items),
		COALESCE((SELECT min(title) FROM items), '')`).Scan(
		&snapshot.Users, &snapshot.Households, &snapshot.Memberships, &snapshot.Credentials, &snapshot.Subjects, &snapshot.Items, &snapshot.ItemTitle)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func instanceAdminGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT user_id::text, grant_source FROM instance_admins`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	grants := map[string]string{}
	for rows.Next() {
		var userID, source string
		if err := rows.Scan(&userID, &source); err != nil {
			t.Fatal(err)
		}
		grants[userID] = source
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return grants
}

func assertNoInstanceAdmins(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if grants := instanceAdminGrants(t, ctx, pool); len(grants) != 0 {
		t.Fatalf("ambiguous installation granted %v", grants)
	}
}

// Case (i): unambiguous v4 setup evidence grants exactly the setup account and
// keeps every other row unchanged.
func TestMigrationTwentyTwoGrantsUnambiguousSetupAccount(t *testing.T) {
	admin, ctx := rewindToV21(t)
	setupUser, householdID := insertV4SetupGraph(t, ctx, admin, "setup_owner")
	var member string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('invited_member') RETURNING id::text`).Scan(&member); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1, $2, 'member')`, member, householdID); err != nil {
		t.Fatal(err)
	}
	var jit string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('member-jit00001') RETURNING id::text`).Scan(&jit); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO oidc_identities(issuer, subject, user_id, created_at) VALUES ('https://issuer.test', 'subject-jit', $1, now())`, jit); err != nil {
		t.Fatal(err)
	}
	addDataToPreserve(t, ctx, admin, householdID)
	before := snapshotData(t, ctx, admin)

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate 022: %v", err)
	}
	grants := instanceAdminGrants(t, ctx, admin)
	if len(grants) != 1 || grants[setupUser] != "v4_bootstrap_migration" {
		t.Fatalf("grants=%v want only %s", grants, setupUser)
	}
	if _, granted := grants[member]; granted {
		t.Fatal("invited member was granted")
	}
	if _, granted := grants[jit]; granted {
		t.Fatal("JIT OIDC account was granted")
	}
	if after := snapshotData(t, ctx, admin); after != before {
		t.Fatalf("upgrade changed data: before=%+v after=%+v", before, after)
	}
}

// Case (ii): the evidenced account was demoted, so the evidence does not
// describe the bootstrap authority and nobody is granted.
func TestMigrationTwentyTwoRefusesDemotedSetupAccount(t *testing.T) {
	admin, ctx := rewindToV21(t)
	setupUser, householdID := insertV4SetupGraph(t, ctx, admin, "demoted_owner")
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='member' WHERE user_id=$1`, setupUser); err != nil {
		t.Fatal(err)
	}
	var other string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('later_admin') RETURNING id::text`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1, $2, 'admin')`, other, householdID); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	assertNoInstanceAdmins(t, ctx, admin)
}

// Case (iii): more than one household makes the bootstrap evidence ambiguous.
func TestMigrationTwentyTwoRefusesMultipleHouseholds(t *testing.T) {
	admin, ctx := rewindToV21(t)
	insertV4SetupGraph(t, ctx, admin, "multi_house_owner")
	if _, err := admin.Exec(ctx, `INSERT INTO households(name, timezone) VALUES ('second home', 'UTC')`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	assertNoInstanceAdmins(t, ctx, admin)
}

// Case (iv): setup never completed, so no bootstrap authority exists yet. The
// full setup graph is present, which makes the completion predicate the only
// reason no grant is derived.
func TestMigrationTwentyTwoRefusesIncompleteSetup(t *testing.T) {
	admin, ctx := rewindToV21(t)
	insertV4SetupGraph(t, ctx, admin, "incomplete_owner")
	if _, err := admin.Exec(ctx, `UPDATE installation_state SET setup_required=true`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate 022: %v", err)
	}
	assertNoInstanceAdmins(t, ctx, admin)
}

// Case (iv): a missing installation_state row cannot prove setup completed, so
// the upgrade still succeeds and grants nobody.
func TestMigrationTwentyTwoRefusesMissingInstallationState(t *testing.T) {
	admin, ctx := rewindToV21(t)
	insertV4SetupGraph(t, ctx, admin, "missing_state_owner")
	if _, err := admin.Exec(ctx, `DELETE FROM installation_state`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate 022 with missing installation_state: %v", err)
	}
	assertNoInstanceAdmins(t, ctx, admin)
}

// Case (v): a second account created at the household timestamp makes the
// "exactly one account" requirement fail.
func TestMigrationTwentyTwoRefusesTiedTimestamps(t *testing.T) {
	admin, ctx := rewindToV21(t)
	insertV4SetupGraph(t, ctx, admin, "tied_owner")
	if _, err := admin.Exec(ctx, `INSERT INTO user_accounts(login, created_at) VALUES ('tied_second', $1)`, v4BootstrapAt); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	assertNoInstanceAdmins(t, ctx, admin)
}

// Case (vi): a credential created at a different instant than the setup
// transaction proves the account was not written by setup.
func TestMigrationTwentyTwoRefusesMismatchedCredentialTimestamp(t *testing.T) {
	admin, ctx := rewindToV21(t)
	setupUser, _ := insertV4SetupGraph(t, ctx, admin, "mismatched_owner")
	if _, err := admin.Exec(ctx, `UPDATE local_credentials SET created_at = created_at + interval '1 second' WHERE user_id=$1`, setupUser); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	assertNoInstanceAdmins(t, ctx, admin)
}

// Case (vi): the v4 admin password reset rewrites the setup credential and
// increments credential_version, so a co-admin who reset the setup account's
// password cannot inherit the installation authority.
func TestMigrationTwentyTwoRefusesResetCredential(t *testing.T) {
	admin, ctx := rewindToV21(t)
	setupUser, _ := insertV4SetupGraph(t, ctx, admin, "reset_credential_owner")
	if _, err := admin.Exec(ctx, `UPDATE local_credentials SET credential_version = credential_version + 1 WHERE user_id=$1`, setupUser); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("migrate 022: %v", err)
	}
	assertNoInstanceAdmins(t, ctx, admin)
}

// Case (vii): Down reverses the grant and a repeated Up re-derives the same
// grant from unchanged evidence.
func TestMigrationTwentyTwoDownThenUpRedrivesGrant(t *testing.T) {
	admin, ctx := rewindToV21(t)
	setupUser, _ := insertV4SetupGraph(t, ctx, admin, "rederive_owner")
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if grants := instanceAdminGrants(t, ctx, admin); grants[setupUser] != "v4_bootstrap_migration" {
		t.Fatalf("grants=%v", grants)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion22SQL); err != nil {
		t.Fatalf("reverse 022: %v", err)
	}
	var exists bool
	if err := admin.QueryRow(ctx, `SELECT to_regclass('public.instance_admins') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("table after Down=%v err=%v", exists, err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-apply 022: %v", err)
	}
	if grants := instanceAdminGrants(t, ctx, admin); len(grants) != 1 || grants[setupUser] != "v4_bootstrap_migration" {
		t.Fatalf("re-derived grants=%v", grants)
	}
}

// Case (viii): a direct upgrade from version 16 converts the released owner
// role and then grants that setup account.
func TestMigrationTwentyTwoFromVersionSixteen(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion20SQL+" ; "+testpostgres.RevertVersion19SQL+" ; "+testpostgres.RevertVersion18SQL+" ; "+testpostgres.RevertVersion17SQL); err != nil {
		t.Fatalf("rewind to version 16: %v", err)
	}
	var version int64
	if err := admin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&version); err != nil || version != 16 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var householdID, ownerID string
	if err = tx.QueryRow(ctx, `INSERT INTO households(name, timezone, created_at) VALUES ('v16 home', 'UTC', $1) RETURNING id::text`, v4BootstrapAt).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `INSERT INTO user_accounts(login, created_at) VALUES ('v16_owner', $1) RETURNING id::text`, v4BootstrapAt).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO local_credentials(user_id, password_hash, created_at) VALUES ($1, 'v16-hash', $2)`, ownerID, v4BootstrapAt); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1, $2, 'owner')`, ownerID, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2 WHERE id=$1`, ownerID, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `UPDATE installation_state SET setup_required=false`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("upgrade from version 16: %v", err)
	}
	var role string
	if err := admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1`, ownerID).Scan(&role); err != nil || role != "admin" {
		t.Fatalf("role=%q err=%v", role, err)
	}
	if grants := instanceAdminGrants(t, ctx, admin); len(grants) != 1 || grants[ownerID] != "v4_bootstrap_migration" {
		t.Fatalf("grants=%v want only %s", grants, ownerID)
	}
}
