package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// instanceAdminFixture migrates the shared test database and clears every
// account-level row, leaving setup required, so each command scenario starts
// from a known installation state.
func instanceAdminFixture(t *testing.T) (context.Context, *pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL instance administrator tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		t.Helper()
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatalf("reset fixtures: %v", err)
		}
	}
	reset()
	t.Cleanup(reset)
	return ctx, admin, app
}

// newInstanceAdminAccount inserts a bare account without a credential or
// household, which is enough for a capability grant.
func newInstanceAdminAccount(t *testing.T, ctx context.Context, admin *pgxpool.Pool, login string) string {
	t.Helper()
	var id string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ($1) RETURNING id::text`, login).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func setSetupRequired(t *testing.T, ctx context.Context, admin *pgxpool.Pool, required bool) {
	t.Helper()
	if _, err := admin.Exec(ctx, `UPDATE installation_state SET setup_required=$1`, required); err != nil {
		t.Fatal(err)
	}
}

// TestInstanceAdminGrantAgainstPostgres covers case (f): the recovery command's
// application service refuses before setup, reports an unknown login, inserts
// exactly one 'cli' grant and is idempotent for an existing administrator.
func TestInstanceAdminGrantAgainstPostgres(t *testing.T) {
	ctx, admin, app := instanceAdminFixture(t)
	service := household.NewInstanceAdmins(NewInstanceAdminRepository(app))
	targetID := newInstanceAdminAccount(t, ctx, admin, "grant_target")

	if _, err := service.Grant(ctx, "grant_target"); !errors.Is(err, household.ErrSetupIncomplete) {
		t.Fatalf("grant before setup err=%v", err)
	}
	var grants int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM instance_admins`).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("grant before setup wrote %d rows err=%v", grants, err)
	}

	setSetupRequired(t, ctx, admin, false)
	if _, err := service.Grant(ctx, "missing_account"); !errors.Is(err, household.ErrAccountNotFound) {
		t.Fatalf("unknown login err=%v", err)
	}
	if _, err := service.Grant(ctx, "GRANT_TARGET"); !errors.Is(err, household.ErrAccountNotFound) {
		t.Fatalf("case-folded login err=%v", err)
	}

	grant, err := service.Grant(ctx, "grant_target")
	if err != nil || !grant.Granted || grant.UserID != targetID {
		t.Fatalf("grant=%+v err=%v", grant, err)
	}
	var source string
	if err := admin.QueryRow(ctx, `SELECT grant_source FROM instance_admins WHERE user_id=$1`, targetID).Scan(&source); err != nil || source != "cli" {
		t.Fatalf("grant source=%q err=%v", source, err)
	}
	repeat, err := service.Grant(ctx, "grant_target")
	if err != nil || repeat.Granted || repeat.UserID != targetID {
		t.Fatalf("repeat grant=%+v err=%v", repeat, err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM instance_admins`).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("grants after repeat=%d err=%v", grants, err)
	}
}

// TestInstanceAdminRuntimeRolePrivileges covers case (f): the runtime role may
// read and insert grants, but can never rewrite or delete one.
func TestInstanceAdminRuntimeRolePrivileges(t *testing.T) {
	ctx, admin, app := instanceAdminFixture(t)
	service := household.NewInstanceAdmins(NewInstanceAdminRepository(app))
	newInstanceAdminAccount(t, ctx, admin, "privilege_target")
	setSetupRequired(t, ctx, admin, false)
	if _, err := service.Grant(ctx, "privilege_target"); err != nil {
		t.Fatal(err)
	}
	var grants int
	if err := app.QueryRow(ctx, `SELECT count(*) FROM instance_admins`).Scan(&grants); err != nil || grants != 1 {
		t.Fatalf("runtime role read grants=%d err=%v", grants, err)
	}
	for _, statement := range []string{
		`UPDATE instance_admins SET grant_source = grant_source`,
		`DELETE FROM instance_admins`,
		`TRUNCATE instance_admins`,
	} {
		_, err := app.Exec(ctx, statement)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("%q error=%v, want 42501", statement, err)
		}
	}
	var publicPrivileges int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM information_schema.role_table_grants WHERE table_name='instance_admins' AND grantee='PUBLIC'`).Scan(&publicPrivileges); err != nil || publicPrivileges != 0 {
		t.Fatalf("PUBLIC privileges=%d err=%v", publicPrivileges, err)
	}
	var canReference, canTrigger bool
	if err := app.QueryRow(ctx, `SELECT has_table_privilege(current_user,'instance_admins','REFERENCES'), has_table_privilege(current_user,'instance_admins','TRIGGER')`).Scan(&canReference, &canTrigger); err != nil || canReference || canTrigger {
		t.Fatalf("runtime REFERENCES=%v TRIGGER=%v err=%v, want both false", canReference, canTrigger, err)
	}
}

// TestInstanceAdminAccountDeleteRestricted covers case (f): ON DELETE RESTRICT
// keeps account deletion from silently removing the last administrator.
func TestInstanceAdminAccountDeleteRestricted(t *testing.T) {
	ctx, admin, app := instanceAdminFixture(t)
	service := household.NewInstanceAdmins(NewInstanceAdminRepository(app))
	targetID := newInstanceAdminAccount(t, ctx, admin, "delete_target")
	setSetupRequired(t, ctx, admin, false)
	if _, err := service.Grant(ctx, "delete_target"); err != nil {
		t.Fatal(err)
	}
	_, err := admin.Exec(ctx, `DELETE FROM user_accounts WHERE id=$1`, targetID)
	var pgErr *pgconn.PgError
	// PostgreSQL 18 reports ON DELETE RESTRICT as restrict_violation (23001).
	if !errors.As(err, &pgErr) || (pgErr.Code != "23503" && pgErr.Code != "23001") {
		t.Fatalf("delete granted account error=%v, want a foreign key/restrict violation", err)
	}
	var accounts, grants int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_accounts WHERE id=$1),(SELECT count(*) FROM instance_admins WHERE user_id=$1)`, targetID).Scan(&accounts, &grants); err != nil || accounts != 1 || grants != 1 {
		t.Fatalf("after refused delete accounts=%d grants=%d err=%v", accounts, grants, err)
	}
	// Once the capability is removed the account is deletable again.
	if _, err := admin.Exec(ctx, `DELETE FROM instance_admins WHERE user_id=$1`, targetID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `DELETE FROM user_accounts WHERE id=$1`, targetID); err != nil {
		t.Fatalf("delete after revoking grant: %v", err)
	}
}

// TestInstanceAdminStartupWarningState covers case (f): startup warns exactly
// when setup is complete and no administrator is configured, and the check is
// read-only.
func TestInstanceAdminStartupWarningState(t *testing.T) {
	ctx, admin, app := instanceAdminFixture(t)
	service := household.NewInstanceAdmins(NewInstanceAdminRepository(app))
	missing, err := service.SetupCompleteWithoutInstanceAdmin(ctx)
	if err != nil || missing {
		t.Fatalf("setup required missing=%v err=%v", missing, err)
	}
	setSetupRequired(t, ctx, admin, false)
	missing, err = service.SetupCompleteWithoutInstanceAdmin(ctx)
	if err != nil || !missing {
		t.Fatalf("setup complete without administrator missing=%v err=%v", missing, err)
	}
	newInstanceAdminAccount(t, ctx, admin, "warning_target")
	if _, err := admin.Exec(ctx, `INSERT INTO instance_admins(user_id, granted_at, grant_source) SELECT id, now(), 'cli' FROM user_accounts WHERE login='warning_target'`); err != nil {
		t.Fatal(err)
	}
	missing, err = service.SetupCompleteWithoutInstanceAdmin(ctx)
	if err != nil || missing {
		t.Fatalf("setup complete with administrator missing=%v err=%v", missing, err)
	}
}
