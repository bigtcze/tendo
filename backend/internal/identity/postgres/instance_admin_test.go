package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/jackc/pgx/v5/pgxpool"
)

// assertNoInstanceAdminGrant refuses any installation-administrator row for the
// account. It is used by the "never granted by" coverage.
func assertNoInstanceAdminGrant(t *testing.T, ctx context.Context, admin *pgxpool.Pool, userID string) {
	t.Helper()
	var grants int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM instance_admins WHERE user_id=$1::uuid`, userID).Scan(&grants); err != nil || grants != 0 {
		t.Fatalf("user %s received %d instance administrator grants", userID, grants)
	}
}

// TestSessionPostgresResolvesInstanceAdmin covers case (e): the capability is
// read from instance_admins on every login and authentication, never cached in
// the session row, so a grant made after a session exists is visible on the
// next request.
func TestSessionPostgresResolvesInstanceAdmin(t *testing.T) {
	appURL, adminURL := testURLs(t)
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
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatalf("reset fixtures: %v", err)
		}
	}
	reset()
	defer reset()
	repo := New(app, ownerFactory, memberPeopleFactory)
	const password = "correct horse battery"
	if err := identity.NewSetupService(repo).CreateOwner(ctx, identity.SetupInput{Login: "owner_admin", Password: password, HouseholdName: "Home", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	service, err := identity.NewSessionService(repo, nil)
	if err != nil {
		t.Fatal(err)
	}

	ownerSession, err := service.Login(ctx, "owner_admin", password)
	if err != nil {
		t.Fatal(err)
	}
	if !ownerSession.Principal.InstanceAdmin {
		t.Fatal("setup owner login reported instanceAdmin=false")
	}
	if info, err := service.Authenticate(ctx, ownerSession.Token); err != nil || !info.InstanceAdmin {
		t.Fatalf("setup owner session instanceAdmin=%v err=%v", info.InstanceAdmin, err)
	}

	hash, err := security.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	var memberID string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('plain_member') RETURNING id::text`).Scan(&memberID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO local_credentials(user_id, password_hash) VALUES ($1::uuid, $2)`, memberID, hash); err != nil {
		t.Fatal(err)
	}
	memberSession, err := service.Login(ctx, "plain_member", password)
	if err != nil {
		t.Fatal(err)
	}
	if memberSession.Principal.InstanceAdmin {
		t.Fatal("ungranted member login reported instanceAdmin=true")
	}
	if info, err := service.Authenticate(ctx, memberSession.Token); err != nil || info.InstanceAdmin {
		t.Fatalf("ungranted member session instanceAdmin=%v err=%v", info.InstanceAdmin, err)
	}

	if _, err := admin.Exec(ctx, `INSERT INTO instance_admins(user_id, granted_at, grant_source) VALUES ($1::uuid, now(), 'cli')`, memberID); err != nil {
		t.Fatal(err)
	}
	if info, err := service.Authenticate(ctx, memberSession.Token); err != nil || !info.InstanceAdmin {
		t.Fatalf("granted member session instanceAdmin=%v err=%v", info.InstanceAdmin, err)
	}
	if relogin, err := service.Login(ctx, "plain_member", password); err != nil || !relogin.Principal.InstanceAdmin {
		t.Fatalf("granted member login instanceAdmin=%v err=%v", relogin.Principal.InstanceAdmin, err)
	}
}

// TestInstanceAdminNotGrantedByInvitationOrOIDC covers case (d): invitation
// acceptance and OIDC JIT account creation are never installation-administrator
// writers.
func TestInstanceAdminNotGrantedByInvitationOrOIDC(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	service, err := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := service.CreateInvitation(ctx, owner, house, "no-instance-admin")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := service.AcceptInvitationNewAccount(ctx, invitation.Token, "invited_plain", "a long sufficiently secure password")
	if err != nil {
		t.Fatal(err)
	}
	assertNoInstanceAdminGrant(t, ctx, admin, accepted.UserID)

	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	_, handleHash := jitHandle(200)
	jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-no-instance-admin"}, handleHash, now, nil)
	_, session := jitSession(201, now)
	pending, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
		HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, Login: "member-no-adm01", Session: session, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertNoInstanceAdminGrant(t, ctx, admin, pending.UserID)

	var total int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM instance_admins`).Scan(&total); err != nil || total != 0 {
		t.Fatalf("invitation/OIDC wrote %d instance administrator grants", total)
	}
}
