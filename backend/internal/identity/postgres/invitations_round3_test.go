package postgres

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInvitationMigrationV8Upgrade(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
	adminCfg := testpostgres.PoolConfigForDatabase(t, os.Getenv("TEST_DATABASE_ADMIN_URL"), dbName)
	admin.Close()
	admin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	appCfg := testpostgres.PoolConfigForDatabase(t, os.Getenv("TEST_DATABASE_URL"), dbName)
	app.Close()
	app, err = pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	if e := database.Migrate(ctx, admin); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; DROP TABLE IF EXISTS oidc_identities; ALTER TABLE items DROP CONSTRAINT IF EXISTS items_responsible_membership_fk; DROP INDEX IF EXISTS items_responsible_membership_idx; ALTER TABLE items DROP COLUMN IF EXISTS responsible_user_id; DROP TABLE IF EXISTS household_invitations; DELETE FROM tendo_schema_migrations WHERE version IN (9,10,11)`); e != nil {
		t.Fatal(e)
	}
	if e := database.ValidateSchema(ctx, app); e == nil {
		t.Fatal("v8 schema accepted")
	}
	var hid, uid string
	if e := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('upgrade v8','UTC') RETURNING id::text`).Scan(&hid); e != nil {
		t.Fatal(e)
	}
	if e := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('upgrade_v8_owner') RETURNING id::text`).Scan(&uid); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, uid, hid); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1::uuid,decode(repeat('ac',32),'hex'),now(),now()+interval '1 day')`, uid); e != nil {
		t.Fatal(e)
	}
	if e := database.Migrate(ctx, admin); e != nil {
		t.Fatal(e)
	}
	var data int
	if e := admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts u JOIN household_memberships m ON m.user_id=u.id JOIN user_sessions s ON s.user_id=u.id WHERE u.id=$1::uuid AND m.household_id=$2::uuid AND m.role='owner'`, uid, hid).Scan(&data); e != nil || data != 1 {
		t.Fatalf("preserved=%d err=%v", data, e)
	}
	var version int
	var dirty bool
	if e := admin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); e != nil || version != 11 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", version, dirty, e)
	}
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	inv, e := svc.CreateInvitation(ctx, uid, hid, "upgrade-invite")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.AcceptInvitationNewAccount(ctx, inv.Token, "upgrade_accept", "long enough upgrade password"); e != nil {
		t.Fatal(e)
	}
}
func TestInvitationParallelExistingAccountInvitesAcrossHouseholds(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, h1 := invitationFixture(t, ctx, admin)
	var h2 string
	if e := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('parallel second','UTC') RETURNING id::text`).Scan(&h2); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, owner, h2); e != nil {
		t.Fatal(e)
	}
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	for n := 0; n < 10; n++ {
		var user string
		if e := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("multi_house_user_%d", n)).Scan(&user); e != nil {
			t.Fatal(e)
		}
		i1, e := svc.CreateInvitation(ctx, owner, h1, fmt.Sprintf("multi-one-%d", n))
		if e != nil {
			t.Fatal(e)
		}
		i2, e := svc.CreateInvitation(ctx, owner, h2, fmt.Sprintf("multi-two-%d", n))
		if e != nil {
			t.Fatal(e)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, token := range []string{i1.Token, i2.Token} {
			wg.Add(1)
			go func(tok string) {
				defer wg.Done()
				<-start
				_, x := svc.AcceptInvitationExistingAccount(ctx, user, tok)
				results <- x
			}(token)
		}
		close(start)
		wg.Wait()
		close(results)
		success, conflict := 0, 0
		for x := range results {
			if x == nil {
				success++
			} else if errors.Is(x, identity.ErrHouseholdConflict) {
				conflict++
			} else {
				t.Fatalf("accept error=%v", x)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("iteration=%d success=%d conflict=%d", n, success, conflict)
		}
		var def string
		if e = admin.QueryRow(ctx, `SELECT default_household_id::text FROM user_accounts WHERE id=$1::uuid`, user).Scan(&def); e != nil {
			t.Fatal(e)
		}
		var joined, totalMemberships int
		if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, user, def).Scan(&joined); e != nil || joined != 1 {
			t.Fatalf("membership=%d err=%v", joined, e)
		}
		if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid`, user).Scan(&totalMemberships); e != nil || totalMemberships != 1 {
			t.Fatalf("total memberships=%d err=%v", totalMemberships, e)
		}
		other := i1.Invitation.ID
		if def == h1 {
			other = i2.Invitation.ID
		}
		var accepted bool
		if e = admin.QueryRow(ctx, `SELECT accepted_at IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, other).Scan(&accepted); e != nil || accepted {
			t.Fatalf("other accepted=%v err=%v", accepted, e)
		}
	}
}
func TestInvitationDifferentDefaultAndElsewhereMembershipConflict(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, h1 := invitationFixture(t, ctx, admin)
	var h2 string
	if e := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('second conflict home','UTC') RETURNING id::text`).Scan(&h2); e != nil {
		t.Fatal(e)
	}
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	for n, withDefault := range []bool{true, false} {
		var u string
		if e := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("conflict_user_%d", n)).Scan(&u); e != nil {
			t.Fatal(e)
		}
		if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, u, h2); e != nil {
			t.Fatal(e)
		}
		if withDefault {
			if _, e := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, u, h2); e != nil {
				t.Fatal(e)
			}
		}
		inv, e := svc.CreateInvitation(ctx, owner, h1, fmt.Sprintf("conflict_inv_%d", n))
		if e != nil {
			t.Fatal(e)
		}
		_, e = svc.AcceptInvitationExistingAccount(ctx, u, inv.Token)
		if !errors.Is(e, identity.ErrHouseholdConflict) {
			t.Fatalf("conflict=%v", e)
		}
		var accepted bool
		var members int
		if e = admin.QueryRow(ctx, `SELECT accepted_at IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, inv.Invitation.ID).Scan(&accepted); e != nil || accepted {
			t.Fatalf("accepted=%v err=%v", accepted, e)
		}
		if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, u, h1).Scan(&members); e != nil || members != 0 {
			t.Fatalf("members=%d err=%v", members, e)
		}
	}
}
func TestInvitationMemberOwnerPoliciesDefaultMembershipAndNoChanges(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, h := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	inv, e := svc.CreateInvitation(ctx, owner, h, "member-policy")
	if e != nil {
		t.Fatal(e)
	}
	var member, nonmember string
	for login, dest := range map[string]*string{"member_policy": &member, "nonmember_policy": &nonmember} {
		if e = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, login).Scan(dest); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, member, h); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, member, h); e != nil {
		t.Fatal(e)
	}
	var before int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CreateInvitation(ctx, member, h, "denied"); !errors.Is(e, household.ErrForbidden) {
		t.Fatalf("member create=%v", e)
	}
	if _, _, e = svc.ListInvitations(ctx, member, h, 10, ""); !errors.Is(e, household.ErrForbidden) {
		t.Fatalf("member list=%v", e)
	}
	if e = svc.RevokeInvitation(ctx, member, h, inv.Invitation.ID); !errors.Is(e, household.ErrForbidden) {
		t.Fatalf("member revoke=%v", e)
	}
	if _, e = svc.CreateInvitation(ctx, nonmember, h, "not-member"); !errors.Is(e, household.ErrNotFound) {
		t.Fatalf("nonmember=%v", e)
	}
	if _, e = svc.CreateInvitation(ctx, owner, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b6f", "missing-house"); !errors.Is(e, household.ErrNotFound) {
		t.Fatalf("missing household=%v", e)
	}
	var after int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations`).Scan(&after); e != nil || after != before {
		t.Fatalf("rows %d -> %d err=%v", before, after, e)
	}
}
