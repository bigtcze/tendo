package postgres

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// TestPasswordResetPostgresCredentialReplacementAdvancesVersion covers the
// shared replacement write behind reset redemption: it advances the credential
// version, deletes every session and reports only the unexpired ones as
// revoked. A vanished credential row returns ErrNotFound so the service can
// roll back without consuming the link.
func TestPasswordResetPostgresCredentialReplacementAdvancesVersion(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	_, target, _ := passwordResetFixture(t, ctx, admin)
	repository := NewPasswordResetRepository(app, passwordResetFactory)
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

	var before int64
	if err := admin.QueryRow(ctx, `SELECT credential_version FROM local_credentials WHERE user_id=$1::uuid`, target).Scan(&before); err != nil {
		t.Fatal(err)
	}
	live, expired := sha256Sum("live-session-token"), sha256Sum("expired-session-token")
	if _, err := admin.Exec(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at,sign_in_method) VALUES
		($1::uuid,$2,$3,$4,'password'), ($1::uuid,$5,$3,$6,'password')`,
		target, live, now.Add(-time.Hour), now.Add(time.Hour), expired, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	var revoked identity.Revocation
	if err := repository.WithPasswordResetTransaction(ctx, func(tx identity.PasswordResetTransaction) error {
		var e error
		revoked, e = tx.ReplaceLocalPassword(ctx, target, "hash:replacement", now)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if revoked.Sessions() != 1 || !revoked.Includes(live) || revoked.Includes(expired) {
		t.Fatalf("revocation sessions=%d includesLive=%v includesExpired=%v", revoked.Sessions(), revoked.Includes(live), revoked.Includes(expired))
	}
	var after int64
	var hash string
	var sessions int
	if err := admin.QueryRow(ctx, `SELECT credential_version, password_hash FROM local_credentials WHERE user_id=$1::uuid`, target).Scan(&after, &hash); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE user_id=$1::uuid`, target).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if after != before+1 || hash != "hash:replacement" || sessions != 0 {
		t.Fatalf("version %d=>%d hash=%q remaining sessions=%d", before, after, hash, sessions)
	}

	var withoutCredential string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('reset_no_credential') RETURNING id::text`).Scan(&withoutCredential); err != nil {
		t.Fatal(err)
	}
	if err := repository.WithPasswordResetTransaction(ctx, func(tx identity.PasswordResetTransaction) error {
		_, e := tx.ReplaceLocalPassword(ctx, withoutCredential, "hash:ignored", now)
		return e
	}); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("missing credential error=%v", err)
	}
}

// TestPasswordResetPostgresIssueRacingRedeemOfPreviousLink runs a new issuance
// against a redemption of the previous link. Only two consistent outcomes are
// allowed: the redemption wins and the new link becomes outstanding, or the
// issuance revokes the old link first and the redemption is invalid. In both
// cases exactly one usable link remains and the password state matches the
// reported outcome.
func TestPasswordResetPostgresIssueRacingRedeemOfPreviousLink(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	sessions, err := identity.NewSessionService(New(app, nil), clock.now)
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetPasswordVerifier(fakeCredentialVerifier)
	current := "old-password-value"

	type issueOutcome struct {
		issued identity.PasswordResetIssued
		err    error
	}
	type redeemOutcome struct {
		err error
	}
	for round := 0; round < 5; round++ {
		replacement := fmt.Sprintf("a brand new long password %d", round)
		previous, err := service.IssuePasswordReset(ctx, owner, house, target)
		if err != nil {
			t.Fatalf("round %d issue: %v", round, err)
		}
		start := make(chan struct{})
		issues, redeems := make(chan issueOutcome, 1), make(chan redeemOutcome, 1)
		go func() {
			<-start
			issued, err := service.IssuePasswordReset(ctx, owner, house, target)
			issues <- issueOutcome{issued, err}
		}()
		go func() {
			<-start
			_, err := service.RedeemPasswordReset(ctx, previous.Token, replacement)
			redeems <- redeemOutcome{err}
		}()
		close(start)
		issued, redeemed := <-issues, <-redeems
		if issued.err != nil {
			t.Fatalf("round %d racing issue: %v", round, issued.err)
		}
		if redeemed.err != nil && !errors.Is(redeemed.err, identity.ErrInvalidPasswordReset) {
			t.Fatalf("round %d racing redeem: %v", round, redeemed.err)
		}

		var outstanding []byte
		if err = admin.QueryRow(ctx, `SELECT token_hash FROM password_reset_tokens WHERE target_user_id=$1::uuid AND used_at IS NULL AND revoked_at IS NULL`, target).Scan(&outstanding); err != nil {
			t.Fatalf("round %d outstanding: %v", round, err)
		}
		if string(outstanding) != string(sha256Sum(issued.issued.Token)) {
			t.Fatalf("round %d outstanding link is not the new issue", round)
		}
		var terminal bool
		if err = admin.QueryRow(ctx, `SELECT used_at IS NOT NULL OR revoked_at IS NOT NULL FROM password_reset_tokens WHERE token_hash=$1`, sha256Sum(previous.Token)).Scan(&terminal); err != nil || !terminal {
			t.Fatalf("round %d previous link still usable: terminal=%v err=%v", round, terminal, err)
		}
		if redeemed.err == nil {
			if _, err = sessions.Login(ctx, "reset_target", current); !errors.Is(err, identity.ErrInvalidCredentials) {
				t.Fatalf("round %d redeemed but the old password still signs in: %v", round, err)
			}
			current = replacement
		}
		if _, err = sessions.Login(ctx, "reset_target", current); err != nil {
			t.Fatalf("round %d current password sign-in: %v", round, err)
		}
	}
}

// TestPasswordResetPostgresRedeemAfterIssuerLeavesHousehold covers an issuer
// whose membership is deleted entirely: the link is void and stays unconsumed.
func TestPasswordResetPostgresRedeemAfterIssuerLeavesHousehold(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, target, house := passwordResetFixture(t, ctx, admin)
	clock := &passwordResetTestClock{at: time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)}
	service := newPasswordResetService(t, app, clock)
	issued, err := service.IssuePasswordReset(ctx, owner, house, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `DELETE FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, owner, house); err != nil {
		t.Fatal(err)
	}
	if _, err = service.RedeemPasswordReset(ctx, issued.Token, "a brand new long password"); !errors.Is(err, identity.ErrInvalidPasswordReset) {
		t.Fatalf("redeem after the issuer left: %v", err)
	}
	var hash string
	var used bool
	if err = admin.QueryRow(ctx, `SELECT c.password_hash, t.used_at IS NOT NULL FROM local_credentials c JOIN password_reset_tokens t ON t.target_user_id=c.user_id WHERE c.user_id=$1::uuid AND t.token_hash=$2`, target, sha256Sum(issued.Token)).Scan(&hash, &used); err != nil {
		t.Fatal(err)
	}
	if hash != "hash:old-password-value" || used {
		t.Fatalf("void link wrote hash=%q used=%v", hash, used)
	}
}
