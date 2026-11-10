package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	testpostgres "github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func jitLocalAccount(t *testing.T, ctx context.Context, admin *pgxpool.Pool, login string) (string, string) {
	t.Helper()
	hash, err := security.HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	var user string
	if err = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, login).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1,$2)`, user, hash); err != nil {
		t.Fatal(err)
	}
	return user, hash
}

func TestOIDCJITLinkPendingAccount(t *testing.T) {
	t.Run("row 7 binds identity and confirms the email", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
			t.Fatal(err)
		}
		_, handleHash := jitHandle(100)
		repo := jitRepo(app)
		now := time.Now().UTC().Truncate(time.Second)
		user, hash := jitLocalAccount(t, ctx, admin, "local_owner")
		jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-local", Email: "anna@example.test", EmailVerified: true}, handleHash, now, nil)
		_, session := jitSession(101, now)
		outcome, err := repo.LinkPendingAccount(ctx, identity.OIDCPendingLinkParams{
			HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, UserID: user,
			VerifiedPasswordHash: hash, Session: session, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Conflict || !outcome.EmailSaved || outcome.Invitation != identity.OIDCInvitationNone {
			t.Fatalf("outcome=%+v", outcome)
		}
		var linked string
		if err = admin.QueryRow(ctx, `SELECT user_id::text FROM oidc_identities WHERE issuer=$1 AND subject='subject-local'`, jitIssuer).Scan(&linked); err != nil || linked != user {
			t.Fatalf("identity=%q err=%v", linked, err)
		}
		var email, source string
		if err = admin.QueryRow(ctx, `SELECT email,email_source FROM user_accounts WHERE id=$1`, user).Scan(&email, &source); err != nil || email != "anna@example.test" || source != "account_confirmed" {
			t.Fatalf("email=%q source=%q err=%v", email, source, err)
		}
		var sessions int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE token_hash=$1`, session.TokenHash).Scan(&sessions); err != nil || sessions != 1 {
			t.Fatalf("sessions=%d err=%v", sessions, err)
		}
		// Replay after success finds nothing.
		_, replay := jitSession(102, now)
		if _, err := repo.LinkPendingAccount(ctx, identity.OIDCPendingLinkParams{
			HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, UserID: user,
			VerifiedPasswordHash: hash, Session: replay, Now: now,
		}); !errors.Is(err, identity.ErrOIDCPendingNotFound) {
			t.Fatalf("replay err=%v", err)
		}
	})
	t.Run("row 7e reports the email was not saved", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
			t.Fatal(err)
		}
		_, handleHash := jitHandle(103)
		repo := jitRepo(app)
		now := time.Now().UTC().Truncate(time.Second)
		user, hash := jitLocalAccount(t, ctx, admin, "local_7e")
		var other string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('other_owner') RETURNING id::text`).Scan(&other); err != nil {
			t.Fatal(err)
		}
		jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-7e", Email: "anna@example.test", EmailVerified: true}, handleHash, now, nil)
		if _, err := admin.Exec(ctx, `UPDATE user_accounts SET email='anna@example.test', email_verified_at=$1, email_source='oidc_claim', email_issuer=$3 WHERE id=$2`, now, other, jitIssuer); err != nil {
			t.Fatal(err)
		}
		_, session := jitSession(104, now)
		outcome, err := repo.LinkPendingAccount(ctx, identity.OIDCPendingLinkParams{
			HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, UserID: user,
			VerifiedPasswordHash: hash, Session: session, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if outcome.EmailSaved || outcome.Conflict {
			t.Fatalf("outcome=%+v", outcome)
		}
		var email *string
		if err := admin.QueryRow(ctx, `SELECT email FROM user_accounts WHERE id=$1`, user).Scan(&email); err != nil || email != nil {
			t.Fatalf("email=%v err=%v", email, err)
		}
	})
	t.Run("row 7a consumes the pending row and binds nothing", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
			t.Fatal(err)
		}
		_, handleHash := jitHandle(105)
		repo := jitRepo(app)
		now := time.Now().UTC().Truncate(time.Second)
		user, hash := jitLocalAccount(t, ctx, admin, "local_7a")
		var other string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('race_owner') RETURNING id::text`).Scan(&other); err != nil {
			t.Fatal(err)
		}
		jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-7a"}, handleHash, now, nil)
		if _, err := admin.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES($1,'subject-7a',$2,$3)`, jitIssuer, other, now); err != nil {
			t.Fatal(err)
		}
		_, session := jitSession(106, now)
		outcome, err := repo.LinkPendingAccount(ctx, identity.OIDCPendingLinkParams{
			HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, UserID: user,
			VerifiedPasswordHash: hash, Session: session, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if !outcome.Conflict {
			t.Fatalf("outcome=%+v", outcome)
		}
		var pending, sessions int
		if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM oidc_pending_logins WHERE handle_hash=$1),(SELECT count(*) FROM user_sessions WHERE token_hash=$2)`, handleHash, session.TokenHash).Scan(&pending, &sessions); err != nil || pending != 0 || sessions != 0 {
			t.Fatalf("pending=%d sessions=%d err=%v", pending, sessions, err)
		}
	})
	t.Run("row 7 racing a password change binds nothing", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
			t.Fatal(err)
		}
		_, handleHash := jitHandle(107)
		repo := jitRepo(app)
		now := time.Now().UTC().Truncate(time.Second)
		user, hash := jitLocalAccount(t, ctx, admin, "local_reset")
		jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-reset"}, handleHash, now, nil)
		// The user proved the old password, then an administrator resets it
		// before the binding transaction starts.
		newHash, err := security.HashPassword("a different password")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = admin.Exec(ctx, `UPDATE local_credentials SET password_hash=$2 WHERE user_id=$1`, user, newHash); err != nil {
			t.Fatal(err)
		}
		_, session := jitSession(108, now)
		if _, err = repo.LinkPendingAccount(ctx, identity.OIDCPendingLinkParams{
			HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, UserID: user,
			VerifiedPasswordHash: hash, Session: session, Now: now,
		}); !errors.Is(err, identity.ErrInvalidCredentials) {
			t.Fatalf("err=%v", err)
		}
		var identities, sessions int
		if err = admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM oidc_identities WHERE subject='subject-reset'),(SELECT count(*) FROM user_sessions WHERE token_hash=$1)`, session.TokenHash).Scan(&identities, &sessions); err != nil || identities != 0 || sessions != 0 {
			t.Fatalf("identities=%d sessions=%d err=%v", identities, sessions, err)
		}
	})
}

// waitForClaimLock waits until one backend is blocked on a row/index lock, so
// the deterministic claim race can release the holder at the right moment.
func waitForClaimLock(t *testing.T, ctx context.Context, app *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		// The runtime role can only inspect its own sessions; both racing
		// transactions use that role.
		if err := app.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	var diagnostics []string
	rows, err := app.Query(ctx, `SELECT datname||'|'||state||'|'||COALESCE(wait_event_type,'')||'|'||COALESCE(wait_event,'')||'|'||left(query,60)
		FROM pg_stat_activity WHERE pid<>pg_backend_pid()`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var line string
			if rows.Scan(&line) == nil {
				diagnostics = append(diagnostics, line)
			}
		}
	}
	t.Fatalf("loser never blocked on the email unique index: %v", diagnostics)
}

func TestOIDCJITDifferentSubjectsSameEmailOwnOnce(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	base := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	handleA, hashA := jitHandle(110)
	handleB, hashB := jitHandle(111)
	_, handleC := jitHandle(112)
	_ = handleC
	jitResolve(t, base, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-a", Email: "anna@example.test", EmailVerified: true}, hashA, now, nil)
	jitResolve(t, base, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-b", Email: "anna@example.test", EmailVerified: true}, hashB, now, nil)

	// B claims the address and pauses before commit, holding the unique index.
	paused := NewWithInvitations(app, nil, memberPeopleFactory, invitationFactory)
	claimed, release := make(chan struct{}), make(chan struct{})
	paused.emailClaimPause = func() { close(claimed); <-release }
	bDone := make(chan error, 1)
	go func() {
		_, session := jitSession(115, now)
		_, err := paused.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
			HandleHash: hashB, Issuer: jitIssuer, ClientID: jitClient, Login: "member-emailB01", Session: session, Now: now,
		})
		bDone <- err
	}()
	<-claimed

	// A (the loser) must really hit user_accounts_email_unique while B is
	// uncommitted; it then skips the email and still creates its account.
	aDone := make(chan error, 1)
	go func() {
		_, session := jitSession(116, now)
		_, err := base.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
			HandleHash: hashA, Issuer: jitIssuer, ClientID: jitClient, Login: "member-emailA01", Session: session, Now: now,
		})
		aDone <- err
	}()
	select {
	case err := <-aDone:
		t.Fatalf("loser finished before any lock wait: err=%v", err)
	default:
	}
	waitForClaimLock(t, ctx, app)
	close(release)
	if err := <-bDone; err != nil {
		t.Fatalf("first claimer err=%v", err)
	}
	if err := <-aDone; err != nil {
		t.Fatalf("loser err=%v", err)
	}
	var owners int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts WHERE email='anna@example.test' AND email_issuer=$1`, jitIssuer).Scan(&owners); err != nil || owners != 1 {
		t.Fatalf("email owners=%d err=%v", owners, err)
	}
	var accounts int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts WHERE login IN ('member-emailA01','member-emailB01')`).Scan(&accounts); err != nil || accounts != 2 {
		t.Fatalf("accounts=%d err=%v", accounts, err)
	}
	_ = handleA
	_ = handleB
}

func TestOIDCJITRow7eLoserSkipsEmail(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	base := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	user, hash := jitLocalAccount(t, ctx, admin, "local_7e_race")
	_, linkHash := jitHandle(120)
	jitResolve(t, base, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-7e-race", Email: "race@example.test", EmailVerified: true}, linkHash, now, nil)
	_, accountHash := jitHandle(121)
	jitResolve(t, base, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-owner-race", Email: "race@example.test", EmailVerified: true}, accountHash, now, nil)

	// The competing account claims the address first and pauses before commit.
	paused := NewWithInvitations(app, nil, memberPeopleFactory, invitationFactory)
	claimed, release := make(chan struct{}), make(chan struct{})
	paused.emailClaimPause = func() { close(claimed); <-release }
	createDone := make(chan error, 1)
	go func() {
		_, session := jitSession(122, now)
		_, err := paused.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
			HandleHash: accountHash, Issuer: jitIssuer, ClientID: jitClient, Login: "member-ownerrace", Session: session, Now: now,
		})
		createDone <- err
	}()
	<-claimed

	linkDone := make(chan identity.OIDCPendingLinkOutcome, 1)
	linkErr := make(chan error, 1)
	go func() {
		_, session := jitSession(123, now)
		outcome, err := base.LinkPendingAccount(ctx, identity.OIDCPendingLinkParams{
			HandleHash: linkHash, Issuer: jitIssuer, ClientID: jitClient, UserID: user,
			VerifiedPasswordHash: hash, Session: session, Now: now,
		})
		linkDone <- outcome
		linkErr <- err
	}()
	waitForClaimLock(t, ctx, app)
	close(release)
	if err := <-createDone; err != nil {
		t.Fatalf("owner claim err=%v", err)
	}
	if err := <-linkErr; err != nil {
		t.Fatalf("link err=%v", err)
	}
	outcome := <-linkDone
	if outcome.Conflict || outcome.EmailSaved {
		t.Fatalf("outcome=%+v", outcome)
	}
	var email *string
	if err := admin.QueryRow(ctx, `SELECT email FROM user_accounts WHERE id=$1`, user).Scan(&email); err != nil || email != nil {
		t.Fatalf("loser email=%v err=%v", email, err)
	}
	var linked int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM oidc_identities WHERE issuer=$1 AND subject='subject-7e-race' AND user_id=$2`, jitIssuer, user).Scan(&linked); err != nil || linked != 1 {
		t.Fatalf("identity linked=%d err=%v", linked, err)
	}
}

func TestOIDCJITInvitationCarryOver(t *testing.T) {
	t.Run("valid invitation joins the household", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		_, house := invitationFixture(t, ctx, admin)
		repo := jitRepo(app)
		now := time.Now().UTC().Truncate(time.Second)
		inviteRaw := make([]byte, 32)
		for i := range inviteRaw {
			inviteRaw[i] = 120
		}
		inviteHash := sha256.Sum256(inviteRaw)
		var invitationID string
		if err := admin.QueryRow(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
			SELECT $1,id,'jit-invite',$2,$3,$4 FROM user_accounts LIMIT 1 RETURNING id::text`, house, inviteHash[:], now, now.Add(time.Hour)).Scan(&invitationID); err != nil {
			t.Fatal(err)
		}
		_, handleHash := jitHandle(121)
		if out := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-invited"}, handleHash, now, inviteHash[:]); out.Resolution != identity.OIDCLoginNeedsPending {
			t.Fatalf("outcome=%+v", out)
		}
		var pendingInvite []byte
		if err := admin.QueryRow(ctx, `SELECT invite_token_hash FROM oidc_pending_logins WHERE handle_hash=$1`, handleHash).Scan(&pendingInvite); err != nil {
			t.Fatal(err)
		}
		if string(pendingInvite) != string(inviteHash[:]) {
			t.Fatal("invitation hash was not carried into the pending row")
		}
		_, session := jitSession(122, now)
		outcome, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
			HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, Login: "member-invited1", Session: session, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Invitation != identity.OIDCInvitationAccepted || outcome.DefaultHouseholdID != house {
			t.Fatalf("outcome=%+v", outcome)
		}
		var role string
		if err := admin.QueryRow(ctx, `SELECT role FROM household_memberships WHERE user_id=$1 AND household_id=$2`, outcome.UserID, house).Scan(&role); err != nil || role != "member" {
			t.Fatalf("role=%q err=%v", role, err)
		}
		var people, accepted int
		if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM subjects WHERE linked_user_id=$1 AND household_id=$2),
			(SELECT count(*) FROM household_invitations WHERE id=$3 AND accepted_at IS NOT NULL)`, outcome.UserID, house, invitationID).Scan(&people, &accepted); err != nil || people != 1 || accepted != 1 {
			t.Fatalf("people=%d accepted=%d err=%v", people, accepted, err)
		}
	})
	t.Run("expired invitation degrades but the account is created", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		_, house := invitationFixture(t, ctx, admin)
		repo := jitRepo(app)
		now := time.Now().UTC().Truncate(time.Second)
		inviteRaw := make([]byte, 32)
		for i := range inviteRaw {
			inviteRaw[i] = 121
		}
		inviteHash := sha256.Sum256(inviteRaw)
		if _, err := admin.Exec(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
			SELECT $1,id,'jit-expired',$2,$3,$4 FROM user_accounts LIMIT 1`, house, inviteHash[:], now.Add(-2*time.Hour), now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		_, handleHash := jitHandle(123)
		jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-expired-invite"}, handleHash, now, inviteHash[:])
		_, session := jitSession(124, now)
		outcome, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
			HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, Login: "member-invited2", Session: session, Now: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Invitation != identity.OIDCInvitationInvalid || outcome.DefaultHouseholdID != "" {
			t.Fatalf("outcome=%+v", outcome)
		}
		var memberships int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1`, outcome.UserID).Scan(&memberships); err != nil || memberships != 0 {
			t.Fatalf("memberships=%d err=%v", memberships, err)
		}
	})
}

func TestOIDCJITDiscardAccount(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	_, handleHash := jitHandle(130)
	jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-discard", Email: "anna@example.test", EmailVerified: true}, handleHash, now, nil)
	_, session := jitSession(131, now)
	created, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
		HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, Login: "member-discard1", Session: session, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	newHandle, newHash := jitHandle(132)
	status, err := repo.DiscardOIDCAccount(ctx, identity.OIDCDiscardParams{
		HandleHash: newHash, UserID: created.UserID, Issuer: jitIssuer, ClientID: jitClient, Now: now, ExpiresAt: now.Add(identity.OIDCPendingLifetime),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.EmailVerified {
		t.Fatalf("status=%+v", status)
	}
	if _, err := repo.ReadPendingLogin(ctx, newHash, now, jitIssuer, jitClient); err != nil {
		t.Fatalf("discard did not create a continuation: %v", err)
	}
	var accounts, identities, sessions int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_accounts WHERE id=$1),(SELECT count(*) FROM oidc_identities WHERE subject='subject-discard'),
		(SELECT count(*) FROM user_sessions WHERE user_id=$1)`, created.UserID).Scan(&accounts, &identities, &sessions); err != nil || accounts != 0 || identities != 0 || sessions != 0 {
		t.Fatalf("accounts=%d identities=%d sessions=%d err=%v", accounts, identities, sessions, err)
	}
	_ = newHandle

	// An account with a membership or credential is refused.
	_, membershipHandle := jitHandle(133)
	jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-keep"}, membershipHandle, now, nil)
	_, membershipSession := jitSession(140, now)
	memberOutcome, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
		HandleHash: membershipHandle, Issuer: jitIssuer, ClientID: jitClient, Login: "member-keep0001", Session: membershipSession, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO households(name,timezone) VALUES('discard keep','UTC')`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) SELECT $1,id,'member' FROM households LIMIT 1`, memberOutcome.UserID); err != nil {
		t.Fatal(err)
	}
	_, refusedHandle := jitHandle(134)
	if _, err := repo.DiscardOIDCAccount(ctx, identity.OIDCDiscardParams{
		HandleHash: refusedHandle, UserID: memberOutcome.UserID, Issuer: jitIssuer, ClientID: jitClient, Now: now, ExpiresAt: now.Add(identity.OIDCPendingLifetime),
	}); !errors.Is(err, identity.ErrOIDCAccountNotDiscardable) {
		t.Fatalf("membership err=%v", err)
	}
	// FK-referenced account is refused, not partially deleted.
	_, fkHandle := jitHandle(135)
	jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-fk"}, fkHandle, now, nil)
	_, fkSession := jitSession(141, now)
	fkAccount, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
		HandleHash: fkHandle, Issuer: jitIssuer, ClientID: jitClient, Login: "member-fk000001", Session: fkSession, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
		SELECT id,$1,'fk-ref',decode(repeat('ee',32),'hex'),$2,$3 FROM households LIMIT 1`, fkAccount.UserID, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, fkRefused := jitHandle(136)
	if _, err := repo.DiscardOIDCAccount(ctx, identity.OIDCDiscardParams{
		HandleHash: fkRefused, UserID: fkAccount.UserID, Issuer: jitIssuer, ClientID: jitClient, Now: now, ExpiresAt: now.Add(identity.OIDCPendingLifetime),
	}); !errors.Is(err, identity.ErrOIDCAccountNotDiscardable) {
		t.Fatalf("fk err=%v", err)
	}
	var survived int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts WHERE id=$1`, fkAccount.UserID).Scan(&survived); err != nil || survived != 1 {
		t.Fatalf("account survived=%d err=%v", survived, err)
	}
}

func TestOIDCJITMigrationFromVersion18(t *testing.T) {
	ctx, admin, _, _ := invitationPools(t)
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
	adminCfg := testpostgres.PoolConfigForDatabase(t, os.Getenv("TEST_DATABASE_ADMIN_URL"), dbName)
	admin.Close()
	admin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var uid, hid string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('pre19_owner') RETURNING id::text`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('pre19','UTC') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1,$2,'admin')`, uid, hid); err != nil {
		t.Fatal(err)
	}
	// Present the exact version 18 schema, then migrate forward.
	if _, err := admin.Exec(ctx, testpostgres.RevertVersion19SQL); err != nil {
		t.Fatal(err)
	}
	var version int
	if err := admin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&version); err != nil || version != 18 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var email, emailVerified, emailSource, emailIssuer *string
	if err := admin.QueryRow(ctx, `SELECT email,email_verified_at::text,email_source,email_issuer FROM user_accounts WHERE id=$1`, uid).Scan(&email, &emailVerified, &emailSource, &emailIssuer); err != nil {
		t.Fatal(err)
	}
	if email != nil || emailVerified != nil || emailSource != nil || emailIssuer != nil {
		t.Fatalf("existing account gained email data: %v/%v/%v/%v", email, emailVerified, emailSource, emailIssuer)
	}
	var dirty bool
	if err := admin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil || version != 19 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", version, dirty, err)
	}
	// Constraint and grant coverage for the new schema.
	for name, statement := range map[string]string{
		"non-canonical upper-case domain": `INSERT INTO user_accounts(login,email,email_verified_at,email_source,email_issuer) VALUES('bad_email_1','anna@EXAMPLE.test',now(),'oidc_claim','i')`,
		"untrimmed email":                 `INSERT INTO user_accounts(login,email,email_verified_at,email_source,email_issuer) VALUES('bad_email_2','anna@example.test ',now(),'oidc_claim','i')`,
		"partial email set":               `INSERT INTO user_accounts(login,email) VALUES('bad_email_3','anna@example.test')`,
		"missing issuer":                  `INSERT INTO user_accounts(login,email,email_verified_at,email_source) VALUES('bad_email_5','anna@example.test',now(),'oidc_claim')`,
		"unknown source":                  `INSERT INTO user_accounts(login,email,email_verified_at,email_source,email_issuer) VALUES('bad_email_4','anna@example.test',now(),'admin_confirmed','i')`,
	} {
		if _, err := admin.Exec(ctx, statement); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if _, err := admin.Exec(ctx, `INSERT INTO user_accounts(login,email,email_verified_at,email_source,email_issuer) VALUES('dup_1','dup@example.test',now(),'oidc_claim','i'),('dup_2','dup@example.test',now(),'oidc_claim','i')`); err == nil {
		t.Fatal("duplicate canonical email accepted")
	}
	if _, err := admin.Exec(ctx, `INSERT INTO oidc_pending_logins(handle_hash,issuer,subject,email,email_verified,client_id,created_at,expires_at)
		VALUES(decode(repeat('aa',32),'hex'),'https://issuer.test','subject-x',NULL,false,'c',now(),now()+interval '11 minutes')`); err == nil {
		t.Fatal("pending row beyond ten minutes accepted")
	}
}

func TestOIDCJITRow7aRollsBackInvitationAcceptance(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, house := invitationFixture(t, ctx, admin)
	now := time.Now().UTC().Truncate(time.Second)
	user, passwordHash := jitLocalAccount(t, ctx, admin, "local_7a_invite")
	rawInvite := make([]byte, 32)
	for i := range rawInvite {
		rawInvite[i] = 0x5a
	}
	inviteToken := base64.RawURLEncoding.EncodeToString(rawInvite)
	inviteHash := sha256.Sum256([]byte(inviteToken))
	var invitationID string
	if err := admin.QueryRow(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at)
		VALUES($1,$2,'row7a-invite',$3,$4,$5) RETURNING id::text`, house, owner, inviteHash[:], now, now.Add(time.Hour)).Scan(&invitationID); err != nil {
		t.Fatal(err)
	}
	_, handleHash := jitHandle(170)
	repo := jitRepo(app)
	jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-7a-invite"}, handleHash, now, inviteHash[:])

	paused := NewWithInvitations(app, nil, memberPeopleFactory, invitationFactory)
	atBind, release := make(chan struct{}), make(chan struct{})
	paused.beforeIdentityBind = func() { close(atBind); <-release }
	type result struct {
		outcome identity.OIDCPendingLinkOutcome
		err     error
	}
	done := make(chan result, 1)
	go func() {
		_, session := jitSession(171, now)
		outcome, err := paused.LinkPendingAccount(ctx, identity.OIDCPendingLinkParams{
			HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, UserID: user,
			VerifiedPasswordHash: passwordHash, Session: session, Now: now,
		})
		done <- result{outcome, err}
	}()
	<-atBind
	// A concurrent auto-attach claims (user_id, issuer) after the row 7
	// re-read, while the invitation acceptance is still uncommitted.
	if _, err := admin.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES($1,'concurrent-subject',$2,$3)`, jitIssuer, user, now); err != nil {
		t.Fatal(err)
	}
	close(release)
	got := <-done
	if got.err != nil || !got.outcome.Conflict {
		t.Fatalf("outcome=%+v err=%v", got.outcome, got.err)
	}
	var memberships, pendingRows, identities int
	var acceptedAt *time.Time
	var defaultHousehold *string
	if err := admin.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM household_memberships WHERE user_id=$1),
		(SELECT count(*) FROM oidc_pending_logins WHERE handle_hash=$2),
		(SELECT count(*) FROM oidc_identities WHERE issuer=$3 AND subject='subject-7a-invite'),
		(SELECT accepted_at FROM household_invitations WHERE id=$4),
		(SELECT default_household_id::text FROM user_accounts WHERE id=$1)`,
		user, handleHash, jitIssuer, invitationID).Scan(&memberships, &pendingRows, &identities, &acceptedAt, &defaultHousehold); err != nil {
		t.Fatal(err)
	}
	if memberships != 0 || identities != 0 || pendingRows != 0 || acceptedAt != nil || defaultHousehold != nil {
		t.Fatalf("row 7a wrote state: memberships=%d identities=%d pending=%d accepted=%v default=%v",
			memberships, identities, pendingRows, acceptedAt, defaultHousehold)
	}
}
