package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// Connected-apps revocation raced against the other ways a grant ends (ADR 0023,
// slice A4b). Each race has one deterministic order that is forced by a held
// lock, and then a concurrent order run for deadlock freedom. The invariants are:
// no deadlock, at most one revocation event, and no surviving secret.

// userRevokeRace starts a user revocation of the account's grant on its own
// goroutine and returns its result channel.
func (e *mcpEnv) userRevokeRace(user, grantID string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- e.svc.RevokeGrantForUser(e.ctx, user, grantID) }()
	return done
}

// requireOneRevocation asserts the grant has exactly one revoked event in total,
// and that the event carries the reason the winner wrote.
func (e *mcpEnv) requireOneRevocation(t *testing.T, grantID string) string {
	t.Helper()
	var total int
	var reason string
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*), COALESCE(min(reason),'') FROM mcp_grant_events WHERE grant_id=$1::uuid AND event='revoked'`,
		grantID).Scan(&total, &reason); err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("revoked events for grant %s = %d, want exactly one", grantID, total)
	}
	return reason
}

// TestConnectedAppsRaceUserRevokeAndRefresh: a refresh holds the account lock
// while the user revoke queues behind it. The refresh commits first, so the
// revoke then revokes the rotated grant. Whatever the refresh returned, the
// rotated pair is dead afterwards and exactly one revocation exists.
func TestConnectedAppsRaceUserRevokeAndRefresh(t *testing.T) {
	env := newMCPEnv(t)
	for i := 0; i < 20; i++ {
		// A fresh account per iteration: a revoke ends its grant, so one grant
		// can only race once.
		user, session := env.newAccount(t, mcpRaceLogin("apps_race_refresh", i))
		first, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatalf("iteration %d exchange: %v", i, err)
		}
		grantID := env.grantOf(t, user).id
		var wg sync.WaitGroup
		var refreshed identity.MCPTokens
		var refreshErr, revokeErr error
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			refreshed, refreshErr = env.refresh(first.RefreshToken)
		}()
		go func() {
			defer wg.Done()
			<-start
			revokeErr = env.svc.RevokeGrantForUser(env.ctx, user, grantID)
		}()
		close(start)
		wg.Wait()
		// The revoke succeeds unless the refresh already ended the grant in a
		// way that leaves it not live for the revoke.
		if revokeErr != nil && !errors.Is(revokeErr, identity.ErrNotFound) {
			t.Fatalf("iteration %d: revoke: %v", i, revokeErr)
		}
		if refreshErr != nil && !errors.Is(refreshErr, identity.ErrMCPInvalidGrant) {
			t.Fatalf("iteration %d: refresh: unexpected %v", i, refreshErr)
		}
		if reason := env.requireOneRevocation(t, grantID); reason != string(identity.MCPRevokeUser) {
			t.Fatalf("iteration %d: revocation reason = %q, want user", i, reason)
		}
		env.requireNoMCPSecrets(t, user)
		if refreshErr == nil {
			if _, err = env.svc.VerifyAccessToken(env.ctx, refreshed.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
				t.Fatalf("iteration %d: access token minted by a refresh that raced a revoke verifies: %v", i, err)
			}
		}
	}
}

// TestConnectedAppsRaceUserRevokeAndPasswordReplacement covers both orders of a
// user revoke and a password change for one grant.
//
// The password change takes local_credentials FOR UPDATE, then the account's
// sessions, then the grant rows (revokeAccountMCPAccess). The user revoke takes
// user_accounts FOR SHARE, then local_credentials FOR SHARE (LockLiveAccount),
// then the grant's codes and the grant row FOR UPDATE. The two paths meet at
// local_credentials, which the change holds exclusively until it commits.
func TestConnectedAppsRaceUserRevokeAndPasswordReplacement(t *testing.T) {
	// Deterministic order: the change parks while holding local_credentials, so
	// the revoke must block on its share lock there. After the change commits the
	// grant is already password_change-revoked, and the revoke is ErrNotFound.
	t.Run("change holds the credential, revoke must block and then refuse", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "apps_race_pw_first")
		if _, err := env.exchange(env.issue(t, user, session)); err != nil {
			t.Fatalf("exchange: %v", err)
		}
		grantID := env.grantOf(t, user).id
		locked, release, done := env.startPasswordReplacement(user, identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: user})
		awaitLocked(t, locked, done)
		// Release the pause on every path, so a failed assertion cannot leave the
		// change's transaction open and hang the package.
		released := false
		defer func() {
			if !released {
				close(release)
				// The test has already failed; the change's result is only logged.
				if err := <-done; err != nil {
					t.Logf("paused password change after failure: %v", err)
				}
			}
		}()
		revokeDone := env.userRevokeRace(user, grantID)
		// The revoke must reach the credential share lock and wait there.
		waitForLockWaiters(t, env.ctx, env.observer, "%ShareLocalCredential%", 1)
		select {
		case err := <-revokeDone:
			t.Fatalf("revoke returned while the change held the credential: %v", err)
		default:
		}
		close(release)
		released = true
		if err := <-done; err != nil {
			t.Fatalf("password replacement: %v", err)
		}
		if err := <-revokeDone; !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("revoke after the change err=%v, want ErrNotFound", err)
		}
		env.requireGrantRevoked(t, user, string(identity.MCPRevokePasswordChange), user)
		env.requireNoMCPSecrets(t, user)
	})

	// The other order, observed without forcing it: the revoke may finish first
	// (reason user) or queue behind the change (reason password_change). Either
	// way there is no deadlock, exactly one revocation event, and no secret row.
	t.Run("either order, no deadlock, one revocation", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "apps_race_pw_either")
		if _, err := env.exchange(env.issue(t, user, session)); err != nil {
			t.Fatalf("exchange: %v", err)
		}
		grantID := env.grantOf(t, user).id
		locked, release, done := env.startPasswordReplacement(user, identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: user})
		awaitLocked(t, locked, done)
		revokeDone := env.userRevokeRace(user, grantID)
		var early error
		select {
		case early = <-revokeDone:
			revokeDone = nil
		case <-time.After(300 * time.Millisecond):
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatalf("password replacement: %v", err)
		}
		if revokeDone != nil {
			select {
			case early = <-revokeDone:
			case <-time.After(10 * time.Second):
				t.Fatal("revoke deadlocked behind the password change")
			}
		}
		if early != nil && !errors.Is(early, identity.ErrNotFound) {
			t.Fatalf("revoke racing a password change: %v", early)
		}
		reason := env.requireOneRevocation(t, grantID)
		if reason != string(identity.MCPRevokePasswordChange) && reason != string(identity.MCPRevokeUser) {
			t.Fatalf("revocation reason = %q, want password_change or user", reason)
		}
		env.requireNoMCPSecrets(t, user)
	})
}

// TestConnectedAppsRaceUserRevokeAndDeletion: the deletion holds the account row
// and the revoke queues behind it. Deletion commits first, so the revoke answers
// ErrUnauthenticated, and only the deletion's revocation exists.
func TestConnectedAppsRaceUserRevokeAndDeletion(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "apps_race_delete")
	actor := env.newActor(t)
	if _, err := env.exchange(env.issue(t, user, session)); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	grantID := env.grantOf(t, user).id

	tx, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = NewAccountEraser(tx).LockDeletableAccount(env.ctx, user); err != nil {
		t.Fatal(err)
	}
	revokeDone := env.userRevokeRace(user, grantID)
	waitForBlockedBackend(t, env.ctx, env.observer)
	if err = eraseInTx(env.ctx, tx, user, actor, env.clock.now()); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-revokeDone; !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("revoke racing a deletion err=%v, want ErrUnauthenticated", err)
	}
	env.requireDeletedGrant(t, user, actor)
	env.requireNoMCPSecrets(t, user)
	if n := env.revokedEvents(t, grantID, string(identity.MCPRevokeUser)); n != 0 {
		t.Fatalf("user revocation written after deletion: %d", n)
	}
}

// TestConnectedAppsRaceUserRevokesConcurrently: two revokes of one grant run at
// once. Exactly one wins; the other is ErrNotFound. There is one revocation
// event and no deadlock within the test deadline.
func TestConnectedAppsRaceUserRevokesConcurrently(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "apps_race_double")
	if _, err := env.exchange(env.issue(t, user, session)); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	grantID := env.grantOf(t, user).id
	ctx, cancel := context.WithTimeout(env.ctx, 20*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- env.svc.RevokeGrantForUser(ctx, user, grantID)
		}()
	}
	close(start)
	var wins, refused int
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			switch {
			case err == nil:
				wins++
			case errors.Is(err, identity.ErrNotFound):
				refused++
			default:
				t.Fatalf("concurrent revoke: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("concurrent revokes deadlocked or stalled")
		}
	}
	if wins != 1 || refused != 1 {
		t.Fatalf("wins=%d refused=%d, want exactly one of each", wins, refused)
	}
	if reason := env.requireOneRevocation(t, grantID); reason != string(identity.MCPRevokeUser) {
		t.Fatalf("revocation reason = %q, want user", reason)
	}
}
