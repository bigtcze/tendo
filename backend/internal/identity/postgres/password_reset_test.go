package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdpostgres "github.com/bigtcze/tendo/backend/internal/household/postgres"
	householddb "github.com/bigtcze/tendo/backend/internal/household/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func passwordResetFactory(tx householddb.DBTX) PasswordResetHouseholdService {
	return householdpostgres.MembershipServiceFor(tx)
}

type passwordResetTestClock struct{ at time.Time }

func (c *passwordResetTestClock) now() time.Time { return c.at }

func newPasswordResetService(t *testing.T, app *pgxpool.Pool, clock *passwordResetTestClock) *identity.PasswordResetService {
	t.Helper()
	repository := NewPasswordResetRepository(app, passwordResetFactory, clock.now)
	service, err := identity.NewPasswordResetService(repository, clock.now)
	if err != nil {
		t.Fatal(err)
	}
	service.SetPasswordHasher(func(password string) (string, error) { return "hash:" + password, nil })
	return service
}

// passwordResetFixture returns an admin owner, a local-password member target
// and the household both belong to.
func passwordResetFixture(t *testing.T, ctx context.Context, admin *pgxpool.Pool) (string, string, string) {
	t.Helper()
	owner, house := invitationFixture(t, ctx, admin)
	var target string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('reset_target') RETURNING id::text`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, target, house); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1::uuid,'hash:old-password-value')`, target); err != nil {
		t.Fatal(err)
	}
	return owner, target, house
}

func createLocalMember(t *testing.T, ctx context.Context, admin *pgxpool.Pool, house, login string) string {
	t.Helper()
	var id string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, login).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, id, house); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1::uuid,'hash:old-password-value')`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func outstandingResetRows(t *testing.T, ctx context.Context, admin *pgxpool.Pool, target string) int {
	t.Helper()
	var count int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE target_user_id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, target).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestPasswordResetPostgresIssueAuthorizationAndStorage(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)

	issued, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	if len(issued.Token) != 43 || !issued.ExpiresAt.Equal(clock.at.Add(24*time.Hour)) || !issued.CreatedAt.Equal(clock.at) {
		t.Fatalf("issued=%+v", issued)
	}
	digest := sha256.Sum256([]byte(issued.Token))
	var stored []byte
	var storedToken []byte
	if err = admin.QueryRow(ctx, `SELECT token_hash FROM password_reset_tokens WHERE target_user_id=$1::uuid`, target).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(digest[:]) {
		t.Fatalf("stored digest=%x", stored)
	}
	if err = admin.QueryRow(ctx, `SELECT token_hash FROM password_reset_tokens WHERE token_hash=$1`, []byte(issued.Token)).Scan(&storedToken); err == nil {
		t.Fatal("raw token was stored")
	}
	if outstandingResetRows(t, ctx, admin, target) != 1 {
		t.Fatal("expected exactly one outstanding reset")
	}

	member := createLocalMember(t, ctx, admin, house, "reset_member_actor")
	if _, err = service.IssuePasswordReset(ctx, member, house, target); !errors.Is(err, household.ErrAdminRequired) {
		t.Fatalf("member issue=%v", err)
	}
	if _, err = service.IssuePasswordReset(ctx, owner, house, owner); !errors.Is(err, identity.ErrCannotResetSelf) {
		t.Fatalf("self issue=%v", err)
	}

	foreignOwner := createLocalMember(t, ctx, admin, house, "reset_foreign_admin")
	var foreignHouse string
	if err = admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('reset foreign','UTC') RETURNING id::text`).Scan(&foreignHouse); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'admin')`, foreignOwner, foreignHouse); err != nil {
		t.Fatal(err)
	}
	if _, err = service.IssuePasswordReset(ctx, foreignOwner, foreignHouse, target); !errors.Is(err, household.ErrNotFound) {
		t.Fatalf("cross-household target issue=%v", err)
	}
	outsider := createLocalMember(t, ctx, admin, house, "reset_outsider")
	if _, err = admin.Exec(ctx, `DELETE FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, outsider, house); err != nil {
		t.Fatal(err)
	}
	if _, err = service.IssuePasswordReset(ctx, outsider, house, target); !errors.Is(err, household.ErrNotFound) {
		t.Fatalf("non-member actor issue=%v", err)
	}

	var oidcOnly string
	if err = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('reset_oidc_only') RETURNING id::text`).Scan(&oidcOnly); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, oidcOnly, house); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES('https://idp.example','oidc-only-subject',$1::uuid,now())`, oidcOnly); err != nil {
		t.Fatal(err)
	}
	if _, err = service.IssuePasswordReset(ctx, owner, house, oidcOnly); !errors.Is(err, identity.ErrNoLocalPassword) {
		t.Fatalf("oidc-only issue=%v", err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1::uuid,'hash:old-password-value')`, oidcOnly); err != nil {
		t.Fatal(err)
	}
	if _, err = service.IssuePasswordReset(ctx, owner, house, oidcOnly); err != nil {
		t.Fatalf("dual local+oidc issue=%v", err)
	}
}

func TestPasswordResetPostgresReissueRevokesPreviousAndCancelIsIdempotent(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)

	first, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token {
		t.Fatal("re-issue reused the token")
	}
	if outstandingResetRows(t, ctx, admin, target) != 1 {
		t.Fatal("re-issue did not leave exactly one outstanding reset")
	}
	if _, err = service.RedeemPasswordReset(ctx, first.Token, "a brand new long password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("superseded redeem=%v", err)
	}
	if err = service.RevokePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatal(err)
	}
	if outstandingResetRows(t, ctx, admin, target) != 0 {
		t.Fatal("cancel left an outstanding reset")
	}
	var revokedAt time.Time
	if err = admin.QueryRow(ctx, `SELECT revoked_at FROM password_reset_tokens WHERE token_hash=$1`, sha256Sum(second.Token)).Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(time.Minute)
	if err = service.RevokePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatalf("idempotent cancel=%v", err)
	}
	var secondRevoked time.Time
	if err = admin.QueryRow(ctx, `SELECT revoked_at FROM password_reset_tokens WHERE token_hash=$1`, sha256Sum(second.Token)).Scan(&secondRevoked); err != nil {
		t.Fatal(err)
	}
	if !secondRevoked.Equal(revokedAt) {
		t.Fatalf("cancel rewrote revoked_at %s => %s", revokedAt, secondRevoked)
	}
	member := createLocalMember(t, ctx, admin, house, "reset_cancel_member")
	if err = service.RevokePasswordReset(ctx, member, house, target); !errors.Is(err, household.ErrAdminRequired) {
		t.Fatalf("member cancel=%v", err)
	}
	if err = service.RevokePasswordReset(ctx, owner, house, owner); !errors.Is(err, identity.ErrCannotResetSelf) {
		t.Fatalf("self cancel=%v", err)
	}
}

func sha256Sum(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func TestPasswordResetPostgresRedeemSuccessAndFailures(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	sessions, err := identity.NewSessionService(New(app, nil), clock.now)
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetPasswordVerifier(func(hash, password string) (bool, error) { return hash == "hash:"+password, nil })

	other := createLocalMember(t, ctx, admin, house, "reset_other")
	if _, err = admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES
		($1::uuid,decode(repeat('a1',32),'hex'),now(),now()+interval '1 day'),
		($1::uuid,decode(repeat('a2',32),'hex'),now(),now()+interval '1 day'),
		($2::uuid,decode(repeat('a3',32),'hex'),now(),now()+interval '1 day')`, target, other); err != nil {
		t.Fatal(err)
	}
	issued, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}

	// A weak password is rejected but does not consume the link.
	if _, err = service.RedeemPasswordReset(ctx, issued.Token, "short"); err == nil {
		t.Fatal("weak password accepted")
	}
	var validation *identity.ValidationError
	if !errors.As(err, &validation) || validation.Field != "password" || validation.Code != "invalid_length" {
		t.Fatalf("weak password error=%v", err)
	}
	login, err := service.RedeemPasswordReset(ctx, issued.Token, "a brand new long password")
	if err != nil {
		t.Fatal(err)
	}
	if login != "reset_target" {
		t.Fatalf("login=%q", login)
	}
	if _, err = sessions.Login(ctx, "reset_target", "old-password-value"); !errors.Is(err, identity.ErrInvalidCredentials) {
		t.Fatalf("old password login=%v", err)
	}
	if _, err = sessions.Login(ctx, "reset_target", "a brand new long password"); err != nil {
		t.Fatalf("new password login=%v", err)
	}
	var used bool
	var sessionsLeft int
	if err = admin.QueryRow(ctx, `SELECT used_at IS NOT NULL FROM password_reset_tokens WHERE token_hash=$1`, sha256Sum(issued.Token)).Scan(&used); err != nil || !used {
		t.Fatalf("used=%v err=%v", used, err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid`, target).Scan(&sessionsLeft); err != nil || sessionsLeft != 1 {
		t.Fatalf("target sessions after redemption=%d err=%v", sessionsLeft, err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid`, other).Scan(&sessionsLeft); err != nil || sessionsLeft != 1 {
		t.Fatalf("other sessions after redemption=%d err=%v", sessionsLeft, err)
	}
	if _, err = service.RedeemPasswordReset(ctx, issued.Token, "another brand new password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("replay=%v", err)
	}

	// Expired link.
	expiring, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	clock.at = expiring.ExpiresAt
	if _, err = service.RedeemPasswordReset(ctx, expiring.Token, "another brand new password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("expired=%v", err)
	}
	clock.at = expiring.ExpiresAt.Add(-time.Second)

	// Revoked link.
	revoked, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.RevokePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RedeemPasswordReset(ctx, revoked.Token, "another brand new password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("revoked=%v", err)
	}

	// Issuer demoted before redemption.
	pending, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE household_memberships SET role='member' WHERE user_id=$1::uuid AND household_id=$2::uuid`, owner, house); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RedeemPasswordReset(ctx, pending.Token, "another brand new password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("demoted issuer=%v", err)
	}
	if _, err = admin.Exec(ctx, `UPDATE household_memberships SET role='admin' WHERE user_id=$1::uuid AND household_id=$2::uuid`, owner, house); err != nil {
		t.Fatal(err)
	}

	// Target removed from the household before redemption.
	pending, err = service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `DELETE FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, target, house); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RedeemPasswordReset(ctx, pending.Token, "another brand new password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("removed target=%v", err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, target, house); err != nil {
		t.Fatal(err)
	}

	// Target local credential removed before redemption.
	pending, err = service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `DELETE FROM local_credentials WHERE user_id=$1::uuid`, target); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RedeemPasswordReset(ctx, pending.Token, "another brand new password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("removed credential=%v", err)
	}
}

func TestPasswordResetPostgresConcurrentIssuesAndRedemptions(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	var issuedTokens sync.Map
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			issued, err := service.IssuePasswordReset(ctx, owner, house, target)
			if err != nil {
				errs <- err
				return
			}
			issuedTokens.Store(issued.Token, true)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent issue: %v", err)
	}
	if outstandingResetRows(t, ctx, admin, target) != 1 {
		t.Fatal("concurrent issues left more than one outstanding reset")
	}

	// Exactly one of the issued tokens is the current outstanding one; a
	// concurrent redemption batch therefore has exactly one winner.
	var outstandingDigest []byte
	if err := admin.QueryRow(ctx, `SELECT token_hash FROM password_reset_tokens WHERE target_user_id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, target).Scan(&outstandingDigest); err != nil {
		t.Fatal(err)
	}
	var token string
	tokens := 0
	issuedTokens.Range(func(key, _ any) bool {
		value := key.(string)
		if string(sha256Sum(value)) == string(outstandingDigest) {
			token = value
		}
		tokens++
		return true
	})
	if tokens != 10 || token == "" {
		t.Fatalf("issued tokens=%d outstanding matched=%v", tokens, token != "")
	}
	var successes atomic.Int32
	start = make(chan struct{})
	errs = make(chan error, 10)
	for n := 0; n < 10; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.RedeemPasswordReset(ctx, token, "a brand new long password")
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, identity.ErrInvalidPasswordReset) {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent redeem: %v", err)
	}
	if successes.Load() != 1 {
		t.Fatalf("successful redemptions=%d", successes.Load())
	}
}

func TestPasswordResetPostgresAccountCenterLookup(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	if _, err := service.IssuePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatal(err)
	}
	accounts := householdpostgres.AccountServiceFor(app, New(app, nil), New(app, nil), NewPasswordResetRepository(app, passwordResetFactory, clock.now), clock.now)
	account, err := accounts.GetAccount(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	if account.PasswordReset == nil || !account.PasswordReset.CreatedAt.Equal(clock.at) || !account.PasswordReset.ExpiresAt.Equal(clock.at.Add(24*time.Hour)) {
		t.Fatalf("passwordReset=%+v", account.PasswordReset)
	}
	if err = service.RevokePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatal(err)
	}
	account, err = accounts.GetAccount(ctx, owner, house, target)
	if err != nil || account.PasswordReset != nil {
		t.Fatalf("after cancel passwordReset=%+v err=%v", account.PasswordReset, err)
	}
	if _, err = service.IssuePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE password_reset_tokens SET created_at=$2::timestamptz - interval '2 minutes', expires_at=$2::timestamptz - interval '1 minute' WHERE target_user_id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, target, clock.at); err != nil {
		t.Fatal(err)
	}
	account, err = accounts.GetAccount(ctx, owner, house, target)
	if err != nil || account.PasswordReset != nil {
		t.Fatalf("expired passwordReset=%+v err=%v", account.PasswordReset, err)
	}
}

func TestPasswordResetPostgresRuntimePrivilegesConstraintsAndCascade(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	var err error
	if _, err = service.IssuePasswordReset(ctx, owner, house, target); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`DELETE FROM password_reset_tokens`,
		`TRUNCATE password_reset_tokens`,
		`UPDATE password_reset_tokens SET token_hash=token_hash`,
		`UPDATE password_reset_tokens SET household_id=household_id`,
		`UPDATE password_reset_tokens SET target_user_id=target_user_id`,
		`UPDATE password_reset_tokens SET created_by_user_id=created_by_user_id`,
		`UPDATE password_reset_tokens SET created_at=created_at`,
		`UPDATE password_reset_tokens SET expires_at=expires_at`,
	} {
		_, err = app.Exec(ctx, query)
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
			t.Errorf("%s: %v", query, err)
		}
	}
	if _, err = app.Exec(ctx, `UPDATE password_reset_tokens SET used_at=now() WHERE target_user_id=$1::uuid`, target); err != nil {
		t.Fatal(err)
	}

	now := clock.at
	checks := []string{
		`INSERT INTO password_reset_tokens(household_id,target_user_id,created_by_user_id,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,decode(repeat('00',31),'hex'),$4::timestamptz,$4::timestamptz+interval '1 hour')`,
		`INSERT INTO password_reset_tokens(household_id,target_user_id,created_by_user_id,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,decode(repeat('11',32),'hex'),$4::timestamptz,$4::timestamptz+interval '25 hours')`,
		`INSERT INTO password_reset_tokens(household_id,target_user_id,created_by_user_id,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,decode(repeat('22',32),'hex'),$4::timestamptz,$4::timestamptz)`,
		`INSERT INTO password_reset_tokens(household_id,target_user_id,created_by_user_id,token_hash,created_at,expires_at,used_at,revoked_at) VALUES($1::uuid,$2::uuid,$3::uuid,decode(repeat('33',32),'hex'),$4::timestamptz,$4::timestamptz+interval '1 hour',$4,$4)`,
	}
	for _, query := range checks {
		_, err = admin.Exec(ctx, query, house, target, owner, now)
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "23514" && postgresError.Code != "23503" {
			t.Errorf("constraint expected: %v", err)
		}
	}
	_, err = admin.Exec(ctx, `INSERT INTO password_reset_tokens(household_id,target_user_id,created_by_user_id,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,$2::uuid,decode(repeat('44',32),'hex'),$3::timestamptz,$3::timestamptz+interval '1 hour')`, house, target, now)
	var selfError *pgconn.PgError
	if !errors.As(err, &selfError) || selfError.Code != "23514" {
		t.Errorf("self issuer constraint expected: %v", err)
	}
	// A second outstanding row for the same target is rejected by the partial
	// unique index even though the token digest differs.
	other := createLocalMember(t, ctx, admin, house, "reset_unique_other")
	if _, err = admin.Exec(ctx, `INSERT INTO password_reset_tokens(household_id,target_user_id,created_by_user_id,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6)`,
		house, other, owner, sha256Sum("first-outstanding"), now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, `INSERT INTO password_reset_tokens(household_id,target_user_id,created_by_user_id,token_hash,created_at,expires_at) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6)`,
		house, other, owner, sha256Sum("second-outstanding"), now, now.Add(time.Hour))
	var uniqueError *pgconn.PgError
	if !errors.As(err, &uniqueError) || uniqueError.Code != "23505" {
		t.Fatalf("duplicate outstanding row error=%v", err)
	}
	if _, err = admin.Exec(ctx, `DELETE FROM households WHERE id=$1::uuid`, house); err != nil {
		t.Fatal(err)
	}
	var remains int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE household_id=$1::uuid`, house).Scan(&remains); err != nil || remains != 0 {
		t.Fatalf("cascade rows=%d err=%v", remains, err)
	}
}
