package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeCredentialVerifier accepts the fixture's "hash:<password>" values so the
// race tests exercise the credential revalidation rather than Argon2.
func fakeCredentialVerifier(hash, password string) (bool, error) {
	return hash == "hash:"+password, nil
}

type loginRaceResult struct {
	session identity.Session
	err     error
}

func countSessionsFor(t *testing.T, ctx context.Context, app *pgxpool.Pool, userID string) int {
	t.Helper()
	var count int
	if err := app.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// TestLoginRacingPasswordResetKeepsNoOldPasswordSession pins the credential
// lock protocol: a sign-in that verified the old password either loses the race
// and fails its revalidation, or commits first and has its session deleted by
// the reset. Neither ordering may leave a usable session for the old password.
func TestLoginRacingPasswordResetKeepsNoOldPasswordSession(t *testing.T) {
	t.Run("reset commits before the login revalidation", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		owner, target, house := passwordResetFixture(t, ctx, admin)
		clock := &passwordResetTestClock{at: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)}
		reset := newPasswordResetService(t, app, clock)

		repo := New(app, nil)
		atInsert, release := make(chan struct{}), make(chan struct{})
		var insertOnce sync.Once
		repo.beforeSessionInsert = func() { insertOnce.Do(func() { close(atInsert) }); <-release }
		sessions, err := identity.NewSessionService(repo, clock.now)
		if err != nil {
			t.Fatal(err)
		}
		sessions.SetPasswordVerifier(fakeCredentialVerifier)

		done := make(chan loginRaceResult, 1)
		go func() {
			session, err := sessions.Login(ctx, "reset_target", "old-password-value")
			done <- loginRaceResult{session, err}
		}()
		<-atInsert

		issued, err := reset.IssuePasswordReset(ctx, owner, house, target)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = reset.RedeemPasswordReset(ctx, issued.Token, "a brand new long password"); err != nil {
			t.Fatalf("redeem during the login pause: %v", err)
		}
		close(release)

		got := <-done
		if !errors.Is(got.err, identity.ErrInvalidCredentials) || got.session.Token != "" {
			t.Fatalf("login after the reset: session=%+v err=%v, want ErrInvalidCredentials", got.session, got.err)
		}
		if left := countSessionsFor(t, ctx, app, target); left != 0 {
			t.Fatalf("sessions after the losing login: %d", left)
		}
		if _, err = sessions.Login(ctx, "reset_target", "old-password-value"); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("old password still signs in: %v", err)
		}
		if _, err = sessions.Login(ctx, "reset_target", "a brand new long password"); err != nil {
			t.Fatalf("new password sign-in: %v", err)
		}
	})

	t.Run("login commits first and the reset deletes its session", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		owner, target, house := passwordResetFixture(t, ctx, admin)
		clock := &passwordResetTestClock{at: time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)}
		reset := newPasswordResetService(t, app, clock)
		issued, err := reset.IssuePasswordReset(ctx, owner, house, target)
		if err != nil {
			t.Fatal(err)
		}

		repo := New(app, nil)
		atCommit, release := make(chan struct{}), make(chan struct{})
		var commitOnce sync.Once
		repo.beforeSessionCommit = func() { commitOnce.Do(func() { close(atCommit) }); <-release }
		sessions, err := identity.NewSessionService(repo, clock.now)
		if err != nil {
			t.Fatal(err)
		}
		sessions.SetPasswordVerifier(fakeCredentialVerifier)

		done := make(chan loginRaceResult, 1)
		go func() {
			session, err := sessions.Login(ctx, "reset_target", "old-password-value")
			done <- loginRaceResult{session, err}
		}()
		// The login now holds the credential share lock with its session written
		// but not committed. The reset must wait for that commit, so it cannot
		// miss the session it has to delete.
		<-atCommit
		resetDone := make(chan error, 1)
		go func() {
			_, err := reset.RedeemPasswordReset(ctx, issued.Token, "a brand new long password")
			resetDone <- err
		}()
		close(release)

		got := <-done
		if got.err != nil || got.session.Token == "" {
			t.Fatalf("login that held the lock: session=%+v err=%v", got.session, got.err)
		}
		if err = <-resetDone; err != nil {
			t.Fatalf("redeem after the login commit: %v", err)
		}
		if _, err = sessions.Authenticate(ctx, got.session.Token); !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("committed session survived the reset: %v", err)
		}
		if left := countSessionsFor(t, ctx, app, target); left != 0 {
			t.Fatalf("sessions after the reset: %d", left)
		}
		if _, err = sessions.Login(ctx, "reset_target", "old-password-value"); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("old password still signs in: %v", err)
		}
	})
}
