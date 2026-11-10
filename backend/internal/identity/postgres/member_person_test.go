package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/identity"
	subjectpostgres "github.com/bigtcze/tendo/backend/internal/subject/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func memberPeopleFactory(tx householddb.DBTX) identity.MemberPersonProvisioner {
	return subjectpostgres.NewMemberPersonProvisioner(tx)
}

func countLinkedPeople(t *testing.T, ctx context.Context, admin *pgxpool.Pool, householdID, userID string) int {
	t.Helper()
	var count int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM subjects WHERE household_id=$1::uuid AND linked_user_id=$2::uuid AND type='person'`, householdID, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestInvitationAcceptanceCreatesExactlyOneMemberPerson(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	service, err := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	newInvitation := func(key string) identity.InvitationResult {
		t.Helper()
		result, err := service.CreateInvitation(ctx, owner, house, key)
		if err != nil {
			t.Fatalf("create invitation: %v", err)
		}
		return result
	}

	invited, err := service.AcceptInvitationNewAccount(ctx, newInvitation("new-account").Token, "invited_person", "correct horse battery")
	if err != nil {
		t.Fatalf("accept new account: %v", err)
	}
	if got := countLinkedPeople(t, ctx, admin, house, invited.UserID); got != 1 {
		t.Fatalf("new-account linked people=%d want 1", got)
	}
	var name string
	if err := admin.QueryRow(ctx, `SELECT name FROM subjects WHERE household_id=$1::uuid AND linked_user_id=$2::uuid`, house, invited.UserID).Scan(&name); err != nil || name != "invited_person" {
		t.Fatalf("new-account person name=%q err=%v", name, err)
	}
	if _, err := service.AcceptInvitationNewAccount(ctx, newInvitation("unused").Token, "invited_person", "correct horse battery"); !errors.Is(err, identity.ErrLoginUnavailable) {
		t.Fatalf("second acceptance err=%v", err)
	}
	if got := countLinkedPeople(t, ctx, admin, house, invited.UserID); got != 1 {
		t.Fatalf("repeat acceptance linked people=%d want 1", got)
	}

	var existing string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('existing_person') RETURNING id::text`).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptInvitationExistingAccount(ctx, existing, newInvitation("existing-account").Token); err != nil {
		t.Fatalf("accept existing account: %v", err)
	}
	if got := countLinkedPeople(t, ctx, admin, house, existing); got != 1 {
		t.Fatalf("existing-account linked people=%d want 1", got)
	}
	var existingName string
	if err := admin.QueryRow(ctx, `SELECT name FROM subjects WHERE household_id=$1::uuid AND linked_user_id=$2::uuid`, house, existing).Scan(&existingName); err != nil || existingName != "existing_person" {
		t.Fatalf("existing-account person name=%q err=%v", existingName, err)
	}
}

func TestConcurrentMemberPersonProvisioningCreatesOne(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	_ = owner
	provisioner := subjectpostgres.NewMemberPersonProvisioner(app)
	const workers = 8
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- provisioner.EnsureMemberPerson(ctx, house, owner, "owner_login")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent provisioning: %v", err)
		}
	}
	if got := countLinkedPeople(t, ctx, admin, house, owner); got != 1 {
		t.Fatalf("concurrently provisioned people=%d want 1", got)
	}
}

type failingMemberPeople struct{}

func (failingMemberPeople) EnsureMemberPerson(context.Context, string, string, string) error {
	return errors.New("injected member person provisioning failure")
}

func TestInvitationAcceptanceRollsBackWhenPersonProvisioningFails(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	service, err := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, func(householddb.DBTX) identity.MemberPersonProvisioner {
		return failingMemberPeople{}
	}), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := service.CreateInvitation(ctx, owner, house, "rollback-person")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptInvitationNewAccount(ctx, invitation.Token, "rollback_person", "correct horse battery"); err == nil {
		t.Fatal("acceptance succeeded despite provisioning failure")
	}
	var users, memberships, people int
	if err := admin.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM user_accounts WHERE login='rollback_person'),
		(SELECT count(*) FROM household_memberships m JOIN user_accounts u ON u.id=m.user_id WHERE u.login='rollback_person'),
		(SELECT count(*) FROM subjects s JOIN user_accounts u ON u.id=s.linked_user_id WHERE u.login='rollback_person')`).Scan(&users, &memberships, &people); err != nil {
		t.Fatal(err)
	}
	if users != 0 || memberships != 0 || people != 0 {
		t.Fatalf("rollback left users=%d memberships=%d people=%d", users, memberships, people)
	}
	var accepted any
	if err := admin.QueryRow(ctx, `SELECT accepted_at FROM household_invitations WHERE id=$1::uuid`, invitation.Invitation.ID).Scan(&accepted); err != nil || accepted != nil {
		t.Fatalf("invitation accepted_at=%v err=%v", accepted, err)
	}
}
