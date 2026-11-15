package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// tombstoneOptions keep a row the real deletion also removes, so a test can
// prove that the read path refuses a deleted account on its own rather than
// only because the dependent row is already gone.
type tombstoneOptions struct {
	keepCredentials    bool
	keepSessions       bool
	keepOIDCIdentities bool
	keepInstanceAdmin  bool
}

// tombstoneAccount anonymizes an account in place exactly as migration 027 /
// the deletion service does: the login becomes the '~deleted-<32 hex>' tombstone,
// deleted_at is set, email facts and the default household are cleared, and the
// credentials, sessions, OIDC links and grants are deleted. It returns the
// stored tombstone login.
func tombstoneAccount(t *testing.T, ctx context.Context, admin *pgxpool.Pool, userID string, opts tombstoneOptions) string {
	t.Helper()
	var login string
	if err := admin.QueryRow(ctx, `UPDATE user_accounts
		SET login = '~deleted-' || replace(id::text, '-', ''),
		    deleted_at = now(),
		    email = NULL, email_verified_at = NULL, email_source = NULL, email_issuer = NULL,
		    default_household_id = NULL
		WHERE id = $1::uuid
		RETURNING login`, userID).Scan(&login); err != nil {
		t.Fatalf("tombstone account %s: %v", userID, err)
	}
	if !opts.keepCredentials {
		if _, err := admin.Exec(ctx, `DELETE FROM local_credentials WHERE user_id=$1::uuid`, userID); err != nil {
			t.Fatalf("tombstone credentials: %v", err)
		}
	}
	if !opts.keepSessions {
		if _, err := admin.Exec(ctx, `DELETE FROM user_sessions WHERE user_id=$1::uuid`, userID); err != nil {
			t.Fatalf("tombstone sessions: %v", err)
		}
	}
	if !opts.keepOIDCIdentities {
		if _, err := admin.Exec(ctx, `DELETE FROM oidc_identities WHERE user_id=$1::uuid`, userID); err != nil {
			t.Fatalf("tombstone identities: %v", err)
		}
	}
	if !opts.keepInstanceAdmin {
		if _, err := admin.Exec(ctx, `DELETE FROM instance_admins WHERE user_id=$1::uuid`, userID); err != nil {
			t.Fatalf("tombstone grant: %v", err)
		}
	}
	return login
}

func newLiveAccount(t *testing.T, ctx context.Context, admin *pgxpool.Pool, login string) string {
	t.Helper()
	var id string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, login).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestDeletedAccountInvitationAndResetRefused covers the local identity paths
// that must never resolve a tombstoned account: accepting an invitation as an
// existing account, looking a login up for a new-account invitation, and
// issuing a password reset.
func TestDeletedAccountInvitationAndResetRefused(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, err := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("accepting an invitation as an existing deleted account is not found", func(t *testing.T) {
		target := newLiveAccount(t, ctx, admin, "deleted_invitee")
		inv, err := svc.CreateInvitation(ctx, owner, house, "deleted-accept")
		if err != nil {
			t.Fatal(err)
		}
		tombstoneAccount(t, ctx, admin, target, tombstoneOptions{})
		if _, err := svc.AcceptInvitationExistingAccount(ctx, target, inv.Token); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("accept deleted account err=%v, want ErrNotFound", err)
		}
		var memberships, pending int
		if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid),
			(SELECT count(*) FROM household_invitations WHERE id=$2::uuid AND accepted_at IS NULL)`, target, inv.Invitation.ID).Scan(&memberships, &pending); err != nil || memberships != 0 || pending != 1 {
			t.Fatalf("memberships=%d pending invitations=%d err=%v", memberships, pending, err)
		}
	})

	t.Run("the tombstone login is not a new-account invitation target", func(t *testing.T) {
		target := newLiveAccount(t, ctx, admin, "deleted_new_invitee")
		tombstoneLogin := tombstoneAccount(t, ctx, admin, target, tombstoneOptions{})
		repository := NewInvitationRepository(app, invitationFactory, memberPeopleFactory)
		err := repository.WithInvitationTransaction(ctx, func(tx identity.InvitationTransaction) error {
			return tx.FindLogin(ctx, tombstoneLogin)
		})
		if !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("FindLoginForInvitation(%q) err=%v, want ErrNotFound", tombstoneLogin, err)
		}
	})

	t.Run("a password reset is never issued against a deleted account", func(t *testing.T) {
		resetOwner, target, resetHouse := passwordResetFixture(t, ctx, admin)
		clock := &passwordResetTestClock{at: time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)}
		resets := newPasswordResetService(t, app, clock)
		tombstoneAccount(t, ctx, admin, target, tombstoneOptions{})
		if _, err := resets.IssuePasswordReset(ctx, resetOwner, resetHouse, target); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("issue reset for deleted account err=%v, want ErrNotFound", err)
		}
		var tokens int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE target_user_id=$1::uuid`, target).Scan(&tokens); err != nil || tokens != 0 {
			t.Fatalf("reset tokens=%d err=%v", tokens, err)
		}
	})
}

// TestDeletedAccountSignInAndDirectoryRefused covers the sign-in reads and the
// account directory: a surviving session and a surviving credential must not
// authenticate the tombstone, and the directory never lists it.
func TestDeletedAccountSignInAndDirectoryRefused(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := New(app, ownerFactory, memberPeopleFactory)
	now := time.Now().UTC().Truncate(time.Second)
	target, _ := jitLocalAccount(t, ctx, admin, "deleted_signin")
	_, session := jitSession(200, now)
	if _, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at,sign_in_method)
		VALUES($1::uuid,$2,$3,$4,'password')`, target, session.TokenHash, now, session.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	tombstoneLogin := tombstoneAccount(t, ctx, admin, target, tombstoneOptions{keepCredentials: true, keepSessions: true})
	if _, err := repo.FindLogin(ctx, tombstoneLogin); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("FindLogin(deleted) err=%v, want ErrNotFound", err)
	}
	if _, err := repo.FindActive(ctx, session.TokenHash, now); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("FindActive(session of deleted account) err=%v, want ErrNotFound", err)
	}

	directory := NewAccountDirectory(app)
	reuse := newLiveAccount(t, ctx, admin, "reuse_login")
	if _, err := admin.Exec(ctx, `UPDATE user_accounts SET email='reuse@example.test', email_verified_at=now(), email_source='oidc_claim', email_issuer=$2 WHERE id=$1::uuid`, reuse, jitIssuer); err != nil {
		t.Fatal(err)
	}
	tombstoneAccount(t, ctx, admin, reuse, tombstoneOptions{})
	page := household.DirectoryPage{Limit: 50}
	accounts, err := directory.ListDirectoryAccounts(ctx, page)
	if err != nil {
		t.Fatal(err)
	}
	for _, account := range accounts {
		if account.UserID == target || account.UserID == reuse {
			t.Fatalf("directory listed deleted account %+v", account)
		}
	}
	// The login is freed for a new live account, which is then the only entry.
	replacement := newLiveAccount(t, ctx, admin, "reuse_login")
	accounts, err = directory.ListDirectoryAccounts(ctx, household.DirectoryPage{Search: "reuse_login", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].UserID != replacement || accounts[0].Login != "reuse_login" {
		t.Fatalf("directory after reuse=%+v", accounts)
	}
	searched, err := directory.ListDirectoryAccounts(ctx, household.DirectoryPage{Search: "~deleted-", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(searched) != 0 {
		t.Fatalf("directory matched tombstone logins: %+v", searched)
	}
}

// TestDeletedAccountStaleOIDCLinkDropped proves a link that survived deletion
// (a racing insert) is dropped and the sign-in continues as an unlinked
// identity instead of reviving the deleted account.
func TestDeletedAccountStaleOIDCLinkDropped(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	target := newLiveAccount(t, ctx, admin, "deleted_oidc_link")
	if _, err := admin.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES($1,'subject-stale',$2::uuid,$3)`,
		jitIssuer, target, now); err != nil {
		t.Fatal(err)
	}
	tombstoneAccount(t, ctx, admin, target, tombstoneOptions{keepOIDCIdentities: true})
	_, handleHash := jitHandle(201)
	t.Cleanup(func() {
		// oidc_pending_logins has no account foreign key, so it is not reached by
		// the TRUNCATE fixtures of the other OIDC tests.
		_, _ = admin.Exec(context.Background(), `DELETE FROM oidc_pending_logins WHERE handle_hash=$1`, handleHash)
	})
	outcome := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-stale"}, handleHash, now, nil)
	if outcome.Resolution != identity.OIDCLoginNeedsPending {
		t.Fatalf("stale link outcome=%+v, want NeedsPending", outcome)
	}
	var identities, sessions int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM oidc_identities WHERE subject='subject-stale'),
		(SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid)`, target).Scan(&identities, &sessions); err != nil || identities != 0 || sessions != 0 {
		t.Fatalf("identities=%d sessions=%d err=%v", identities, sessions, err)
	}
}

// waitForBlockedBackend waits until one backend is blocked on a lock. Tests run
// serially (-p 1), so any waiter other than this connection belongs to the
// path under test.
func waitForBlockedBackend(t *testing.T, ctx context.Context, observer *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := observer.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the path under test never blocked on the account row lock")
}

// TestDeletedAccountInvitationAcceptRaceRefused is the finding 1 race: the
// acceptance reads and locks the account row while a concurrent deletion holds
// an uncommitted tombstone lock. The acceptance must lose and report not found.
func TestDeletedAccountInvitationAcceptRaceRefused(t *testing.T) {
	ctx, admin, app, observer := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, err := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	target := newLiveAccount(t, ctx, admin, "race_deleted_invitee")
	inv, err := svc.CreateInvitation(ctx, owner, house, "deleted-accept-race")
	if err != nil {
		t.Fatal(err)
	}
	deleter, err := app.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deleter.Rollback(context.Background()) }()
	if _, err = deleter.Exec(ctx, `UPDATE user_accounts
		SET login = '~deleted-' || replace(id::text, '-', ''), deleted_at = now(), default_household_id = NULL
		WHERE id = $1::uuid`, target); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, acceptErr := svc.AcceptInvitationExistingAccount(ctx, target, inv.Token)
		result <- acceptErr
	}()
	waitForBlockedBackend(t, ctx, observer)
	if err := deleter.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("accept racing a deletion err=%v, want ErrNotFound", err)
	}
	var memberships, accepted int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid),
		(SELECT count(*) FROM household_invitations WHERE id=$2::uuid AND accepted_at IS NOT NULL)`, target, inv.Invitation.ID).Scan(&memberships, &accepted); err != nil || memberships != 0 || accepted != 0 {
		t.Fatalf("memberships=%d accepted invitations=%d err=%v", memberships, accepted, err)
	}
}

// TestDeletedAccountOIDCAttachRaceRefused is the finding 2 race: the verified
// email owner is read before any account lock. A concurrent deletion of that
// owner must not let the sign-in attach the identity to the tombstone.
func TestDeletedAccountOIDCAttachRaceRefused(t *testing.T) {
	ctx, admin, app, observer := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	owner := newLiveAccount(t, ctx, admin, "race_email_owner")
	if _, err := admin.Exec(ctx, `UPDATE user_accounts SET email='race@example.test', email_verified_at=$2, email_source='oidc_claim', email_issuer=$3 WHERE id=$1::uuid`,
		owner, now, jitIssuer); err != nil {
		t.Fatal(err)
	}
	deleter, err := app.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deleter.Rollback(context.Background()) }()
	if _, err = deleter.Exec(ctx, `UPDATE user_accounts
		SET login = '~deleted-' || replace(id::text, '-', ''), deleted_at = now(),
		    email = NULL, email_verified_at = NULL, email_source = NULL, email_issuer = NULL, default_household_id = NULL
		WHERE id = $1::uuid`, owner); err != nil {
		t.Fatal(err)
	}
	_, handleHash := jitHandle(202)
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DELETE FROM oidc_pending_logins WHERE handle_hash=$1`, handleHash)
	})
	_, session := jitSession(203, now)
	type resolution struct {
		outcome identity.OIDCLoginOutcome
		err     error
	}
	resolved := make(chan resolution, 1)
	go func() {
		outcome, resolveErr := repo.ResolveLogin(ctx, identity.OIDCLoginRequest{
			Verified:          identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-attach-race", Email: "race@example.test", EmailVerified: true},
			Session:           session,
			PendingHandleHash: handleHash,
			ClientID:          jitClient,
			Now:               now,
			PendingExpiresAt:  now.Add(identity.OIDCPendingLifetime),
		})
		resolved <- resolution{outcome: outcome, err: resolveErr}
	}()
	waitForBlockedBackend(t, ctx, observer)
	if err := deleter.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got := <-resolved
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.outcome.Resolution != identity.OIDCLoginNeedsPending {
		t.Fatalf("attach racing a deletion outcome=%+v, want NeedsPending", got.outcome)
	}
	var identities, sessions int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM oidc_identities WHERE subject='subject-attach-race'),
		(SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid)`, owner).Scan(&identities, &sessions); err != nil || identities != 0 || sessions != 0 {
		t.Fatalf("identities=%d sessions=%d err=%v", identities, sessions, err)
	}
}
