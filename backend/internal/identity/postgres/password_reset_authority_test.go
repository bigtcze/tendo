package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A password reset replaces the target's global credential, so the reset
// authority is the whole account, not one household: the actor must
// administer every household the target belongs to, and only an instance
// administrator may reset another instance administrator (ADR 0017).

func addHousehold(t *testing.T, ctx context.Context, admin *pgxpool.Pool, name string) string {
	t.Helper()
	var house string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES($1,'UTC') RETURNING id::text`, name).Scan(&house); err != nil {
		t.Fatal(err)
	}
	return house
}

func addMembership(t *testing.T, ctx context.Context, admin *pgxpool.Pool, user, house, role string) {
	t.Helper()
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,$3)`, user, house, role); err != nil {
		t.Fatal(err)
	}
}

func grantInstanceAdmin(t *testing.T, ctx context.Context, admin *pgxpool.Pool, user string) {
	t.Helper()
	if _, err := admin.Exec(ctx, `INSERT INTO instance_admins(user_id,granted_at,grant_source) VALUES($1::uuid,now(),'cli')`, user); err != nil {
		t.Fatal(err)
	}
}

func resetRowCount(t *testing.T, ctx context.Context, admin *pgxpool.Pool, target string) int {
	t.Helper()
	var count int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE target_user_id=$1::uuid`, target).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertLinkUsable(t *testing.T, ctx context.Context, admin *pgxpool.Pool, token string) {
	t.Helper()
	var usable bool
	if err := admin.QueryRow(ctx, `SELECT used_at IS NULL AND revoked_at IS NULL FROM password_reset_tokens WHERE token_hash=$1`, sha256Sum(token)).Scan(&usable); err != nil || !usable {
		t.Fatalf("link usable=%v err=%v", usable, err)
	}
}

func assertCredential(t *testing.T, ctx context.Context, admin *pgxpool.Pool, user, wantHash string, wantVersion int64) {
	t.Helper()
	var hash string
	var version int64
	if err := admin.QueryRow(ctx, `SELECT password_hash, credential_version FROM local_credentials WHERE user_id=$1::uuid`, user).Scan(&hash, &version); err != nil {
		t.Fatal(err)
	}
	if hash != wantHash || version != wantVersion {
		t.Fatalf("credential hash=%q version=%d, want %q/%d", hash, version, wantHash, wantVersion)
	}
}

func TestPasswordResetPostgresIssueRefusesTargetInHouseholdActorDoesNotAdminister(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)

	// The target also belongs to a second household administered by someone
	// else. A co-admin of both households issues a valid link first.
	other := addHousehold(t, ctx, admin, "reset authority other")
	otherAdmin := createLocalMember(t, ctx, admin, other, "reset_other_admin")
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid`, otherAdmin); err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, target, other, household.RoleMember)
	coAdmin := createLocalMember(t, ctx, admin, house, "reset_co_admin")
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid`, coAdmin); err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, coAdmin, other, household.RoleAdmin)
	valid, err := service.IssuePasswordReset(ctx, coAdmin, house, target)
	if err != nil {
		t.Fatalf("co-admin of every target household: %v", err)
	}

	// The H1-only admin and the H2-only admin are each refused, store nothing
	// and leave the co-admin's link outstanding.
	for name, actor := range map[string][2]string{"first household admin": {owner, house}, "second household admin": {otherAdmin, other}} {
		if _, err = service.IssuePasswordReset(ctx, actor[0], actor[1], target); !errors.Is(err, identity.ErrPasswordResetNotPermitted) {
			t.Fatalf("%s issue=%v", name, err)
		}
		if rows := resetRowCount(t, ctx, admin, target); rows != 1 {
			t.Fatalf("%s refused issue stored rows=%d", name, rows)
		}
		assertLinkUsable(t, ctx, admin, valid.Token)
	}

	// Admin in one household and only a member in the other is not enough.
	addMembership(t, ctx, admin, owner, other, household.RoleMember)
	if _, err = service.IssuePasswordReset(ctx, owner, house, target); !errors.Is(err, identity.ErrPasswordResetNotPermitted) {
		t.Fatalf("admin of one household, member of the other: %v", err)
	}
	assertLinkUsable(t, ctx, admin, valid.Token)

	// Order: self and non-member answers are unchanged, and the authority
	// refusal comes before the local-password fact so it leaks nothing more.
	if _, err = service.IssuePasswordReset(ctx, owner, house, owner); !errors.Is(err, identity.ErrCannotResetSelf) {
		t.Fatalf("self issue=%v", err)
	}
	outsider := createLocalMember(t, ctx, admin, other, "reset_authority_outsider")
	if _, err = service.IssuePasswordReset(ctx, owner, house, outsider); !errors.Is(err, household.ErrNotFound) {
		t.Fatalf("non-member target=%v", err)
	}
	var oidcOnly string
	if err = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('reset_authority_oidc') RETURNING id::text`).Scan(&oidcOnly); err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, oidcOnly, house, household.RoleMember)
	addMembership(t, ctx, admin, oidcOnly, other, household.RoleMember)
	if _, err = service.IssuePasswordReset(ctx, otherAdmin, other, oidcOnly); !errors.Is(err, identity.ErrPasswordResetNotPermitted) {
		t.Fatalf("oidc-only dual member=%v", err)
	}

	// Once the actor administers every target household the issue succeeds and
	// supersedes the earlier link exactly as a normal re-issue does.
	if _, err = admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid AND household_id=$2::uuid`, owner, other); err != nil {
		t.Fatal(err)
	}
	if _, err = service.IssuePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatalf("admin of every target household: %v", err)
	}
	if outstandingResetRows(t, ctx, admin, target) != 1 {
		t.Fatal("permitted re-issue did not leave exactly one outstanding link")
	}
}

func TestPasswordResetPostgresIssueRefusesInstanceAdminTargetForHouseholdAdmin(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	grantInstanceAdmin(t, ctx, admin, target)

	if _, err := service.IssuePasswordReset(ctx, owner, house, target); !errors.Is(err, identity.ErrPasswordResetNotPermitted) {
		t.Fatalf("household admin resetting an instance admin: %v", err)
	}
	if rows := resetRowCount(t, ctx, admin, target); rows != 0 {
		t.Fatalf("refused issue stored rows=%d", rows)
	}
	grantInstanceAdmin(t, ctx, admin, owner)
	if _, err := service.IssuePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatalf("instance admin resetting an instance admin: %v", err)
	}
}

// Facts can change between issue and redeem. The redeem re-applies the same
// authority rule against the issuer and voids the link without consuming it.
func TestPasswordResetPostgresRedeemRechecksAuthority(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	other := addHousehold(t, ctx, admin, "reset redeem other")

	issued, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, target, other, household.RoleMember)
	if _, err = service.RedeemPasswordReset(ctx, issued.Token, "a brand new long password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("redeem after the target joined another household: %v", err)
	}
	assertLinkUsable(t, ctx, admin, issued.Token)
	assertCredential(t, ctx, admin, target, "hash:old-password-value", 1)

	// The link becomes usable again when the issuer administers that household.
	addMembership(t, ctx, admin, owner, other, household.RoleAdmin)
	if _, err = service.RedeemPasswordReset(ctx, issued.Token, "a brand new long password"); err != nil {
		t.Fatalf("redeem once the issuer administers every target household: %v", err)
	}
	assertCredential(t, ctx, admin, target, "hash:a brand new long password", 2)

	issued, err = service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	grantInstanceAdmin(t, ctx, admin, target)
	if _, err = service.RedeemPasswordReset(ctx, issued.Token, "another brand new password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("redeem after the target became an instance admin: %v", err)
	}
	assertLinkUsable(t, ctx, admin, issued.Token)
	assertCredential(t, ctx, admin, target, "hash:a brand new long password", 2)
}

// Admins of different households racing to reset the same dual member, while a
// co-admin of both issues and redeems, must never deadlock and must never let
// a single-household admin through.
func TestPasswordResetPostgresConcurrentCrossHouseholdIssues(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	other := addHousehold(t, ctx, admin, "reset concurrent other")
	otherAdmin := createLocalMember(t, ctx, admin, other, "reset_concurrent_other_admin")
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid`, otherAdmin); err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, target, other, household.RoleMember)
	coAdmin := createLocalMember(t, ctx, admin, house, "reset_concurrent_co_admin")
	if _, err := admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid`, coAdmin); err != nil {
		t.Fatal(err)
	}
	addMembership(t, ctx, admin, coAdmin, other, household.RoleAdmin)

	for round := 0; round < 5; round++ {
		previous, err := service.IssuePasswordReset(ctx, coAdmin, other, target)
		if err != nil {
			t.Fatalf("round %d co-admin issue: %v", round, err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 4)
		run := func(i int, work func() error) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = work()
			}()
		}
		run(0, func() error { _, e := service.IssuePasswordReset(ctx, owner, house, target); return e })
		run(1, func() error { _, e := service.IssuePasswordReset(ctx, otherAdmin, other, target); return e })
		run(2, func() error { _, e := service.IssuePasswordReset(ctx, coAdmin, house, target); return e })
		run(3, func() error {
			_, e := service.RedeemPasswordReset(ctx, previous.Token, fmt.Sprintf("a brand new long password %d", round))
			return e
		})
		close(start)
		wg.Wait()
		for i, e := range errs {
			var pgErr *pgconn.PgError
			if errors.As(e, &pgErr) {
				t.Fatalf("round %d worker %d database error %s: %v", round, i, pgErr.Code, e)
			}
		}
		if !errors.Is(errs[0], identity.ErrPasswordResetNotPermitted) || !errors.Is(errs[1], identity.ErrPasswordResetNotPermitted) {
			t.Fatalf("round %d single-household admins: %v / %v", round, errs[0], errs[1])
		}
		if errs[2] != nil {
			t.Fatalf("round %d co-admin issue: %v", round, errs[2])
		}
		if errs[3] != nil && !errors.Is(errs[3], identity.ErrInvalidPasswordReset) {
			t.Fatalf("round %d redeem: %v", round, errs[3])
		}
		if outstandingResetRows(t, ctx, admin, target) != 1 {
			t.Fatalf("round %d did not leave exactly one outstanding link", round)
		}
		var foreign int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE target_user_id=$1::uuid AND created_by_user_id IN ($2::uuid,$3::uuid)`, target, owner, otherAdmin).Scan(&foreign); err != nil || foreign != 0 {
			t.Fatalf("round %d single-household admin rows=%d err=%v", round, foreign, err)
		}
	}
}
