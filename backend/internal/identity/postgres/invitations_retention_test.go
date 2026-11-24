package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// invitationDigest reports whether the invitation row still holds a token
// digest, and fails the test when the row is gone: retention redacts, it never
// deletes history.
func invitationDigest(t *testing.T, ctx context.Context, admin *pgxpool.Pool, id string) bool {
	t.Helper()
	var held bool
	if err := admin.QueryRow(ctx, `SELECT token_hash IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, id).Scan(&held); err != nil {
		t.Fatalf("invitation %s row: %v", id, err)
	}
	return held
}

// TestInvitationRetentionTerminalRowsHoldNoDigest covers ADR 0024 on the
// application paths: revocation and acceptance clear the digest in their own
// transaction, the redacted row keeps its history and list status, a revoked
// token is refused, and a create retry with the same key still returns the
// original invitation without a token.
func TestInvitationRetentionTerminalRowsHoldNoDigest(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	revoked, err := svc.CreateInvitation(ctx, owner, house, "retention-revoked")
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := svc.CreateInvitation(ctx, owner, house, "retention-accepted")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := svc.CreateInvitation(ctx, owner, house, "retention-pending")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeInvitation(ctx, owner, house, revoked.Invitation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptInvitationNewAccount(ctx, accepted.Token, "retention_member", "a sufficiently long password"); err != nil {
		t.Fatal(err)
	}
	if invitationDigest(t, ctx, admin, revoked.Invitation.ID) {
		t.Fatal("revoked invitation still holds its token digest")
	}
	if invitationDigest(t, ctx, admin, accepted.Invitation.ID) {
		t.Fatal("accepted invitation still holds its token digest")
	}
	if !invitationDigest(t, ctx, admin, pending.Invitation.ID) {
		t.Fatal("pending invitation lost its token digest")
	}
	if _, err := svc.AcceptInvitationNewAccount(ctx, revoked.Token, "retention_late", "a sufficiently long password"); !errors.Is(err, identity.ErrInvalidInvitation) {
		t.Fatalf("revoked token accept=%v want invalid invitation", err)
	}
	// Revoking again stays a successful no-op on the redacted row.
	if err := svc.RevokeInvitation(ctx, owner, house, revoked.Invitation.ID); err != nil {
		t.Fatalf("repeat revoke=%v", err)
	}
	retry, err := svc.CreateInvitation(ctx, owner, house, "retention-revoked")
	if err != nil || retry.Created || retry.Token != "" || retry.Invitation.ID != revoked.Invitation.ID || retry.Invitation.Status != "revoked" {
		t.Fatalf("idempotent retry=%+v err=%v", retry, err)
	}
	list, _, err := svc.ListInvitations(ctx, owner, house, 10, "", false)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, inv := range list {
		statuses[inv.ID] = inv.Status
	}
	if statuses[revoked.Invitation.ID] != "revoked" || statuses[accepted.Invitation.ID] != "accepted" || statuses[pending.Invitation.ID] != "pending" {
		t.Fatalf("list statuses=%v", statuses)
	}
	var orphan int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations WHERE token_hash IS NOT NULL AND (accepted_at IS NOT NULL OR revoked_at IS NOT NULL)`).Scan(&orphan); err != nil || orphan != 0 {
		t.Fatalf("terminal rows with digest=%d err=%v", orphan, err)
	}
}

// TestInvitationRetentionDatabaseGuards pins the storage rules: a terminal row
// cannot hold a digest (CHECK), and the runtime role may clear a digest but
// never set or replace one, nor delete a row.
func TestInvitationRetentionDatabaseGuards(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	inv, err := svc.CreateInvitation(ctx, owner, house, "retention-guards")
	if err != nil {
		t.Fatal(err)
	}
	var pgErr *pgconn.PgError
	_, err = admin.Exec(ctx, `UPDATE household_invitations SET revoked_at=now() WHERE id=$1::uuid`, inv.Invitation.ID)
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "household_invitations_terminal_no_digest" {
		t.Fatalf("terminal row with digest: %v", err)
	}
	for _, q := range []string{
		`UPDATE household_invitations SET token_hash=decode(repeat('ab',32),'hex') WHERE id=$1::uuid`,
		`DELETE FROM household_invitations WHERE id=$1::uuid`,
	} {
		_, err = app.Exec(ctx, q, inv.Invitation.ID)
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := app.Exec(ctx, `UPDATE household_invitations SET token_hash=NULL WHERE id=$1::uuid`, inv.Invitation.ID); err != nil {
		t.Fatalf("runtime clear: %v", err)
	}
	_, err = app.Exec(ctx, `UPDATE household_invitations SET token_hash=decode(repeat('ab',32),'hex') WHERE id=$1::uuid`, inv.Invitation.ID)
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("runtime revive cleared digest: %v", err)
	}
}

// TestInvitationRetentionSweepClearsOnlyExpiredDigests seeds pending, expired,
// boundary and terminal rows plus more expired rows than one batch, and proves
// the sweep clears exactly the expired digests in bounded batches, keeps every
// row, and leaves list output and pending acceptance unchanged.
func TestInvitationRetentionSweepClearsOnlyExpiredDigests(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	now := time.Now().UTC().Truncate(time.Second)
	expired := identity.InvitationSweepBatchSize + 7
	if _, err := admin.Exec(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
		SELECT $1::uuid,$2::uuid,'sweep-expired-'||n,decode(lpad(to_hex(n),64,'0'),'hex'),$3::timestamptz-interval '8 days',$3::timestamptz-interval '1 hour'
		FROM generate_series(1,$4::int) n`, house, owner, now, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
		VALUES ($1::uuid,$2::uuid,'sweep-boundary',decode(repeat('b1',32),'hex'),$3::timestamptz-interval '7 days',$3::timestamptz)`, house, owner, now); err != nil {
		t.Fatal(err)
	}
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), func() time.Time { return now })
	pending, err := svc.CreateInvitation(ctx, owner, house, "sweep-pending")
	if err != nil {
		t.Fatal(err)
	}
	var before int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_invitations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	listBefore, _, err := svc.ListInvitations(ctx, owner, house, 100, "", false)
	if err != nil {
		t.Fatal(err)
	}
	report, err := identity.SweepInvitationDigests(ctx, NewInvitationRepository(app, nil), now)
	if err != nil || len(report.Locked) != 0 || report.Swept[identity.InvitationSweepExpiredDigests] != expired+1 {
		t.Fatalf("sweep report=%+v err=%v want cleared=%d", report, err, expired+1)
	}
	var after, held int
	if err := admin.QueryRow(ctx, `SELECT count(*), count(token_hash) FROM household_invitations`).Scan(&after, &held); err != nil {
		t.Fatal(err)
	}
	if after != before || held != 1 || !invitationDigest(t, ctx, admin, pending.Invitation.ID) {
		t.Fatalf("rows before=%d after=%d digests=%d", before, after, held)
	}
	listAfter, _, err := svc.ListInvitations(ctx, owner, house, 100, "", false)
	if err != nil || fmt.Sprint(listAfter) != fmt.Sprint(listBefore) {
		t.Fatalf("list changed by sweep: before=%v after=%v err=%v", listBefore, listAfter, err)
	}
	again, err := identity.SweepInvitationDigests(ctx, NewInvitationRepository(app, nil), now)
	if err != nil || again.Swept[identity.InvitationSweepExpiredDigests] != 0 {
		t.Fatalf("second sweep=%+v err=%v", again, err)
	}
	if _, err := svc.AcceptInvitationNewAccount(ctx, pending.Token, "sweep_member", "a sufficiently long password"); err != nil {
		t.Fatalf("pending invitation after sweep: %v", err)
	}
}

// TestInvitationRetentionSweepYieldsToLockedRow proves the sweep skips a row an
// acceptance or revocation holds and never waits on it: the holder finishes its
// own transition, and the next run clears what remains.
func TestInvitationRetentionSweepYieldsToLockedRow(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	now := time.Now().UTC().Truncate(time.Second)
	var held, free string
	for key, target := range map[string]*string{"lock-held": &held, "lock-free": &free} {
		if err := admin.QueryRow(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
			VALUES ($1::uuid,$2::uuid,$3,sha256(convert_to($3,'UTF8')),$4::timestamptz-interval '8 days',$4::timestamptz-interval '1 hour') RETURNING id::text`, house, owner, key, now).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM household_invitations WHERE id=$1::uuid FOR UPDATE`, held); err != nil {
		t.Fatal(err)
	}
	report, err := identity.SweepInvitationDigests(ctx, NewInvitationRepository(app, nil), now)
	if err != nil || report.Swept[identity.InvitationSweepExpiredDigests] != 1 {
		t.Fatalf("sweep with a held row=%+v err=%v", report, err)
	}
	if !invitationDigest(t, ctx, admin, held) || invitationDigest(t, ctx, admin, free) {
		t.Fatal("sweep touched the held row or missed the free one")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	report, err = identity.SweepInvitationDigests(ctx, NewInvitationRepository(app, nil), now)
	if err != nil || report.Swept[identity.InvitationSweepExpiredDigests] != 1 || invitationDigest(t, ctx, admin, held) {
		t.Fatalf("follow-up sweep=%+v err=%v", report, err)
	}
}

// TestInvitationRetentionAcceptRevokeAndSweepRace races acceptance, revocation
// and the sweep on the same invitation. Even rounds sweep at the real time, so
// the sweep must leave the still-valid invitation alone; odd rounds sweep with a
// cutoff past expiry, so the sweep competes for the row itself and must skip it
// while an acceptance or revocation holds it. Every round ends with exactly one
// winner among the three, no terminal row holding a digest, and no failure.
func TestInvitationRetentionAcceptRevokeAndSweepRace(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	svc, _ := identity.NewInvitationService(NewInvitationRepository(app, invitationFactory, memberPeopleFactory), time.Now)
	sweeper := NewInvitationRepository(app, nil)
	for n := 0; n < 20; n++ {
		inv, err := svc.CreateInvitation(ctx, owner, house, fmt.Sprintf("triple-race-%d", n))
		if err != nil {
			t.Fatal(err)
		}
		cutoff := time.Now()
		pastExpiry := n%2 == 1
		if pastExpiry {
			cutoff = inv.Invitation.ExpiresAt.Add(time.Second)
		} else {
			// A sweep at real time on its own must leave the valid digest.
			if _, err := identity.SweepInvitationDigests(ctx, sweeper, cutoff); err != nil {
				t.Fatal(err)
			}
			if !invitationDigest(t, ctx, admin, inv.Invitation.ID) {
				t.Fatalf("round=%d sweep at real time cleared a still-valid digest", n)
			}
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var acceptErr, revokeErr, sweepErr error
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-start
			_, acceptErr = svc.AcceptInvitationNewAccount(ctx, inv.Token, fmt.Sprintf("triple_race_%02d", n), "another long password")
		}()
		go func() {
			defer wg.Done()
			<-start
			revokeErr = svc.RevokeInvitation(ctx, owner, house, inv.Invitation.ID)
		}()
		go func() {
			defer wg.Done()
			<-start
			_, sweepErr = identity.SweepInvitationDigests(ctx, sweeper, cutoff)
		}()
		close(start)
		wg.Wait()
		if revokeErr != nil || sweepErr != nil {
			t.Fatalf("round=%d revoke=%v sweep=%v", n, revokeErr, sweepErr)
		}
		if acceptErr != nil && !errors.Is(acceptErr, identity.ErrInvalidInvitation) {
			t.Fatalf("round=%d accept=%v", n, acceptErr)
		}
		var a, r int
		var digest bool
		if err := admin.QueryRow(ctx, `SELECT (accepted_at IS NOT NULL)::int,(revoked_at IS NOT NULL)::int,token_hash IS NOT NULL FROM household_invitations WHERE id=$1::uuid`, inv.Invitation.ID).Scan(&a, &r, &digest); err != nil {
			t.Fatal(err)
		}
		if digest || (acceptErr == nil) != (a == 1) {
			t.Fatalf("round=%d accepted=%d revoked=%d digest=%v acceptErr=%v", n, a, r, digest, acceptErr)
		}
		// At real time the invitation is valid, so the sweep never wins (checked
		// directly before the race above): accept or revoke does. Past expiry the sweep may win, leaving the row pending
		// in its timestamps but without a digest, so nobody can use it.
		if a+r > 1 || (!pastExpiry && a+r != 1) {
			t.Fatalf("round=%d accepted=%d revoked=%d pastExpiry=%v", n, a, r, pastExpiry)
		}
	}
}

// TestInvitationRetentionSweepYieldsOnLockTimeout proves that a statement
// waiting on a table lock gives up after lock_timeout and reports the step as
// locked without failing the run, against PostgreSQL rather than a fake.
func TestInvitationRetentionSweepYieldsOnLockTimeout(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `LOCK TABLE household_invitations IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	report, err := identity.SweepInvitationDigests(ctx, NewInvitationRepository(app, nil), time.Now())
	if err != nil {
		t.Fatalf("locked sweep err=%v", err)
	}
	if len(report.Locked) != 1 || report.Locked[0] != identity.InvitationSweepExpiredDigests {
		t.Fatalf("locked steps=%v", report.Locked)
	}
	if _, ok := report.Swept[identity.InvitationSweepOIDCPendingLoginsExpired]; !ok {
		t.Fatalf("later steps did not run after the lock yield: %+v", report)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("sweep waited %v on the lock", elapsed)
	}
}

// TestInvitationRetentionSweepDeletesExpiredOIDCCopies proves the sweep also
// deletes expired OIDC flows and pending sign-ins, which may carry a copy of an
// invitation digest, and keeps the ones still in progress.
func TestInvitationRetentionSweepDeletesExpiredOIDCCopies(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	now := time.Now().UTC().Truncate(time.Second)
	// Start from empty tables (oidc_pending_logins has no foreign key the
	// fixtures truncate through) and leave them empty for later tests.
	clear := func() error {
		_, err := admin.Exec(context.Background(), `DELETE FROM oidc_flows; DELETE FROM oidc_pending_logins`)
		return err
	}
	if err := clear(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clear() })
	// Row 0 was created two hours ago and has expired; row 1 is in progress.
	for i, created := range []time.Time{now.Add(-2 * time.Hour), now} {
		if _, err := admin.Exec(ctx, `INSERT INTO oidc_flows(state_hash,browser_token_hash,issuer,client_id,nonce,pkce_verifier,purpose,created_at,expires_at,invite_token_hash)
			VALUES (decode(repeat($1,32),'hex'),decode(repeat($2,32),'hex'),'i','c','n',repeat('A',43),'login',$3::timestamptz,$3::timestamptz+interval '5 minutes',decode(repeat('ee',32),'hex'))`,
			fmt.Sprintf("%02x", 0x30+i), fmt.Sprintf("%02x", 0x40+i), created); err != nil {
			t.Fatalf("flow %d: %v", i, err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO oidc_pending_logins(handle_hash,issuer,subject,email_verified,client_id,invite_token_hash,created_at,expires_at)
			VALUES (decode(repeat($1,32),'hex'),'i',$2,false,'c',decode(repeat('ee',32),'hex'),$3::timestamptz,$3::timestamptz+interval '5 minutes')`,
			fmt.Sprintf("%02x", 0x50+i), fmt.Sprintf("subject-%d", i), created); err != nil {
			t.Fatalf("pending %d: %v", i, err)
		}
	}
	report, err := identity.SweepInvitationDigests(ctx, NewInvitationRepository(app, nil), now)
	if err != nil || report.Swept[identity.InvitationSweepOIDCFlowsExpired] != 1 || report.Swept[identity.InvitationSweepOIDCPendingLoginsExpired] != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	var flows, pending int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM oidc_flows WHERE state_hash=decode(repeat('31',32),'hex')),
		(SELECT count(*) FROM oidc_pending_logins WHERE handle_hash=decode(repeat('51',32),'hex'))`).Scan(&flows, &pending); err != nil || flows != 1 || pending != 1 {
		t.Fatalf("in-progress flow=%d pending=%d err=%v", flows, pending, err)
	}
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM oidc_flows) + (SELECT count(*) FROM oidc_pending_logins)`).Scan(&flows); err != nil || flows != 2 {
		t.Fatalf("rows left=%d err=%v want only the two in progress", flows, err)
	}
}
