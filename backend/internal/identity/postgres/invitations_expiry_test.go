package postgres

import (
	"errors"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"testing"
	"time"
)

func TestInvitationExpiryBoundariesAndListStatuses(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	clock := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), func() time.Time { return clock })
	early, e := svc.CreateInvitation(ctx, owner, house, "expiry-early")
	if e != nil {
		t.Fatal(e)
	}
	clock = early.Invitation.ExpiresAt.Add(-time.Second)
	if _, e = svc.AcceptInvitationNewAccount(ctx, early.Token, "expiry_early_user", "a sufficiently long password"); e != nil {
		t.Fatal(e)
	}
	exact, e := svc.CreateInvitation(ctx, owner, house, "expiry-exact")
	if e != nil {
		t.Fatal(e)
	}
	clock = exact.Invitation.ExpiresAt
	if _, e = svc.AcceptInvitationNewAccount(ctx, exact.Token, "expiry_exact_user", "a sufficiently long password"); !errors.Is(e, identity.ErrInvalidInvitation) {
		t.Fatalf("exact expiry accept=%v", e)
	}
	var accepted bool
	if e = admin.QueryRow(ctx, `SELECT accepted_at IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, exact.Invitation.ID).Scan(&accepted); e != nil || accepted {
		t.Fatalf("accepted=%v err=%v", accepted, e)
	}
	var users int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts WHERE login='expiry_exact_user'`).Scan(&users); e != nil || users != 0 {
		t.Fatalf("user count=%d err=%v", users, e)
	}
	revoked, e := svc.CreateInvitation(ctx, owner, house, "expiry-revoked")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.RevokeInvitation(ctx, owner, house, revoked.Invitation.ID); e != nil {
		t.Fatal(e)
	}
	clock = exact.Invitation.ExpiresAt.Add(time.Second)
	rows, _, e := svc.ListInvitations(ctx, owner, house, 100, "")
	if e != nil {
		t.Fatal(e)
	}
	statuses := map[string]bool{}
	for _, row := range rows {
		statuses[row.Status] = true
	}
	for _, status := range []string{"accepted", "revoked", "expired"} {
		if !statuses[status] {
			t.Fatalf("missing %s in statuses=%v", status, statuses)
		}
	}
	pending, e := svc.CreateInvitation(ctx, owner, house, "expiry-pending")
	if e != nil {
		t.Fatal(e)
	}
	if pending.Invitation.Status != "pending" {
		t.Fatalf("pending status=%s", pending.Invitation.Status)
	}
	listed, _, e := svc.ListInvitations(ctx, owner, house, 100, "")
	if e != nil {
		t.Fatal(e)
	}
	statuses = map[string]bool{}
	for _, row := range listed {
		statuses[row.Status] = true
	}
	for _, status := range []string{"pending", "accepted", "revoked", "expired"} {
		if !statuses[status] {
			t.Fatalf("list missing status %s: %v", status, statuses)
		}
	}
}
