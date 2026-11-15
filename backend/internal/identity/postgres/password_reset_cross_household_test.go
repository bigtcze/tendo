package postgres

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	"github.com/bigtcze/tendo/backend/internal/identity"
)

// A reset link replaces an account-wide password, so it is a fact about the
// account, not about the household it was issued through (ADR 0017
// amendment). A household shows the person's outstanding link when its issuer
// currently administers that household, and any admin of a household the
// person belongs to can cancel it.
func TestPasswordResetPostgresLinkIssuedInOtherHouseholdIsVisibleAndCancellable(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	other := addHousehold(t, ctx, admin, "reset visibility other")
	addMembership(t, ctx, admin, target, other, household.RoleMember)
	// The co-admin administers both households, so it holds authority over the
	// whole account; the owner administers only the first one.
	coAdmin := createLocalMember(t, ctx, admin, house, "reset_visibility_co_admin")
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid`, coAdmin); err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, coAdmin, other, household.RoleAdmin)
	// A member of the other household only, so its account list has rows that
	// must not pick up the target's link by accident.
	bystander := createLocalMember(t, ctx, admin, other, "reset_visibility_bystander")

	issued, err := service.IssuePasswordReset(ctx, coAdmin, other, target)
	if err != nil {
		t.Fatalf("co-admin issue through the other household: %v", err)
	}
	accounts := householdpostgres.AccountServiceFor(app, New(app, nil), New(app, nil), NewPasswordResetRepository(app, passwordResetFactory, clock.now), clock.now)
	for _, view := range []struct{ actor, house string }{{owner, house}, {coAdmin, house}, {coAdmin, other}} {
		account, e := accounts.GetAccount(ctx, view.actor, view.house, target)
		if e != nil {
			t.Fatal(e)
		}
		if account.PasswordReset == nil || !account.PasswordReset.CreatedAt.Equal(clock.at) || !account.PasswordReset.ExpiresAt.Equal(clock.at.Add(24*time.Hour)) {
			t.Fatalf("household %s does not show the link issued through %s: %+v", view.house, other, account.PasswordReset)
		}
	}
	listed, _, err := accounts.ListAccounts(ctx, coAdmin, other, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range listed {
		if account.UserID == bystander && account.PasswordReset != nil {
			t.Fatalf("a link for one account leaked onto another row: %+v", account)
		}
	}

	// The owner cannot issue (no authority over the whole account) but can
	// cancel through its own household, because cancelling never grants access.
	if _, err = service.IssuePasswordReset(ctx, owner, house, target); !errors.Is(err, identity.ErrPasswordResetNotPermitted) {
		t.Fatalf("single-household admin issue: %v", err)
	}
	if err = service.RevokePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatalf("cancel through the first household: %v", err)
	}
	if outstanding := outstandingResetRows(t, ctx, admin, target); outstanding != 0 {
		t.Fatalf("outstanding links after cancel=%d", outstanding)
	}
	account, err := accounts.GetAccount(ctx, owner, house, target)
	if err != nil || account.PasswordReset != nil {
		t.Fatalf("after cancel passwordReset=%+v err=%v", account.PasswordReset, err)
	}
	if _, err = service.RedeemPasswordReset(ctx, issued.Token, "a brand new long password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("cancelled link still redeems: %v", err)
	}
	assertCredential(t, ctx, admin, target, "hash:old-password-value", 1)

	// Cancelling still requires the target to be a member of the URL household:
	// an admin of a household the target is not in has nothing to cancel.
	if _, err = service.IssuePasswordReset(ctx, coAdmin, house, target); err != nil {
		t.Fatal(err)
	}
	if err = service.RevokePasswordReset(ctx, coAdmin, other, bystander); err != nil {
		t.Fatalf("cancel for a link-less bystander: %v", err)
	}
	stranger := addHousehold(t, ctx, admin, "reset visibility stranger")
	strangerAdmin := createLocalMember(t, ctx, admin, stranger, "reset_visibility_stranger_admin")
	if _, err = admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid`, strangerAdmin); err != nil {
		t.Fatal(err)
	}
	if err = service.RevokePasswordReset(ctx, strangerAdmin, stranger, target); !errors.Is(err, household.ErrNotFound) {
		t.Fatalf("cancel from a household the target is not in: %v", err)
	}
	if outstanding := outstandingResetRows(t, ctx, admin, target); outstanding != 1 {
		t.Fatalf("a refused cancel changed the link: outstanding=%d", outstanding)
	}
}

// Two co-admins resetting each other through different households used to
// deadlock: each issuance locked its own target's account row, and inserting
// the token row took the foreign-key KEY SHARE on the issuer's account row,
// which the other issuance held. The test holds both account locks before
// either insert, which is exactly the interleaving that formed the cycle.
func TestPasswordResetPostgresMutualResetDoesNotDeadlock(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	first, _, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)}
	other := addHousehold(t, ctx, admin, "reset mutual other")
	addMembership(t, ctx, admin, first, other, household.RoleAdmin)
	if _, err := admin.Exec(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1::uuid,'hash:old-password-value')`, first); err != nil {
		t.Fatal(err)
	}
	second := createLocalMember(t, ctx, admin, house, "reset_mutual_second")
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid`, second); err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, second, other, household.RoleAdmin)
	repository := NewPasswordResetRepository(app, passwordResetFactory, clock.now)

	type side struct{ actor, house, target string }
	sides := []side{{first, house, second}, {second, other, first}}
	locked := make(chan struct{}, len(sides))
	insert := make(chan struct{})
	errs := make([]error, len(sides))
	var wg sync.WaitGroup
	for i, s := range sides {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = repository.WithPasswordResetTransaction(ctx, func(tx identity.PasswordResetTransaction) error {
				if e := tx.RequireAdmin(ctx, s.actor, s.house); e != nil {
					return e
				}
				if _, e := tx.LockPasswordResetTarget(ctx, s.target); e != nil {
					return e
				}
				locked <- struct{}{}
				<-insert
				if e := tx.RevokeOutstandingPasswordResets(ctx, s.target, clock.at); e != nil {
					return e
				}
				_, e := tx.CreatePasswordReset(ctx, identity.CreatePasswordResetInput{
					HouseholdID: s.house, TargetUserID: s.target, ActorID: s.actor,
					TokenHash: sha256Sum("mutual-" + s.actor), CreatedAt: clock.at, ExpiresAt: clock.at.Add(time.Hour),
				})
				return e
			})
		}()
	}
	for range sides {
		select {
		case <-locked:
		case <-time.After(5 * time.Second):
			close(insert)
			wg.Wait()
			t.Fatalf("both account locks were not held at once: %v", errs)
		}
	}
	close(insert)
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("side %d failed (deadlock=%v): %v", i, isDeadlockError(e), e)
		}
	}
	for _, user := range []string{first, second} {
		if outstanding := outstandingResetRows(t, ctx, admin, user); outstanding != 1 {
			t.Fatalf("outstanding links for %s=%d", user, outstanding)
		}
	}

	// The same through the full service, racing repeatedly, never surfaces a
	// database error (which the HTTP layer would answer as 503).
	service := newPasswordResetService(t, app, clock)
	for round := 0; round < 10; round++ {
		start := make(chan struct{})
		for i, s := range sides {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, errs[i] = service.IssuePasswordReset(ctx, s.actor, s.house, s.target)
			}()
		}
		close(start)
		wg.Wait()
		for i, e := range errs {
			if e != nil {
				t.Fatalf("round %d side %d (deadlock=%v): %v", round, i, isDeadlockError(e), e)
			}
		}
	}
}

// A household shows a member's link only when its issuer currently
// administers that household. A link issued before the person joined, by an
// admin who has no role here, is already void at redemption, and showing it
// would reveal another household's activity.
func TestPasswordResetPostgresLinkFromNonAdminOfHouseholdStaysHidden(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	issued, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	later := addHousehold(t, ctx, admin, "reset hidden later")
	laterAdmin := createLocalMember(t, ctx, admin, later, "reset_hidden_later_admin")
	if _, err = admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid`, laterAdmin); err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, target, later, household.RoleMember)
	accounts := householdpostgres.AccountServiceFor(app, New(app, nil), New(app, nil), NewPasswordResetRepository(app, passwordResetFactory, clock.now), clock.now)
	account, err := accounts.GetAccount(ctx, laterAdmin, later, target)
	if err != nil || account.PasswordReset != nil {
		t.Fatalf("household %s shows a link its admins did not issue: %+v err=%v", later, account.PasswordReset, err)
	}
	if account, err = accounts.GetAccount(ctx, owner, house, target); err != nil || account.PasswordReset == nil {
		t.Fatalf("issuing household lost its link: %+v err=%v", account.PasswordReset, err)
	}
	// The hidden link really is void, and stays unconsumed.
	if _, err = service.RedeemPasswordReset(ctx, issued.Token, "a brand new long password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("hidden link redeems: %v", err)
	}
	assertCredential(t, ctx, admin, target, "hash:old-password-value", 1)
}

// A cancel that waits for a concurrent re-issue must also revoke the link that
// re-issue created. The two run through different households, so the household
// row lock in RequireAdmin does not order them. Without the account-row lock in
// cancel, its UPDATE waited on the old link row and then could not see the new
// one, answering 204 while a fresh link stayed usable.
func TestPasswordResetPostgresCancelRacingReissueLeavesNoLink(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	other := addHousehold(t, ctx, admin, "reset cancel other")
	otherAdmin := createLocalMember(t, ctx, admin, other, "reset_cancel_other_admin")
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid`, otherAdmin); err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, target, other, household.RoleMember)
	// The owner keeps reset authority over the whole account (ADR 0017).
	addMembership(t, ctx, admin, owner, other, household.RoleAdmin)
	if _, err := service.IssuePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatal(err)
	}
	repository := NewPasswordResetRepository(app, passwordResetFactory, clock.now)
	// The adapter returns the canonical account ID whatever spelling the URL
	// used; the per-target budget is keyed by it.
	if err := repository.WithPasswordResetTransaction(ctx, func(tx identity.PasswordResetTransaction) error {
		canonical, e := tx.LockPasswordResetTarget(ctx, strings.ToUpper(target))
		if e == nil && canonical != target {
			t.Errorf("locked target ID=%q, want canonical %q", canonical, target)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	reissueReady, reissueGo, reissueDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var reissueErr error
	go func() {
		defer close(reissueDone)
		reissueErr = repository.WithPasswordResetTransaction(ctx, func(tx identity.PasswordResetTransaction) error {
			if e := tx.RequireAdmin(ctx, owner, house); e != nil {
				return e
			}
			if _, e := tx.LockPasswordResetTarget(ctx, target); e != nil {
				return e
			}
			if e := tx.RevokeOutstandingPasswordResets(ctx, target, clock.at); e != nil {
				return e
			}
			close(reissueReady)
			<-reissueGo
			_, e := tx.CreatePasswordReset(ctx, identity.CreatePasswordResetInput{
				HouseholdID: house, TargetUserID: target, ActorID: owner,
				TokenHash: sha256Sum("cancel-race-reissue"), CreatedAt: clock.at, ExpiresAt: clock.at.Add(time.Hour),
			})
			return e
		})
	}()
	<-reissueReady
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- service.RevokePasswordReset(ctx, otherAdmin, other, target) }()
	// Let the cancel reach its first blocking lock before the re-issue commits.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := app.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			close(reissueGo)
			<-reissueDone
			t.Fatalf("cancel never waited for the re-issue: %v", <-cancelDone)
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(reissueGo)
	<-reissueDone
	if reissueErr != nil {
		t.Fatalf("re-issue: %v", reissueErr)
	}
	if err := <-cancelDone; err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if outstanding := outstandingResetRows(t, ctx, admin, target); outstanding != 0 {
		t.Fatalf("cancel answered success but %d link(s) remain usable", outstanding)
	}
}
