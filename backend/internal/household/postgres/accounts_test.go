package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	subjectpostgres "github.com/bigtcze/tendo/backend/internal/subject/postgres"
)

func testMemberPeople(tx householddb.DBTX) identityapp.MemberPersonProvisioner {
	return subjectpostgres.NewMemberPersonProvisioner(tx)
}

type emptyResetLookup struct{}

func (emptyResetLookup) PasswordResetsByUserIDs(context.Context, string, []string) (map[string]household.PasswordReset, error) {
	return map[string]household.PasswordReset{}, nil
}

func TestAccountsAgainstPostgres(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL account tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		t.Helper()
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households, household_role_changes CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatalf("reset fixtures: %v", err)
		}
	}
	reset()
	defer reset()

	clock := func() time.Time { return time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC) }
	service := AccountServiceFor(app, identitypostgres.New(app, nil), identitypostgres.New(app, nil), emptyResetLookup{}, clock)

	newUser := func(login string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ($1) RETURNING id::text`, login).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newHousehold := func(name string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ($1, 'UTC') RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	join := func(userID, householdID, role string) {
		t.Helper()
		if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid, $2::uuid, $3)`, userID, householdID, role); err != nil {
			t.Fatal(err)
		}
	}
	password := func(userID string) {
		t.Helper()
		hash, err := security.HashPassword("correct horse battery")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO local_credentials(user_id, password_hash) VALUES ($1::uuid, $2)`, userID, hash); err != nil {
			t.Fatal(err)
		}
	}
	oidc := func(userID string) {
		t.Helper()
		if _, err := admin.Exec(ctx, `INSERT INTO oidc_identities(issuer, subject, user_id, created_at) VALUES ('https://idp.example', $1, $2::uuid, now())`, userID, userID); err != nil {
			t.Fatal(err)
		}
	}
	auditCount := func(householdID string) int {
		t.Helper()
		var count int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_role_changes WHERE household_id=$1::uuid`, householdID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	roleOf := func(householdID, userID string) (string, int64) {
		t.Helper()
		var role string
		var version int64
		if err := admin.QueryRow(ctx, `SELECT role, version FROM household_memberships WHERE household_id=$1::uuid AND user_id=$2::uuid`, householdID, userID).Scan(&role, &version); err != nil {
			t.Fatal(err)
		}
		return role, version
	}

	admin1, admin2, member, outsider := newUser("accounts_admin1"), newUser("accounts_admin2"), newUser("accounts_member"), newUser("accounts_outsider")
	house, otherHouse := newHousehold("accounts house"), newHousehold("accounts other house")
	join(admin1, house, household.RoleAdmin)
	join(admin2, house, household.RoleAdmin)
	join(member, house, household.RoleMember)
	join(admin1, otherHouse, household.RoleAdmin)
	join(outsider, otherHouse, household.RoleMember)
	password(admin1)
	password(member)
	oidc(admin2)

	t.Run("admin lists accounts with identity sign-in methods", func(t *testing.T) {
		accounts, next, err := service.ListAccounts(ctx, admin1, house, "", 50)
		if err != nil || next != "" || len(accounts) != 3 {
			t.Fatalf("accounts=%+v next=%q err=%v", accounts, next, err)
		}
		byID := map[string]household.Account{}
		for _, account := range accounts {
			byID[account.UserID] = account
		}
		if got := byID[admin1]; got.Role != household.RoleAdmin || got.Login != "accounts_admin1" || !got.SignInMethods.Password || got.SignInMethods.OIDC {
			t.Fatalf("admin1=%+v", got)
		}
		if got := byID[admin2]; !got.SignInMethods.OIDC || got.SignInMethods.Password {
			t.Fatalf("admin2=%+v", got)
		}
		if got := byID[member]; got.Role != household.RoleMember || got.SignInMethods.OIDC {
			t.Fatalf("member=%+v", got)
		}
		page, next, err := service.ListAccounts(ctx, admin1, house, "", 2)
		if err != nil || len(page) != 2 || next == "" {
			t.Fatalf("first page=%+v next=%q err=%v", page, next, err)
		}
		rest, next, err := service.ListAccounts(ctx, admin1, house, next, 2)
		if err != nil || len(rest) != 1 || next != "" {
			t.Fatalf("second page=%+v next=%q err=%v", rest, next, err)
		}
	})

	t.Run("non-admin member is forbidden on every account route", func(t *testing.T) {
		if _, _, err := service.ListAccounts(ctx, member, house, "", 50); !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("list err=%v", err)
		}
		if _, err := service.GetAccount(ctx, member, house, admin1); !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("get err=%v", err)
		}
		if _, err := service.ChangeRole(ctx, member, house, admin1, household.RoleMember, 1); !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("change err=%v", err)
		}
	})

	t.Run("non-member and unknown household are not found", func(t *testing.T) {
		if _, _, err := service.ListAccounts(ctx, outsider, house, "", 50); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("list err=%v", err)
		}
		if _, err := service.GetAccount(ctx, outsider, house, member); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("get err=%v", err)
		}
		if _, err := service.ChangeRole(ctx, outsider, house, member, household.RoleAdmin, 1); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("change err=%v", err)
		}
		if _, err := service.GetAccount(ctx, admin1, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bff", member); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("unknown household err=%v", err)
		}
	})

	t.Run("cross-household target is not found and unchanged", func(t *testing.T) {
		before, beforeVersion := roleOf(otherHouse, outsider)
		if _, err := service.ChangeRole(ctx, admin1, house, outsider, household.RoleAdmin, 1); !errors.Is(err, household.ErrNotFound) {
			t.Fatalf("change err=%v", err)
		}
		after, afterVersion := roleOf(otherHouse, outsider)
		if before != after || beforeVersion != afterVersion {
			t.Fatalf("foreign membership changed: %s/%d -> %s/%d", before, beforeVersion, after, afterVersion)
		}
	})

	t.Run("stale version is rejected without audit or change", func(t *testing.T) {
		beforeAudit := auditCount(house)
		if _, err := service.ChangeRole(ctx, admin1, house, member, household.RoleAdmin, 7); !errors.Is(err, household.ErrPreconditionFailed) {
			t.Fatalf("err=%v", err)
		}
		if role, version := roleOf(house, member); role != household.RoleMember || version != 1 || auditCount(house) != beforeAudit {
			t.Fatalf("role=%s version=%d audit=%d", role, version, auditCount(house))
		}
	})

	t.Run("unchanged role is a no-op", func(t *testing.T) {
		beforeAudit := auditCount(house)
		account, err := service.ChangeRole(ctx, admin1, house, member, household.RoleMember, 1)
		if err != nil || account.Role != household.RoleMember || account.Version != 1 {
			t.Fatalf("account=%+v err=%v", account, err)
		}
		if auditCount(house) != beforeAudit {
			t.Fatalf("no-op wrote audit rows")
		}
	})

	t.Run("promote then self-demote keeps audit history", func(t *testing.T) {
		beforeAudit := auditCount(house)
		promoted, err := service.ChangeRole(ctx, admin1, house, member, household.RoleAdmin, 1)
		if err != nil || promoted.Role != household.RoleAdmin || promoted.Version != 2 {
			t.Fatalf("promote=%+v err=%v", promoted, err)
		}
		if auditCount(house) != beforeAudit+1 {
			t.Fatalf("promote audit=%d want %d", auditCount(house), beforeAudit+1)
		}
		demoted, err := service.ChangeRole(ctx, admin1, house, admin1, household.RoleMember, 1)
		if err != nil || demoted.Role != household.RoleMember || demoted.Version != 2 {
			t.Fatalf("self-demote=%+v err=%v", demoted, err)
		}
		if auditCount(house) != beforeAudit+2 {
			t.Fatalf("self-demote audit=%d want %d", auditCount(house), beforeAudit+2)
		}
		// The member version is now 2; an If-Match from before that change is stale.
		if _, err := service.ChangeRole(ctx, admin2, house, member, household.RoleMember, 1); !errors.Is(err, household.ErrPreconditionFailed) {
			t.Fatalf("stale after another change err=%v", err)
		}
		if auditCount(house) != beforeAudit+2 {
			t.Fatalf("stale change wrote audit rows")
		}
		var previous, next string
		if err := admin.QueryRow(ctx, `SELECT previous_role, new_role FROM household_role_changes WHERE household_id=$1::uuid AND target_user_id=$2::uuid ORDER BY changed_at DESC LIMIT 1`, house, admin1).Scan(&previous, &next); err != nil {
			t.Fatal(err)
		}
		if previous != household.RoleAdmin || next != household.RoleMember {
			t.Fatalf("audit previous=%s new=%s", previous, next)
		}
		// Restore the fixture for the remaining tests.
		if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='admin', version=1 WHERE household_id=$1::uuid AND user_id=$2::uuid`, house, member); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='admin', version=1 WHERE household_id=$1::uuid AND user_id=$2::uuid`, house, admin1); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("demoting the last admin is rejected", func(t *testing.T) {
		lastHouse := newHousehold("last admin house")
		join(admin1, lastHouse, household.RoleAdmin)
		if _, err := service.ChangeRole(ctx, admin1, lastHouse, admin1, household.RoleMember, 1); !errors.Is(err, household.ErrLastAdmin) {
			t.Fatalf("err=%v", err)
		}
		if role, version := roleOf(lastHouse, admin1); role != household.RoleAdmin || version != 1 {
			t.Fatalf("role=%s version=%d", role, version)
		}
		if auditCount(lastHouse) != 0 {
			t.Fatalf("last-admin rejection wrote audit rows")
		}
	})

	t.Run("concurrent admins demoting each other leave exactly one admin", func(t *testing.T) {
		concurrentHouse := newHousehold("concurrent house")
		join(admin1, concurrentHouse, household.RoleAdmin)
		join(admin2, concurrentHouse, household.RoleAdmin)
		start := make(chan struct{})
		results := make(chan error, 2)
		var ready sync.WaitGroup
		ready.Add(2)
		for _, pair := range [][2]string{{admin1, admin2}, {admin2, admin1}} {
			actor, target := pair[0], pair[1]
			go func() {
				ready.Done()
				<-start
				_, err := service.ChangeRole(ctx, actor, concurrentHouse, target, household.RoleMember, 1)
				results <- err
			}()
		}
		ready.Wait()
		close(start)
		successes, forbidden := 0, 0
		for i := 0; i < 2; i++ {
			err := <-results
			switch {
			case err == nil:
				successes++
			case errors.Is(err, household.ErrAdminRequired):
				forbidden++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		}
		if successes != 1 || forbidden != 1 {
			t.Fatalf("successes=%d forbidden=%d", successes, forbidden)
		}
		var admins int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE household_id=$1::uuid AND role='admin'`, concurrentHouse).Scan(&admins); err != nil || admins != 1 {
			t.Fatalf("admins=%d err=%v", admins, err)
		}
	})

	t.Run("demoted actor cannot create invitations", func(t *testing.T) {
		demotedHouse := newHousehold("demotion invitation house")
		join(admin1, demotedHouse, household.RoleAdmin)
		join(admin2, demotedHouse, household.RoleAdmin)
		if _, err := service.ChangeRole(ctx, admin1, demotedHouse, admin2, household.RoleMember, 1); err != nil {
			t.Fatalf("demote: %v", err)
		}
		repository := identitypostgres.NewInvitationRepository(app, func(tx householddb.DBTX) identitypostgres.InvitationHouseholdService {
			return MembershipServiceFor(tx)
		}, testMemberPeople)
		invitations, err := identityapp.NewInvitationService(repository, clock)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invitations.CreateInvitation(ctx, admin2, demotedHouse, "after-demote"); !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("create invitation err=%v", err)
		}
		var count int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations WHERE household_id=$1::uuid`, demotedHouse).Scan(&count); err != nil || count != 0 {
			t.Fatalf("invitations=%d err=%v", count, err)
		}
	})

	t.Run("member cannot self-promote", func(t *testing.T) {
		selfHouse := newHousehold("self promotion house")
		selfAdmin := newUser("self_promotion_admin")
		join(selfAdmin, selfHouse, household.RoleAdmin)
		join(member, selfHouse, household.RoleMember)
		if _, err := service.ChangeRole(ctx, member, selfHouse, member, household.RoleAdmin, 1); !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("self-promote err=%v", err)
		}
		if role, version := roleOf(selfHouse, member); role != household.RoleMember || version != 1 {
			t.Fatalf("self-promote changed membership: role=%s version=%d", role, version)
		}
		if auditCount(selfHouse) != 0 {
			t.Fatal("self-promote wrote audit rows")
		}
	})

	t.Run("admin elsewhere is forbidden when only a member here", func(t *testing.T) {
		memberHouse := newHousehold("actor member house")
		target := newUser("member_house_target")
		join(admin1, memberHouse, household.RoleMember)
		join(target, memberHouse, household.RoleMember)
		if _, err := service.ChangeRole(ctx, admin1, memberHouse, target, household.RoleAdmin, 1); !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("admin elsewhere err=%v", err)
		}
		if role, version := roleOf(memberHouse, target); role != household.RoleMember || version != 1 {
			t.Fatalf("forbidden cross-household change mutated target: role=%s version=%d", role, version)
		}
		if auditCount(memberHouse) != 0 {
			t.Fatal("forbidden cross-household change wrote audit rows")
		}
	})

	t.Run("runtime role cannot mutate the role audit table", func(t *testing.T) {
		if _, err := app.Exec(ctx, `UPDATE household_role_changes SET new_role = new_role`); err == nil {
			t.Fatal("runtime role updated household_role_changes")
		}
		if _, err := app.Exec(ctx, `DELETE FROM household_role_changes`); err == nil {
			t.Fatal("runtime role deleted from household_role_changes")
		}
	})
}

// TestInFlightDemotionForbidsAdminWrites holds an uncommitted demotion of the
// acting admin while each admin operation starts. The operation must block on
// the household row lock, then observe the committed member role and fail with
// ErrAdminRequired without changing any state.
func TestInFlightDemotionForbidsAdminWrites(t *testing.T) {
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL account tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := connect(t, ctx, adminURL), connect(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		t.Helper()
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households, household_role_changes CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatalf("reset fixtures: %v", err)
		}
	}
	reset()
	defer reset()

	clock := func() time.Time { return time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC) }
	accounts := AccountServiceFor(app, identitypostgres.New(app, nil), identitypostgres.New(app, nil), emptyResetLookup{}, clock)
	invitations, err := identityapp.NewInvitationService(identitypostgres.NewInvitationRepository(app, func(tx householddb.DBTX) identitypostgres.InvitationHouseholdService {
		return MembershipServiceFor(tx)
	}, testMemberPeople), clock)
	if err != nil {
		t.Fatal(err)
	}

	newUser := func(login string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ($1) RETURNING id::text`, login).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	newHousehold := func(name string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO households(name, timezone) VALUES ($1, 'UTC') RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	join := func(userID, householdID, role string) {
		t.Helper()
		if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id, household_id, role) VALUES ($1::uuid, $2::uuid, $3)`, userID, householdID, role); err != nil {
			t.Fatal(err)
		}
	}
	invitationCount := func(householdID string) int {
		t.Helper()
		var count int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations WHERE household_id=$1::uuid`, householdID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	pendingInvitation := func(householdID, invitationID string) bool {
		t.Helper()
		var pending bool
		if err := admin.QueryRow(ctx, `SELECT revoked_at IS NULL AND accepted_at IS NULL FROM household_invitations WHERE household_id=$1::uuid AND id=$2::uuid`, householdID, invitationID).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		return pending
	}
	roleOf := func(householdID, userID string) (string, int64) {
		t.Helper()
		var role string
		var version int64
		if err := admin.QueryRow(ctx, `SELECT role, version FROM household_memberships WHERE household_id=$1::uuid AND user_id=$2::uuid`, householdID, userID).Scan(&role, &version); err != nil {
			t.Fatal(err)
		}
		return role, version
	}
	auditCount := func(householdID string) int {
		t.Helper()
		var count int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_role_changes WHERE household_id=$1::uuid`, householdID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	// holdDemotion starts write while a separate connection holds the household
	// row lock and has uncommitted role='member' for actor. It waits until the
	// write is observed waiting on the lock, commits the demotion, and returns
	// the write's result.
	holdDemotion := func(householdID, actorID string, write func() error) error {
		t.Helper()
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = tx.Rollback(rollbackCtx)
		}()
		if _, err := tx.Exec(ctx, `SELECT 1 FROM households WHERE id=$1::uuid FOR NO KEY UPDATE`, householdID); err != nil {
			t.Fatalf("lock household: %v", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE household_memberships SET role='member' WHERE household_id=$1::uuid AND user_id=$2::uuid`, householdID, actorID); err != nil {
			t.Fatalf("uncommitted demotion: %v", err)
		}
		done := make(chan error, 1)
		go func() { done <- write() }()
		deadline := time.Now().Add(15 * time.Second)
		for {
			select {
			case err := <-done:
				t.Fatalf("write returned before the demotion committed: %v", err)
			default:
			}
			var waiting int
			// Poll from the runtime pool: pg_stat_activity hides other-role
			// sessions from the migrator connection, while a role can see its
			// own sessions.
			if err := app.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid() AND query LIKE '%FOR NO KEY UPDATE%'`).Scan(&waiting); err != nil {
				t.Fatalf("poll lock waiters: %v", err)
			}
			if waiting > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("write never blocked on the household row lock")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit demotion: %v", err)
		}
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			t.Fatalf("write did not finish: %v", ctx.Err())
			return nil
		}
	}

	t.Run("invitation create", func(t *testing.T) {
		reset()
		actor, other := newUser("inflight_create_admin"), newUser("inflight_create_member")
		house := newHousehold("inflight create house")
		join(actor, house, household.RoleAdmin)
		join(other, house, household.RoleMember)
		err := holdDemotion(house, actor, func() error {
			_, e := invitations.CreateInvitation(ctx, actor, house, "inflight-create")
			return e
		})
		if !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("create invitation err=%v", err)
		}
		if count := invitationCount(house); count != 0 {
			t.Fatalf("invitations created during in-flight demotion: %d", count)
		}
		if role, version := roleOf(house, actor); role != household.RoleMember || version != 1 {
			t.Fatalf("actor role=%s version=%d", role, version)
		}
		if auditCount(house) != 0 {
			t.Fatal("in-flight demotion wrote audit rows")
		}
	})

	t.Run("invitation revoke", func(t *testing.T) {
		reset()
		actor, other := newUser("inflight_revoke_admin"), newUser("inflight_revoke_member")
		house := newHousehold("inflight revoke house")
		join(actor, house, household.RoleAdmin)
		join(other, house, household.RoleMember)
		created, err := invitations.CreateInvitation(ctx, actor, house, "inflight-revoke")
		if err != nil {
			t.Fatal(err)
		}
		err = holdDemotion(house, actor, func() error {
			return invitations.RevokeInvitation(ctx, actor, house, created.Invitation.ID)
		})
		if !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("revoke invitation err=%v", err)
		}
		if !pendingInvitation(house, created.Invitation.ID) || invitationCount(house) != 1 {
			t.Fatal("in-flight demotion revoked the invitation")
		}
	})

	t.Run("role change of another member", func(t *testing.T) {
		reset()
		actor, target := newUser("inflight_role_admin"), newUser("inflight_role_member")
		house := newHousehold("inflight role house")
		join(actor, house, household.RoleAdmin)
		join(target, house, household.RoleMember)
		err := holdDemotion(house, actor, func() error {
			_, e := accounts.ChangeRole(ctx, actor, house, target, household.RoleAdmin, 1)
			return e
		})
		if !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("change role err=%v", err)
		}
		if role, version := roleOf(house, target); role != household.RoleMember || version != 1 {
			t.Fatalf("target role=%s version=%d", role, version)
		}
		if auditCount(house) != 0 {
			t.Fatal("in-flight demotion wrote audit rows")
		}
	})

	t.Run("account list read", func(t *testing.T) {
		reset()
		actor, other := newUser("inflight_list_admin"), newUser("inflight_list_member")
		house := newHousehold("inflight list house")
		join(actor, house, household.RoleAdmin)
		join(other, house, household.RoleMember)
		err := holdDemotion(house, actor, func() error {
			_, _, e := accounts.ListAccounts(ctx, actor, house, "", 50)
			return e
		})
		if !errors.Is(err, household.ErrAdminRequired) {
			t.Fatalf("list accounts err=%v", err)
		}
	})
}
