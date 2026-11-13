package postgres

import (
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// TestInvitationListDualAdminHouseholdIsolation covers an actor who administers
// both households. Listing H2 invitations must never surface an H1 invitation
// ID, even though the actor may list H1 invitations through H1.
func TestInvitationListDualAdminHouseholdIsolation(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	var actor string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('dual_admin_invitations') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	household := func(name string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES($1,'UTC') RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	h1, h2 := household("dual invitation home one"), household("dual invitation home two")
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'admin'),($1::uuid,$3::uuid,'admin')`, actor, h1, h2); err != nil {
		t.Fatal(err)
	}
	service, err := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), func() time.Time {
		return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	h1Invitation, err := service.CreateInvitation(ctx, actor, h1, "dual-household-h1-invitation")
	if err != nil {
		t.Fatal(err)
	}
	h2Invitation, err := service.CreateInvitation(ctx, actor, h2, "dual-household-h2-invitation")
	if err != nil {
		t.Fatal(err)
	}

	// The H1 invitation is real and listable through its own household.
	h1Rows, _, err := service.ListInvitations(ctx, actor, h1, 10, "", true)
	if err != nil || len(h1Rows) != 1 || h1Rows[0].ID != h1Invitation.Invitation.ID || h1Rows[0].HouseholdID != h1 {
		t.Fatalf("H1 listing=%+v err=%v", h1Rows, err)
	}

	rows, next, err := service.ListInvitations(ctx, actor, h2, 10, "", true)
	if err != nil || next != "" || len(rows) != 1 || rows[0].ID != h2Invitation.Invitation.ID {
		t.Fatalf("H2 listing=%+v next=%q err=%v", rows, next, err)
	}
	for _, row := range rows {
		if row.ID == h1Invitation.Invitation.ID {
			t.Fatalf("H2 listing leaked the H1 invitation %s", row.ID)
		}
		if row.HouseholdID != h2 {
			t.Fatalf("H2 listing returned a %s invitation: %+v", row.HouseholdID, row)
		}
	}

	// Listing is read-only: both rows and their audit columns are untouched.
	counts := func(house string) int {
		t.Helper()
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations WHERE household_id=$1::uuid AND accepted_at IS NULL AND revoked_at IS NULL`, house).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := counts(h1); got != 1 {
		t.Fatalf("H1 invitation rows=%d", got)
	}
	if got := counts(h2); got != 1 {
		t.Fatalf("H2 invitation rows=%d", got)
	}
	// The H1 invitation is still active after the H2 listing.
	var revokedAt, acceptedAt *time.Time
	if err := admin.QueryRow(ctx, `SELECT revoked_at, accepted_at FROM household_invitations WHERE id=$1::uuid`, h1Invitation.Invitation.ID).Scan(&revokedAt, &acceptedAt); err != nil {
		t.Fatal(err)
	}
	if revokedAt != nil || acceptedAt != nil {
		t.Fatalf("H1 invitation state changed revoked=%v accepted=%v", revokedAt, acceptedAt)
	}
}
