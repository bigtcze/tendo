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
)

const tombstoneLoginExpr = `'~deleted-'||replace(id::text,'-','')`

// TestMigrationTwentySevenUpAndPrivileges covers migration 027 (ADR 0022): the
// account tombstone column and CHECK constraint, the partial login uniqueness
// that frees a deleted account's login, the append-only deletion and
// administrator-revocation audit tables and the runtime role privileges.
func TestMigrationTwentySevenUpAndPrivileges(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}

	requireSchemaVersion(t, ctx, admin, 30)
	for _, relation := range []string{"account_deletions", "instance_admin_revocations"} {
		if !migrationRelationExists(t, ctx, admin, relation) {
			t.Fatalf("%s missing after migrate to 30", relation)
		}
	}
	if !migrationColumnExists(t, ctx, admin, "user_accounts", "deleted_at") {
		t.Fatal("user_accounts.deleted_at missing after migrate to 30")
	}
	var tombstoneConstraint string
	if err := admin.QueryRow(ctx, `SELECT conname FROM pg_constraint
		WHERE conname='user_accounts_deleted_tombstone' AND contype='c'`).Scan(&tombstoneConstraint); err != nil {
		t.Fatalf("tombstone CHECK constraint missing: %v", err)
	}

	for _, table := range []string{"account_deletions", "instance_admin_revocations"} {
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
				`SELECT has_table_privilege('tendo',$1,$2)`, table, check.privilege).Scan(&got); err != nil || got != check.want {
				t.Errorf("tendo %s %s = %v, want %v (err=%v)", check.privilege, table, got, check.want, err)
			}
		}
		var publicGrants int
		if err := admin.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.role_table_grants WHERE table_name=$1 AND grantee='PUBLIC'`, table).Scan(&publicGrants); err != nil || publicGrants != 0 {
			t.Fatalf("%s PUBLIC table grants=%d err=%v", table, publicGrants, err)
		}
	}
	for _, table := range []string{"instance_admins", "oidc_identities"} {
		var got bool
		if err := admin.QueryRow(ctx,
			`SELECT has_table_privilege('tendo',$1,'DELETE')`, table).Scan(&got); err != nil || !got {
			t.Errorf("tendo DELETE %s = %v, want true (err=%v)", table, got, err)
		}
	}

	// The login is unique among live accounts only.
	live := insertMigrationAccount(t, ctx, admin, "deletion_live")
	duplicate := insertMigrationAccount(t, ctx, admin, "deletion_duplicate")
	_, err := admin.Exec(ctx, `UPDATE user_accounts SET login='deletion_live' WHERE id=$1::uuid`, duplicate)
	var loginConflict *pgconn.PgError
	if !errors.As(err, &loginConflict) || loginConflict.Code != "23505" || loginConflict.ConstraintName != "user_accounts_login_key" {
		t.Fatalf("duplicate live login err=%v want 23505 user_accounts_login_key", err)
	}

	// A tombstone frees the login for reuse.
	if _, err := admin.Exec(ctx, `UPDATE user_accounts
		SET deleted_at=now(), login=`+tombstoneLoginExpr+`, email=NULL, default_household_id=NULL
		WHERE id=$1::uuid`, live); err != nil {
		t.Fatalf("tombstone account: %v", err)
	}
	reused := insertMigrationAccount(t, ctx, admin, "deletion_live")
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM user_accounts WHERE login='deletion_live'`); got != 1 {
		t.Fatalf("live rows with the reused login=%d want 1", got)
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM user_accounts WHERE deleted_at IS NOT NULL`); got != 1 {
		t.Fatalf("tombstoned rows=%d want 1", got)
	}
	if got := migrationString(t, ctx, admin, `SELECT login FROM user_accounts WHERE id=$1::uuid`, live); got != "~deleted-"+strings.ReplaceAll(live, "-", "") {
		t.Fatalf("tombstone login=%q", got)
	}

	// The CHECK constraint enforces the full tombstone shape.
	for name, tc := range map[string]struct {
		query string
		arg   string
	}{
		"deleted without tombstone login": {
			`UPDATE user_accounts SET deleted_at=now() WHERE id=$1::uuid`, reused,
		},
		"tombstone login without deleted_at": {
			`UPDATE user_accounts SET login=` + tombstoneLoginExpr + ` WHERE id=$1::uuid`, reused,
		},
		"deleted with email": {
			`UPDATE user_accounts SET email='kept@example.test', deleted_at=now(), login=` + tombstoneLoginExpr + ` WHERE id=$1::uuid`, reused,
		},
	} {
		_, err := admin.Exec(ctx, tc.query, tc.arg)
		var violation *pgconn.PgError
		if !errors.As(err, &violation) || violation.Code != "23514" || violation.ConstraintName != "user_accounts_deleted_tombstone" {
			t.Fatalf("%s: err=%v want 23514 user_accounts_deleted_tombstone", name, err)
		}
	}
}

// TestMigrationTwentySevenRevertAndReapply reverses 027 on a clean schema,
// asserts the version 26 shape and that migrating forward works again.
func TestMigrationTwentySevenRevertAndReapply(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	// A non-tombstoned account keeps the Down legal.
	insertMigrationAccount(t, ctx, admin, "deletion_revert_live")

	if _, err := admin.Exec(ctx, testpostgres.RevertVersion27SQL); err != nil {
		t.Fatalf("reverse 027: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 26)
	for _, relation := range []string{"account_deletions", "instance_admin_revocations"} {
		if migrationRelationExists(t, ctx, admin, relation) {
			t.Fatalf("%s survived Down", relation)
		}
	}
	if migrationColumnExists(t, ctx, admin, "user_accounts", "deleted_at") {
		t.Fatal("user_accounts.deleted_at survived Down")
	}
	// The plain (non-partial) login uniqueness is restored.
	var constraintType string
	var partial bool
	if err := admin.QueryRow(ctx, `SELECT c.contype::text, i.indpred IS NOT NULL
		FROM pg_constraint c JOIN pg_index i ON i.indexrelid=c.conindid
		WHERE c.conname='user_accounts_login_key'`).Scan(&constraintType, &partial); err != nil {
		t.Fatalf("login uniqueness after Down: %v", err)
	}
	if constraintType != "u" || partial {
		t.Fatalf("login uniqueness after Down type=%q partial=%v want u/false", constraintType, partial)
	}
	for _, table := range []string{"instance_admins", "oidc_identities"} {
		var got bool
		if err := admin.QueryRow(ctx,
			`SELECT has_table_privilege('tendo',$1,'DELETE')`, table).Scan(&got); err != nil || got {
			t.Errorf("tendo DELETE %s after Down = %v, want false (err=%v)", table, got, err)
		}
	}

	// Up again over the populated version 26 schema.
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatalf("re-apply 027: %v", err)
	}
	requireSchemaVersion(t, ctx, admin, 30)
	for _, relation := range []string{"account_deletions", "instance_admin_revocations"} {
		if !migrationRelationExists(t, ctx, admin, relation) {
			t.Fatalf("%s missing after re-apply", relation)
		}
	}
}

// TestMigrationTwentySevenDownRefusesAfterDeletion proves the Down guard: an
// earlier release cannot represent a tombstone, so the reversal fails and
// leaves version 28 intact.
func TestMigrationTwentySevenDownRefusesAfterDeletion(t *testing.T) {
	admin := migrationFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	deleted := insertMigrationAccount(t, ctx, admin, "deletion_guard_target")
	actor := insertMigrationAccount(t, ctx, admin, "deletion_guard_actor")
	if _, err := admin.Exec(ctx, `UPDATE user_accounts
		SET deleted_at=now(), login=`+tombstoneLoginExpr+`, email=NULL, default_household_id=NULL
		WHERE id=$1::uuid`, deleted); err != nil {
		t.Fatalf("tombstone account: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO account_deletions(user_id,actor_user_id,deleted_at)
		VALUES ($1::uuid,$2::uuid,now())`, deleted, actor); err != nil {
		t.Fatalf("deletion record: %v", err)
	}

	if _, err := admin.Exec(ctx, testpostgres.RevertVersion27SQL); err == nil || !strings.Contains(err.Error(), "cannot be reversed") {
		t.Fatalf("Down after deletion err=%v want cannot be reversed", err)
	}
	requireSchemaVersion(t, ctx, admin, 30)
	for _, relation := range []string{"account_deletions", "instance_admin_revocations"} {
		if !migrationRelationExists(t, ctx, admin, relation) {
			t.Fatalf("%s removed by the refused Down", relation)
		}
	}
	if !migrationColumnExists(t, ctx, admin, "user_accounts", "deleted_at") {
		t.Fatal("user_accounts.deleted_at removed by the refused Down")
	}
	if got := migrationInt(t, ctx, admin, `SELECT count(*) FROM account_deletions`); got != 1 {
		t.Fatalf("deletion rows after refused Down=%d want 1", got)
	}
}
