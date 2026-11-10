package postgres

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// TestPasswordResetPostgresCredentialUpdateReportsAffectedRows covers the
// execrows contract behind the reset write: one row means the password moved to
// the next credential version, and zero rows must stay visible so the service
// can roll back without consuming the link.
func TestPasswordResetPostgresCredentialUpdateReportsAffectedRows(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	_, target, _ := passwordResetFixture(t, ctx, admin)
	repository := NewPasswordResetRepository(app, passwordResetFactory)

	var before int64
	if err := admin.QueryRow(ctx, `SELECT credential_version FROM local_credentials WHERE user_id=$1::uuid`, target).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var rows int64
	if err := repository.WithPasswordResetTransaction(ctx, func(tx identity.PasswordResetTransaction) error {
		var e error
		rows, e = tx.UpdateLocalCredential(ctx, target, "hash:replacement")
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("credential update rows=%d", rows)
	}
	var after int64
	var hash string
	if err := admin.QueryRow(ctx, `SELECT credential_version, password_hash FROM local_credentials WHERE user_id=$1::uuid`, target).Scan(&after, &hash); err != nil {
		t.Fatal(err)
	}
	if after != before+1 || hash != "hash:replacement" {
		t.Fatalf("version %d=>%d hash=%q", before, after, hash)
	}

	var withoutCredential string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('reset_no_credential') RETURNING id::text`).Scan(&withoutCredential); err != nil {
		t.Fatal(err)
	}
	if err := repository.WithPasswordResetTransaction(ctx, func(tx identity.PasswordResetTransaction) error {
		var e error
		rows, e = tx.UpdateLocalCredential(ctx, withoutCredential, "hash:ignored")
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("missing credential update rows=%d", rows)
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
