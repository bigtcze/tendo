package postgres

import (
	"errors"
	"fmt"
	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestInvitationPostgresConcurrentAcceptOneWinner(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	inv, e := svc.CreateInvitation(ctx, owner, house, "concurrent-accept")
	if e != nil {
		t.Fatal(e)
	}
	start := make(chan struct{})
	errs := make(chan error, 10)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			_, x := svc.AcceptInvitationNewAccount(ctx, inv.Token, fmt.Sprintf("winner_%d", n), "a long secure password")
			if x == nil {
				successes.Add(1)
			} else if !errors.Is(x, identity.ErrInvalidInvitation) {
				errs <- x
			}
		}(n)
	}
	close(start)
	wg.Wait()
	close(errs)
	for x := range errs {
		t.Error(x)
	}
	var users, members int
	if e := admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts WHERE login LIKE 'winner_%'`).Scan(&users); e != nil {
		t.Fatal(e)
	}
	if e := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE household_id=$1::uuid AND role='member'`, house).Scan(&members); e != nil {
		t.Fatal(e)
	}
	if successes.Load() != 1 || users != 1 || members != 1 {
		t.Fatalf("success=%d users=%d members=%d", successes.Load(), users, members)
	}
}
func TestInvitationPostgresExistingAccountPoliciesAndRevokeIdempotence(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	clock := time.Now().UTC().Truncate(time.Second)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), func() time.Time { return clock })
	inv, e := svc.CreateInvitation(ctx, owner, house, "existing-policy")
	if e != nil {
		t.Fatal(e)
	}
	var member string
	if e = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('existing_member') RETURNING id::text`).Scan(&member); e != nil {
		t.Fatal(e)
	}
	_, e = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, member, house)
	if e != nil {
		t.Fatal(e)
	}
	_, e = svc.AcceptInvitationExistingAccount(ctx, member, inv.Token)
	if !errors.Is(e, identity.ErrAlreadyMember) {
		t.Fatalf("owner join=%v", e)
	}
	var role string
	if e = admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, member, house).Scan(&role); e != nil {
		t.Fatal(e)
	}
	if role != "owner" {
		t.Fatalf("role downgraded to %s", role)
	}
	other, e := svc.CreateInvitation(ctx, owner, house, "revoke-pending")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.RevokeInvitation(ctx, owner, house, other.Invitation.ID); e != nil {
		t.Fatal(e)
	}
	var first time.Time
	if e = admin.QueryRow(ctx, `SELECT revoked_at FROM household_invitations WHERE id=$1::uuid`, other.Invitation.ID).Scan(&first); e != nil {
		t.Fatal(e)
	}
	clock = clock.Add(time.Second)
	if e = svc.RevokeInvitation(ctx, owner, house, other.Invitation.ID); e != nil {
		t.Fatal(e)
	}
	var second time.Time
	if e = admin.QueryRow(ctx, `SELECT revoked_at FROM household_invitations WHERE id=$1::uuid`, other.Invitation.ID).Scan(&second); e != nil {
		t.Fatal(e)
	}
	if !first.Equal(second) {
		t.Fatalf("revoke timestamp changed %s => %s", first, second)
	}
	acceptedInvite, e := svc.CreateInvitation(ctx, owner, house, "accepted-before-revoke")
	if e != nil {
		t.Fatal(e)
	}
	acceptedUser, e := svc.AcceptInvitationNewAccount(ctx, acceptedInvite.Token, "accepted_then_revoke", "a long enough acceptance password")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.RevokeInvitation(ctx, owner, house, acceptedInvite.Invitation.ID); e != nil {
		t.Fatal(e)
	}
	var accepted, revoked bool
	var retained int
	if e = admin.QueryRow(ctx, `SELECT accepted_at IS NOT NULL, revoked_at IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, acceptedInvite.Invitation.ID).Scan(&accepted, &revoked); e != nil || !accepted || revoked {
		t.Fatalf("accepted=%v revoked=%v err=%v", accepted, revoked, e)
	}
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, acceptedUser.UserID, house).Scan(&retained); e != nil || retained != 1 {
		t.Fatalf("membership=%d err=%v", retained, e)
	}
	noMember, e := svc.CreateInvitation(ctx, owner, house, "no-memberships")
	if e != nil {
		t.Fatal(e)
	}
	var fresh string
	if e = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('fresh_existing') RETURNING id::text`).Scan(&fresh); e != nil {
		t.Fatal(e)
	}
	result, e := svc.AcceptInvitationExistingAccount(ctx, fresh, noMember.Token)
	if e != nil {
		t.Fatal(e)
	}
	if result.Role != household.RoleMember {
		t.Fatalf("role=%s", result.Role)
	}
	var def string
	if e = admin.QueryRow(ctx, `SELECT default_household_id::text FROM user_accounts WHERE id=$1::uuid`, fresh).Scan(&def); e != nil {
		t.Fatal(e)
	}
	if def != house {
		t.Fatalf("default=%s", def)
	}
}
