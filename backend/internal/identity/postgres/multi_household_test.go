package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	"github.com/bigtcze/tendo/backend/internal/identity"
)

// TestExistingAccountInvitationJoinsSecondHouseholdKeepingIndependentRole shows
// the new join-on-invite rule: an account that already administers one
// household accepts an invitation to a second one, keeps its admin role in the
// first, gains its independent member role in the second, and moves its default.
func TestExistingAccountInvitationJoinsSecondHouseholdKeepingIndependentRole(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, houseA := invitationFixture(t, ctx, admin)
	var houseB string
	if e := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('second home','UTC') RETURNING id::text`).Scan(&houseB); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'admin')`, owner, houseB); e != nil {
		t.Fatal(e)
	}
	var invitee string
	if e := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('multi_house_invitee') RETURNING id::text`).Scan(&invitee); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'admin')`, invitee, houseA); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, invitee, houseA); e != nil {
		t.Fatal(e)
	}
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	inv, e := svc.CreateInvitation(ctx, owner, houseB, "join-second-household")
	if e != nil {
		t.Fatal(e)
	}
	result, e := svc.AcceptInvitationExistingAccount(ctx, invitee, inv.Token)
	if e != nil {
		t.Fatalf("join-on-invite refused an account with its own default household: %v", e)
	}
	if result.UserID != invitee || result.HouseholdID != houseB || result.Role != household.RoleMember {
		t.Fatalf("result=%+v", result)
	}
	var roleA, roleB, defaultHouse string
	if e = admin.QueryRow(ctx, `SELECT
		(SELECT role FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid),
		(SELECT role FROM household_memberships WHERE user_id=$1::uuid AND household_id=$3::uuid),
		(SELECT default_household_id::text FROM user_accounts WHERE id=$1::uuid)`, invitee, houseA, houseB).Scan(&roleA, &roleB, &defaultHouse); e != nil {
		t.Fatal(e)
	}
	if roleA != "admin" || roleB != "member" || defaultHouse != houseB {
		t.Fatalf("role A=%q role B=%q default=%q", roleA, roleB, defaultHouse)
	}
	var accepted bool
	if e = admin.QueryRow(ctx, `SELECT accepted_at IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, inv.Invitation.ID).Scan(&accepted); e != nil || !accepted {
		t.Fatalf("accepted=%v err=%v", accepted, e)
	}
}

// TestExistingAccountInvitationAlreadyMemberIsRejectedWithoutWrites keeps the
// one surviving refusal: accepting an invitation to a household the account
// already belongs to is a 409 and changes no state.
func TestExistingAccountInvitationAlreadyMemberIsRejectedWithoutWrites(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	var member string
	if e := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('already_member_user') RETURNING id::text`).Scan(&member); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, member, house); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, member, house); e != nil {
		t.Fatal(e)
	}
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	inv, e := svc.CreateInvitation(ctx, owner, house, "already-member")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.AcceptInvitationExistingAccount(ctx, member, inv.Token); !errors.Is(e, identity.ErrAlreadyMember) {
		t.Fatalf("err=%v want ErrAlreadyMember", e)
	}
	var memberships int
	var role, defaultHouse string
	if e = admin.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid),
		(SELECT role FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid),
		(SELECT default_household_id::text FROM user_accounts WHERE id=$1::uuid)`, member, house).Scan(&memberships, &role, &defaultHouse); e != nil {
		t.Fatal(e)
	}
	if memberships != 1 || role != "member" || defaultHouse != house {
		t.Fatalf("memberships=%d role=%q default=%q", memberships, role, defaultHouse)
	}
	var accepted bool
	if e = admin.QueryRow(ctx, `SELECT accepted_at IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, inv.Invitation.ID).Scan(&accepted); e != nil || accepted {
		t.Fatalf("accepted=%v err=%v", accepted, e)
	}
}

// TestSessionDefaultHouseholdSwitchAndReferenceValidation exercises the PATCH
// /session persistence: one statement authorizes through membership, a same
// value rewrite is a successful no-op, malformed, unknown, and non-member
// references collapse to one error, and the reference never grants access.
func TestSessionDefaultHouseholdSwitchAndReferenceValidation(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, home := invitationFixture(t, ctx, admin)
	var second, foreign string
	if e := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('switch target','UTC') RETURNING id::text`).Scan(&second); e != nil {
		t.Fatal(e)
	}
	if e := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('not a membership','UTC') RETURNING id::text`).Scan(&foreign); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, owner, second); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, owner, home); e != nil {
		t.Fatal(e)
	}
	service, e := identity.NewSessionService(New(app, nil), time.Now)
	if e != nil {
		t.Fatal(e)
	}
	readDefault := func() string {
		t.Helper()
		var got string
		if e := admin.QueryRow(ctx, `SELECT default_household_id::text FROM user_accounts WHERE id=$1::uuid`, owner).Scan(&got); e != nil {
			t.Fatal(e)
		}
		return got
	}

	if e = service.SetDefaultHousehold(ctx, owner, second); e != nil {
		t.Fatalf("switch failed: %v", e)
	}
	if got := readDefault(); got != second {
		t.Fatalf("default=%q want %q", got, second)
	}
	// Re-selecting the same value is a 200 no-op, not a conflict.
	if e = service.SetDefaultHousehold(ctx, owner, second); e != nil || readDefault() != second {
		t.Fatalf("same-value switch err=%v default=%q", e, readDefault())
	}

	for name, householdID := range map[string]string{
		"malformed":  "not-a-uuid",
		"unknown":    "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b99",
		"non-member": foreign,
	} {
		t.Run(name, func(t *testing.T) {
			if e := service.SetDefaultHousehold(ctx, owner, householdID); !errors.Is(e, identity.ErrInvalidReference) {
				t.Fatalf("err=%v want ErrInvalidReference", e)
			}
			if got := readDefault(); got != second {
				t.Fatalf("failed switch changed default to %q", got)
			}
		})
	}

	if e = service.SetDefaultHousehold(ctx, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b98", second); !errors.Is(e, identity.ErrUnauthenticated) {
		t.Fatalf("vanished user err=%v want ErrUnauthenticated", e)
	}

	// Selecting a household is never an authorization grant: the household
	// resource route still answers not found for a non-member.
	if _, e = householdpostgres.NewRepository(app).FindForMember(ctx, owner, foreign); !errors.Is(e, household.ErrNotFound) {
		t.Fatalf("non-member household read err=%v want ErrNotFound", e)
	}
}
