package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txLoginLookup struct{ tx householddb.DBTX }

func (l txLoginLookup) LoginsByUserIDs(ctx context.Context, ids []string) (map[string]string, error) {
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		var u pgtype.UUID
		if e := u.Scan(id); e != nil {
			return nil, e
		}
		uuids = append(uuids, u)
	}
	rows, e := identitydb.New(l.tx).MemberLogins(ctx, uuids)
	if e != nil {
		return nil, e
	}
	out := map[string]string{}
	for _, r := range rows {
		out[r.UserID] = r.Login
	}
	return out, nil
}
func invitationFactory(tx householddb.DBTX) InvitationHouseholdService {
	return householdpostgres.MembershipServiceFor(tx, txLoginLookup{tx})
}
func invitationPools(t *testing.T) (context.Context, *pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	adminURL, appURL := os.Getenv("TEST_DATABASE_ADMIN_URL"), os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL required")
	}
	admin, e := pgxpool.New(ctx, adminURL)
	if e != nil {
		t.Fatal(e)
	}
	app, e := pgxpool.New(ctx, appURL)
	if e != nil {
		admin.Close()
		t.Fatal(e)
	}
	t.Cleanup(admin.Close)
	t.Cleanup(app.Close)
	if e = database.Migrate(ctx, admin); e != nil {
		t.Fatal(e)
	}
	return ctx, admin, app
}
func invitationFixture(t *testing.T, ctx context.Context, admin *pgxpool.Pool) (string, string) {
	t.Helper()
	if _, e := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); e != nil {
		t.Fatal(e)
	}
	var owner, house string
	if e := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("invite_owner_%d", time.Now().UnixNano())).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	if e := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES($1,'UTC') RETURNING id::text`, fmt.Sprintf("Invitation %d", time.Now().UnixNano())).Scan(&house); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, owner, house); e != nil {
		t.Fatal(e)
	}
	return owner, house
}
func TestInvitationPostgresDigestIdempotencyConcurrencyAndAdmission(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	first, e := svc.CreateInvitation(ctx, owner, house, "first-key")
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256([]byte(first.Token))
	var got []byte
	if e = admin.QueryRow(ctx, `SELECT token_hash FROM household_invitations WHERE id=$1::uuid`, first.Invitation.ID).Scan(&got); e != nil || string(got) != string(digest[:]) || len(got) != 32 {
		t.Fatalf("digest=%x err=%v", got, e)
	}
	retry, e := svc.CreateInvitation(ctx, owner, house, "first-key")
	if e != nil || retry.Created || retry.Token != "" || retry.Invitation.ID != first.Invitation.ID {
		t.Fatalf("retry=%+v err=%v", retry, e)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	errch := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, x := svc.CreateInvitation(ctx, owner, house, "parallel-key")
			if x != nil {
				errch <- x
			} else if r.Token != "" {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errch)
	for x := range errch {
		t.Error(x)
	}
	var rows int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations WHERE household_id=$1::uuid AND creation_key='parallel-key'`, house).Scan(&rows); e != nil || rows != 1 || wins.Load() != 1 {
		t.Fatalf("rows=%d token-winners=%d err=%v", rows, wins.Load(), e)
	}
	accepted, e := svc.AcceptInvitationNewAccount(ctx, first.Token, "invited_new", "a long sufficiently secure password")
	if e != nil {
		t.Fatal(e)
	}
	var role, defaultID, encoded string
	if e = admin.QueryRow(ctx, `SELECT m.role,u.default_household_id::text,c.password_hash FROM household_memberships m JOIN user_accounts u ON u.id=m.user_id JOIN local_credentials c ON c.user_id=u.id WHERE u.id=$1::uuid`, accepted.UserID).Scan(&role, &defaultID, &encoded); e != nil || role != "member" || defaultID != house {
		t.Fatalf("role=%s default=%s err=%v", role, defaultID, e)
	}
	valid, e := security.VerifyPassword(encoded, "a long sufficiently secure password")
	if e != nil || !valid {
		t.Fatalf("credential valid=%v err=%v", valid, e)
	}
}
func TestInvitationPostgresConcurrentAcceptAndRevoke(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	for n := 0; n < 20; n++ {
		inv, e := svc.CreateInvitation(ctx, owner, house, fmt.Sprintf("race-%d", n))
		if e != nil {
			t.Fatal(e)
		}
		start := make(chan struct{})
		var accepted atomic.Bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			<-start
			_, x := svc.AcceptInvitationNewAccount(ctx, inv.Token, fmt.Sprintf("race_user_%02d", i), "another long password")
			accepted.Store(x == nil)
		}(n)
		go func() {
			defer wg.Done()
			if e := svc.RevokeInvitation(ctx, owner, house, inv.Invitation.ID); e != nil {
				t.Error(e)
			}
		}()
		close(start)
		wg.Wait()
		var a, r int
		if e = admin.QueryRow(ctx, `SELECT (accepted_at IS NOT NULL)::int,(revoked_at IS NOT NULL)::int FROM household_invitations WHERE id=$1::uuid`, inv.Invitation.ID).Scan(&a, &r); e != nil || a+r != 1 || accepted.Load() != (a == 1) {
			t.Fatalf("iteration=%d accepted=%d revoked=%d success=%v err=%v", n, a, r, accepted.Load(), e)
		}
	}
}
func TestInvitationPostgresRuntimePrivilegesConstraintsAndCascade(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	inv, e := svc.CreateInvitation(ctx, owner, house, "privilege-key")
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`DELETE FROM household_invitations`, `TRUNCATE household_invitations`, `UPDATE household_invitations SET token_hash=token_hash`, `UPDATE household_invitations SET household_id=household_id`, `UPDATE household_invitations SET expires_at=expires_at`, `UPDATE household_invitations SET creation_key=creation_key`, `UPDATE household_invitations SET created_by_user_id=created_by_user_id`} {
		_, e = app.Exec(ctx, q)
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "42501" {
			t.Errorf("%s: %v", q, e)
		}
	}
	if _, e = app.Exec(ctx, `UPDATE household_invitations SET accepted_at=now() WHERE id=$1::uuid`, inv.Invitation.ID); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Second)
	checks := []string{`INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,'bad-digest',decode(repeat('00',31),'hex'),$3::timestamptz,$3::timestamptz+interval '1 day')`, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at,accepted_at,revoked_at) VALUES($1::uuid,$2::uuid,'both-terminal',decode(repeat('01',32),'hex'),$3::timestamptz,$3::timestamptz+interval '1 day',$3,$3)`, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,'bad-expiry',decode(repeat('02',32),'hex'),$3::timestamptz,$3::timestamptz)`, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,'has space',decode(repeat('03',32),'hex'),$3::timestamptz,$3::timestamptz+interval '1 day')`}
	for _, q := range checks {
		_, e = admin.Exec(ctx, q, house, owner, now)
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "23514" {
			t.Errorf("constraint expected 23514: %v", e)
		}
	}
	if _, e = admin.Exec(ctx, `DELETE FROM households WHERE id=$1::uuid`, house); e != nil {
		t.Fatal(e)
	}
	var remains int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations WHERE id=$1::uuid`, inv.Invitation.ID).Scan(&remains); e != nil || remains != 0 {
		t.Fatalf("cascade count=%d err=%v", remains, e)
	}
}
func TestInvitationPostgresAcceptanceRollbackLeavesInvitationRedeemable(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	svc.SetPasswordHasher(func(string) (string, error) { return "test-hash", nil })
	var target string
	if e := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('rollback_collision') RETURNING id::text`).Scan(&target); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, target, house); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, target, house); e != nil {
		t.Fatal(e)
	}
	inv, e := svc.CreateInvitation(ctx, owner, house, "rollback-accept")
	if e != nil {
		t.Fatal(e)
	}
	svc.SetPasswordHasher(func(string) (string, error) { return "test-hash", nil })
	// The deferred CHECK fails only at the final invitation update, after user, credential,
	// membership, and default-household writes have run in the transaction.
	if _, e = admin.Exec(ctx, `ALTER TABLE household_invitations ADD CONSTRAINT reject_rollback_token CHECK (accepted_at IS NULL OR creation_key <> 'rollback-accept')`); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.Exec(cleanupCtx, `ALTER TABLE household_invitations DROP CONSTRAINT IF EXISTS reject_rollback_token`)
	})
	_, e = svc.AcceptInvitationNewAccount(ctx, inv.Token, "rollback_joiner", "a sufficiently long rollback password")
	if e == nil {
		t.Fatal("expected transactional final-write failure")
	}
	var users, credentials, members int
	for q, dst := range map[string]*int{`SELECT count(*) FROM user_accounts WHERE login='rollback_joiner'`: &users, `SELECT count(*) FROM local_credentials WHERE user_id IN(SELECT id FROM user_accounts WHERE login='rollback_joiner')`: &credentials, `SELECT count(*) FROM household_memberships WHERE user_id IN(SELECT id FROM user_accounts WHERE login='rollback_joiner')`: &members} {
		if e = admin.QueryRow(ctx, q).Scan(dst); e != nil {
			t.Fatal(e)
		}
	}
	if users != 0 || credentials != 0 || members != 0 {
		t.Fatalf("partial state user=%d credential=%d membership=%d", users, credentials, members)
	}
	var defaultID string
	if e = admin.QueryRow(ctx, `SELECT default_household_id::text FROM user_accounts WHERE id=$1::uuid`, target).Scan(&defaultID); e != nil || defaultID != house {
		t.Fatalf("preexisting default=%q err=%v", defaultID, e)
	}
	if _, e = admin.Exec(ctx, `ALTER TABLE household_invitations DROP CONSTRAINT reject_rollback_token`); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.AcceptInvitationNewAccount(ctx, inv.Token, "rollback_joiner", "a sufficiently long rollback password"); e != nil {
		t.Fatalf("invitation not redeemable after rollback: %v", e)
	}
}

func TestInvitationPostgresLoginCollisionLeavesReusableInvitation(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	var id, original string
	if e := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('collision_login') RETURNING id::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if e := admin.QueryRow(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1::uuid,'original-hash') RETURNING password_hash`, id).Scan(&original); e != nil {
		t.Fatal(e)
	}
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	inv, e := svc.CreateInvitation(ctx, owner, house, "collision")
	if e != nil {
		t.Fatal(e)
	}
	_, e = svc.AcceptInvitationNewAccount(ctx, inv.Token, "collision_login", "long enough password")
	if !errors.Is(e, identity.ErrLoginUnavailable) {
		t.Fatalf("err=%v", e)
	}
	var pending bool
	var current string
	if e = admin.QueryRow(ctx, `SELECT accepted_at IS NULL FROM household_invitations WHERE id=$1::uuid`, inv.Invitation.ID).Scan(&pending); e != nil || !pending {
		t.Fatalf("pending=%v err=%v", pending, e)
	}
	if e = admin.QueryRow(ctx, `SELECT password_hash FROM local_credentials WHERE user_id=$1::uuid`, id).Scan(&current); e != nil || current != original {
		t.Fatalf("credential=%q err=%v", current, e)
	}
	if _, e = svc.AcceptInvitationNewAccount(ctx, inv.Token, "collision_joined", "long enough password"); e != nil {
		t.Fatal(e)
	}
}
func TestInvitationPostgresPaginationAndCrossHouseholdRevoke(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	svc.SetPasswordHasher(func(string) (string, error) { return "test-hash", nil })
	for i := 0; i < 5; i++ {
		if _, e := svc.CreateInvitation(ctx, owner, house, fmt.Sprintf("page-%d", i)); e != nil {
			t.Fatal(e)
		}
	}
	cursor := ""
	seen := map[string]bool{}
	var ids []string
	for p := 0; p < 3; p++ {
		rows, next, e := svc.ListInvitations(ctx, owner, house, 2, cursor)
		if e != nil {
			t.Fatal(e)
		}
		for _, row := range rows {
			if seen[row.ID] {
				t.Fatalf("duplicate %s", row.ID)
			}
			seen[row.ID] = true
			ids = append(ids, row.ID)
		}
		cursor = next
	}
	if len(seen) != 5 {
		t.Fatalf("rows=%d", len(seen))
	}
	if _, _, e := svc.ListInvitations(ctx, owner, house, 2, "bad"); e == nil {
		t.Fatal("invalid cursor accepted")
	}
	var other string
	if e := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('second household same owner','UTC') RETURNING id::text`).Scan(&other); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, owner, other); e != nil {
		t.Fatal(e)
	}
	if e := svc.RevokeInvitation(ctx, owner, other, ids[0]); !errors.Is(e, household.ErrNotFound) {
		t.Fatalf("cross-house revoke=%v", e)
	}
	var pending bool
	if e := admin.QueryRow(ctx, `SELECT revoked_at IS NULL FROM household_invitations WHERE id=$1::uuid`, ids[0]).Scan(&pending); e != nil || !pending {
		t.Fatalf("household A invitation pending=%v err=%v", pending, e)
	}
}
func TestInvitationPostgresPolicyAndDefaultHouseholdDoesNotAuthorize(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	inv, e := svc.CreateInvitation(ctx, owner, house, "policy")
	if e != nil {
		t.Fatal(e)
	}
	var member, nonmember string
	if e = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('policy_member') RETURNING id::text`).Scan(&member); e != nil {
		t.Fatal(e)
	}
	if e = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('policy_nonmember') RETURNING id::text`).Scan(&nonmember); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, member, house); e != nil {
		t.Fatal(e)
	}
	if _, e = admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2::uuid WHERE id=$1::uuid`, member, house); e != nil {
		t.Fatal(e)
	}
	before := 0
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.CreateInvitation(ctx, member, house, "denied"); !errors.Is(e, household.ErrForbidden) {
		t.Fatalf("member create=%v", e)
	}
	if _, _, e = svc.ListInvitations(ctx, member, house, 10, ""); !errors.Is(e, household.ErrForbidden) {
		t.Fatalf("member list=%v", e)
	}
	if e = svc.RevokeInvitation(ctx, member, house, inv.Invitation.ID); !errors.Is(e, household.ErrForbidden) {
		t.Fatalf("member revoke=%v", e)
	}
	if _, e = svc.CreateInvitation(ctx, nonmember, house, "nonmember"); !errors.Is(e, household.ErrNotFound) {
		t.Fatalf("nonmember create=%v", e)
	}
	if _, e = svc.CreateInvitation(ctx, owner, "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b6f", "missing-house"); !errors.Is(e, household.ErrNotFound) {
		t.Fatalf("missing house=%v", e)
	}
	var after int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations`).Scan(&after); e != nil || after != before {
		t.Fatalf("rows changed %d -> %d err=%v", before, after, e)
	}
}
func TestInvitationPostgresExpiryStatusesAndClockBoundaries(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	clock := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), func() time.Time { return clock })
	early, e := svc.CreateInvitation(ctx, owner, house, "expires-early")
	if e != nil {
		t.Fatal(e)
	}
	clock = early.Invitation.ExpiresAt.Add(-time.Second)
	if _, e = svc.AcceptInvitationNewAccount(ctx, early.Token, "expiry_success", "a long enough password"); e != nil {
		t.Fatal(e)
	}
	boundary, e := svc.CreateInvitation(ctx, owner, house, "expires-boundary")
	if e != nil {
		t.Fatal(e)
	}
	clock = boundary.Invitation.ExpiresAt
	if _, e = svc.AcceptInvitationNewAccount(ctx, boundary.Token, "expiry_failure", "a long enough password"); !errors.Is(e, identity.ErrInvalidInvitation) {
		t.Fatalf("boundary accept=%v", e)
	}
	var accepted bool
	if e = admin.QueryRow(ctx, `SELECT accepted_at IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, boundary.Invitation.ID).Scan(&accepted); e != nil || accepted {
		t.Fatalf("accepted=%v err=%v", accepted, e)
	}
	var users int
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts WHERE login='expiry_failure'`).Scan(&users); e != nil || users != 0 {
		t.Fatalf("users=%d err=%v", users, e)
	}
	revoked, e := svc.CreateInvitation(ctx, owner, house, "status-revoked")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.RevokeInvitation(ctx, owner, house, revoked.Invitation.ID); e != nil {
		t.Fatal(e)
	}
	clock = boundary.Invitation.ExpiresAt.Add(time.Second)
	rows, _, e := svc.ListInvitations(ctx, owner, house, 100, "")
	if e != nil {
		t.Fatal(e)
	}
	statuses := map[string]bool{}
	for _, r := range rows {
		statuses[r.Status] = true
	}
	for _, s := range []string{"accepted", "revoked", "expired"} {
		if !statuses[s] {
			t.Fatalf("missing status %s: %v", s, statuses)
		}
	}
	pending, e := svc.CreateInvitation(ctx, owner, house, "status-pending")
	if e != nil {
		t.Fatal(e)
	}
	if pending.Invitation.Status != "pending" {
		t.Fatalf("pending status=%s", pending.Invitation.Status)
	}
}
func TestInvitationPostgresRevokeAfterAcceptKeepsMembership(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory), time.Now)
	inv, e := svc.CreateInvitation(ctx, owner, house, "accept-revoke")
	if e != nil {
		t.Fatal(e)
	}
	user, e := svc.AcceptInvitationNewAccount(ctx, inv.Token, "accept_revoke_user", "a long enough password")
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.RevokeInvitation(ctx, owner, house, inv.Invitation.ID); e != nil {
		t.Fatal(e)
	}
	var accepted, revoked bool
	var members int
	if e = admin.QueryRow(ctx, `SELECT accepted_at IS NOT NULL,revoked_at IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, inv.Invitation.ID).Scan(&accepted, &revoked); e != nil || !accepted || revoked {
		t.Fatalf("accepted=%v revoked=%v err=%v", accepted, revoked, e)
	}
	if e = admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, user.UserID, house).Scan(&members); e != nil || members != 1 {
		t.Fatalf("members=%d err=%v", members, e)
	}
}
