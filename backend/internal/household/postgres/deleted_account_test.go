package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// tombstoneAccountFixture anonymizes an account in place exactly as migration
// 027 / the deletion service does, leaving the row and its memberships intact.
// keepGrant leaves an instance-admins row behind to prove the authority check
// refuses it on its own. It returns the stored tombstone login.
func tombstoneAccountFixture(t *testing.T, ctx context.Context, admin *pgxpool.Pool, userID string, keepGrant bool) string {
	t.Helper()
	var login string
	if err := admin.QueryRow(ctx, `UPDATE user_accounts
		SET login = '~deleted-' || replace(id::text, '-', ''),
		    deleted_at = now(),
		    email = NULL, email_verified_at = NULL, email_source = NULL, email_issuer = NULL,
		    default_household_id = NULL
		WHERE id = $1::uuid
		RETURNING login`, userID).Scan(&login); err != nil {
		t.Fatalf("tombstone account %s: %v", userID, err)
	}
	for _, statement := range []string{
		`DELETE FROM local_credentials WHERE user_id=$1::uuid`,
		`DELETE FROM user_sessions WHERE user_id=$1::uuid`,
		`DELETE FROM oidc_identities WHERE user_id=$1::uuid`,
	} {
		if _, err := admin.Exec(ctx, statement, userID); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if !keepGrant {
		if _, err := admin.Exec(ctx, `DELETE FROM instance_admins WHERE user_id=$1::uuid`, userID); err != nil {
			t.Fatalf("tombstone grant: %v", err)
		}
	}
	return login
}

// newAdminCenterTestFixture builds the same wire-up as the other instance-admin
// center tests so this file can assert against the real Postgres adapters.
func newAdminCenterTestFixture(t *testing.T) (context.Context, *adminCenterFixture) {
	t.Helper()
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL deleted-account tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newAdminCenterFixture(t, ctx, admin, app)
	f.prepare()
	t.Cleanup(f.reset)
	return ctx, f
}

// TestDeletedAccountAdminCenterRefused covers the instance-admin assignment,
// household-admin removal, directory, CLI recovery grant and authority gate:
// none of them may resolve or act on a tombstoned account.
func TestDeletedAccountAdminCenterRefused(t *testing.T) {
	ctx, f := newAdminCenterTestFixture(t)
	actor := f.newUserWithLogin("tombstone_actor")
	f.grantInstanceAdmin(actor)
	house := f.newHousehold("tombstone house")

	t.Run("assignment refuses a deleted account", func(t *testing.T) {
		houseAdmin := f.newUserWithLogin("tombstone_assign_house_admin")
		f.join(houseAdmin, house, household.RoleAdmin)
		target := f.newUserWithLogin("tombstone_assign_target")
		tombstoneAccountFixture(t, ctx, f.admin, target, false)
		if _, _, err := f.center.AssignAccount(ctx, actor, house, target, household.RoleMember); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("assign deleted account err=%v, want ErrNotFound", err)
		}
		if got := f.userMembershipCount(house, target); got != 0 {
			t.Fatalf("refused assignment wrote %d memberships", got)
		}
		if got := f.assignmentCount(house); got != 0 {
			t.Fatalf("refused assignment wrote %d audit rows", got)
		}
	})

	t.Run("removal refuses a deleted account", func(t *testing.T) {
		houseAdmin := f.newUserWithLogin("tombstone_removal_admin")
		f.join(houseAdmin, house, household.RoleAdmin)
		member := f.newUserWithLogin("tombstone_removal_member")
		f.join(member, house, household.RoleMember)
		tombstoneAccountFixture(t, ctx, f.admin, member, false)
		if err := f.service.RemoveMember(ctx, houseAdmin, house, member, 1); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("remove deleted account err=%v, want ErrNotFound", err)
		}
		if _, _, ok := f.membership(house, member); !ok {
			t.Fatal("refused removal deleted the membership")
		}
	})

	t.Run("directory excludes tombstones and frees the login", func(t *testing.T) {
		gone := f.newUserWithLogin("tombstone_directory_entry")
		tombstoneAccountFixture(t, ctx, f.admin, gone, false)
		accounts, _, err := f.center.ListDirectory(ctx, actor, "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, account := range accounts {
			if account.UserID == gone {
				t.Fatalf("directory listed deleted account %+v", account)
			}
		}
		// The partial unique index frees the old login for a new live account,
		// which is then the only directory entry for it.
		var replacement string
		if err := f.admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('tombstone_directory_entry') RETURNING id::text`).Scan(&replacement); err != nil {
			t.Fatalf("reuse freed login: %v", err)
		}
		accounts, _, err = f.center.ListDirectory(ctx, actor, "tombstone_directory_entry", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(accounts) != 1 || accounts[0].UserID != replacement || accounts[0].Login != "tombstone_directory_entry" {
			t.Fatalf("directory after reuse=%+v", accounts)
		}
		searched, _, err := f.center.ListDirectory(ctx, actor, "~deleted-", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(searched) != 0 {
			t.Fatalf("directory matched tombstone logins: %+v", searched)
		}
	})

	t.Run("the CLI grant refuses the tombstone login", func(t *testing.T) {
		target := f.newUserWithLogin("tombstone_cli_target")
		tombstoneLogin := tombstoneAccountFixture(t, ctx, f.admin, target, false)
		f.setSetupRequired(false)
		service := household.NewInstanceAdmins(NewInstanceAdminRepository(f.app))
		if _, err := service.Grant(ctx, tombstoneLogin); !errors.Is(err, household.ErrAccountNotFound) {
			t.Fatalf("grant by tombstone login err=%v, want ErrAccountNotFound", err)
		}
		var grants int
		if err := f.admin.QueryRow(ctx, `SELECT count(*) FROM instance_admins WHERE user_id=$1::uuid`, target).Scan(&grants); err != nil || grants != 0 {
			t.Fatalf("grants=%d err=%v", grants, err)
		}
	})

	t.Run("a left-behind grant does not authorise a deleted account", func(t *testing.T) {
		target := f.newUserWithLogin("tombstone_admin")
		f.grantInstanceAdmin(target)
		tombstoneAccountFixture(t, ctx, f.admin, target, true)
		var left int
		if err := f.admin.QueryRow(ctx, `SELECT count(*) FROM instance_admins WHERE user_id=$1::uuid`, target).Scan(&left); err != nil || left != 1 {
			t.Fatalf("left-behind grants=%d err=%v", left, err)
		}
		if err := f.center.RequireInstanceAdmin(ctx, target); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("deleted instance admin err=%v, want ErrInstanceAdminRequired", err)
		}
		if _, _, err := f.center.ListHouseholds(ctx, target, "", 10); !errors.Is(err, household.ErrInstanceAdminRequired) {
			t.Fatalf("deleted instance admin ListHouseholds err=%v", err)
		}
	})
}

// waitForAppLockWait waits until a backend of the runtime pool is blocked on a
// lock. Packages run serially (-p 1), so any waiter belongs to the path under
// test.
func waitForAppLockWait(t *testing.T, ctx context.Context, app *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := app.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the assignment never blocked on the account row lock")
}

// TestDeletedAccountAssignmentRaceRefused is the finding 1 race on the
// instance-admin assignment: the account lock acquired after a concurrent,
// uncommitted tombstone must see the deleted row and report not found.
func TestDeletedAccountAssignmentRaceRefused(t *testing.T) {
	ctx, f := newAdminCenterTestFixture(t)
	actor := f.newUserWithLogin("race_assignment_actor")
	f.grantInstanceAdmin(actor)
	house := f.newHousehold("race assignment house")
	houseAdmin := f.newUserWithLogin("race_assignment_house_admin")
	f.join(houseAdmin, house, household.RoleAdmin)
	target := f.newUserWithLogin("race_assignment_target")

	deleter, err := f.app.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deleter.Rollback(context.Background()) }()
	if _, err = deleter.Exec(ctx, `UPDATE user_accounts
		SET login = '~deleted-' || replace(id::text, '-', ''), deleted_at = now(), default_household_id = NULL
		WHERE id = $1::uuid`, target); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, _, assignErr := f.center.AssignAccount(ctx, actor, house, target, household.RoleMember)
		result <- assignErr
	}()
	waitForAppLockWait(t, ctx, f.app)
	if err := deleter.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, household.ErrNotFound) {
		t.Fatalf("assignment racing a deletion err=%v, want ErrNotFound", err)
	}
	if got := f.userMembershipCount(house, target); got != 0 {
		t.Fatalf("refused assignment wrote %d memberships", got)
	}
	if got := f.assignmentCount(house); got != 0 {
		t.Fatalf("refused assignment wrote %d audit rows", got)
	}
}
