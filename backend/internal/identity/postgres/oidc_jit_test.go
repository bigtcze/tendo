package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

const jitIssuer = "https://issuer.test"
const jitClient = "test-client"

func jitRepo(app *pgxpool.Pool) *Repository {
	return NewWithInvitations(app, nil, memberPeopleFactory, invitationFactory)
}

func jitHandle(seed byte) (string, []byte) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed
	}
	return base64.RawURLEncoding.EncodeToString(raw), raw
}

func jitSession(seed byte, now time.Time) (string, identity.NewSession) {
	token := "session-" + fmt.Sprintf("%d", seed)
	hash := sha256.Sum256([]byte(token))
	return token, identity.NewSession{TokenHash: hash[:], CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
}

func jitResolve(t *testing.T, repo *Repository, verified identity.OIDCVerifiedIdentity, handleHash []byte, now time.Time, invite []byte) identity.OIDCLoginOutcome {
	t.Helper()
	_, session := jitSession(1, now)
	outcome, err := repo.ResolveLogin(context.Background(), identity.OIDCLoginRequest{
		Verified: verified, Session: session, PendingHandleHash: handleHash, ClientID: jitClient,
		InviteTokenHash: invite, Now: now, PendingExpiresAt: now.Add(identity.OIDCPendingLifetime),
	})
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func jitCounts(t *testing.T, ctx context.Context, admin *pgxpool.Pool) (accounts, identities, sessions, memberships int) {
	t.Helper()
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_accounts),(SELECT count(*) FROM oidc_identities),
		(SELECT count(*) FROM user_sessions),(SELECT count(*) FROM household_memberships)`).Scan(&accounts, &identities, &sessions, &memberships); err != nil {
		t.Fatal(err)
	}
	return
}

func TestOIDCJITFirstLoginCreatesExactlyOneAccount(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	_, handleHash := jitHandle(10)
	verified := identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-new", Email: "anna@example.test", EmailVerified: true}
	if outcome := jitResolve(t, repo, verified, handleHash, now, nil); outcome.Resolution != identity.OIDCLoginNeedsPending {
		t.Fatalf("outcome=%+v", outcome)
	}
	accounts, identities, sessions, memberships := jitCounts(t, ctx, admin)
	if accounts != 0 || identities != 0 || sessions != 0 || memberships != 0 {
		t.Fatalf("row 3 wrote state: %d/%d/%d/%d", accounts, identities, sessions, memberships)
	}
	_, session := jitSession(2, now)
	outcome, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
		HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, Login: "member-abcdefgh", Session: session, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Login != "member-abcdefgh" || outcome.UserID == "" || outcome.Invitation != identity.OIDCInvitationNone {
		t.Fatalf("outcome=%+v", outcome)
	}
	accounts, identities, sessions, memberships = jitCounts(t, ctx, admin)
	if accounts != 1 || identities != 1 || sessions != 1 || memberships != 0 {
		t.Fatalf("row 6 wrote %d/%d/%d/%d", accounts, identities, sessions, memberships)
	}
	var credential, role, email string
	var emailSource *string
	if err := admin.QueryRow(ctx, `SELECT COALESCE((SELECT password_hash FROM local_credentials WHERE user_id=u.id),''),
		COALESCE((SELECT default_household_id::text FROM user_accounts WHERE id=u.id),''), COALESCE(u.email,''), u.email_source
		FROM user_accounts u WHERE u.id=$1`, outcome.UserID).Scan(&credential, &role, &email, &emailSource); err != nil {
		t.Fatal(err)
	}
	if credential != "" || role != "" || email != "anna@example.test" || emailSource == nil || *emailSource != "oidc_claim" {
		t.Fatalf("jit account credential=%q default=%q email=%q source=%v", credential, role, email, emailSource)
	}
	// The pending handle is single use.
	if _, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
		HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, Login: "member-second01", Session: session, Now: now,
	}); !errors.Is(err, identity.ErrOIDCPendingNotFound) {
		t.Fatalf("replay err=%v", err)
	}
}

func TestOIDCJITReturningLoginAndVerifiedEmailMatch(t *testing.T) {
	t.Run("row 1 reuses the linked account", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		invitationFixture(t, ctx, admin)
		repo := jitRepo(app)
		now := time.Now().UTC().Truncate(time.Second)
		var user string
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('returning') RETURNING id::text`).Scan(&user); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES($1,'subject-1',$2,$3)`, jitIssuer, user, now); err != nil {
			t.Fatal(err)
		}
		_, handleHash := jitHandle(20)
		outcome := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-1", Email: "other@example.test", EmailVerified: true}, handleHash, now, nil)
		if outcome.Resolution != identity.OIDCLoginSignedIn {
			t.Fatalf("outcome=%+v", outcome)
		}
		var storedEmail *string
		if err := admin.QueryRow(ctx, `SELECT email FROM user_accounts WHERE id=$1`, user).Scan(&storedEmail); err != nil {
			t.Fatal(err)
		}
		if storedEmail == nil || *storedEmail != "other@example.test" {
			t.Fatalf("row 1 did not record the verified claim: %v", storedEmail)
		}
		if accounts, identities, _, _ := jitCounts(t, ctx, admin); accounts != 2 || identities != 1 {
			t.Fatalf("row 1 created state: accounts=%d identities=%d", accounts, identities)
		}
	})
	t.Run("row 2 reuses the verified email owner and its membership", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		owner, house := invitationFixture(t, ctx, admin)
		repo := jitRepo(app)
		now := time.Now().UTC().Truncate(time.Second)
		if _, err := admin.Exec(ctx, `UPDATE user_accounts SET email='anna@example.test', email_verified_at=$1, email_source='account_confirmed', email_issuer=$3 WHERE id=$2`, now, owner, jitIssuer); err != nil {
			t.Fatal(err)
		}
		_, handleHash := jitHandle(21)
		outcome := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-2", Email: "anna@example.test", EmailVerified: true}, handleHash, now, nil)
		if outcome.Resolution != identity.OIDCLoginSignedIn {
			t.Fatalf("outcome=%+v", outcome)
		}
		var linked string
		if err := admin.QueryRow(ctx, `SELECT user_id::text FROM oidc_identities WHERE issuer=$1 AND subject='subject-2'`, jitIssuer).Scan(&linked); err != nil {
			t.Fatal(err)
		}
		if linked != owner {
			t.Fatalf("linked=%s owner=%s", linked, owner)
		}
		var membership int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1 AND household_id=$2`, owner, house).Scan(&membership); err != nil || membership != 1 {
			t.Fatalf("membership=%d err=%v", membership, err)
		}
	})
	t.Run("row 2c refuses a second subject for the same account", func(t *testing.T) {
		ctx, admin, app, _ := invitationPools(t)
		owner, _ := invitationFixture(t, ctx, admin)
		repo := jitRepo(app)
		now := time.Now().UTC().Truncate(time.Second)
		if _, err := admin.Exec(ctx, `UPDATE user_accounts SET email='anna@example.test', email_verified_at=$1, email_source='oidc_claim', email_issuer=$3 WHERE id=$2`, now, owner, jitIssuer); err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES($1,'subject-old',$2,$3)`, jitIssuer, owner, now); err != nil {
			t.Fatal(err)
		}
		_, handleHash := jitHandle(22)
		outcome := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-new", Email: "anna@example.test", EmailVerified: true}, handleHash, now, nil)
		if outcome.Resolution != identity.OIDCLoginConflict {
			t.Fatalf("outcome=%+v", outcome)
		}
		var pending int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM oidc_pending_logins WHERE handle_hash=$1`, handleHash).Scan(&pending); err != nil || pending != 0 {
			t.Fatalf("row 2c wrote a pending row: %d err=%v", pending, err)
		}
	})
}

func TestOIDCJITUnverifiedEmailNeverAttaches(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, _ := invitationFixture(t, ctx, admin)
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := admin.Exec(ctx, `UPDATE user_accounts SET email='anna@example.test', email_verified_at=$1, email_source='account_confirmed', email_issuer=$3 WHERE id=$2`, now, owner, jitIssuer); err != nil {
		t.Fatal(err)
	}
	_, handleHash := jitHandle(30)
	outcome := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-unverified", Email: "anna@example.test", EmailVerified: false}, handleHash, now, nil)
	if outcome.Resolution != identity.OIDCLoginNeedsPending {
		t.Fatalf("row 5 outcome=%+v", outcome)
	}
	if accounts, identities, _, _ := jitCounts(t, ctx, admin); accounts != 1 || identities != 0 {
		t.Fatalf("row 5 wrote state: accounts=%d identities=%d", accounts, identities)
	}
	_, session := jitSession(3, now)
	created, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
		HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, Login: "member-unverif01", Session: session, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	var email *string
	if err := admin.QueryRow(ctx, `SELECT email FROM user_accounts WHERE id=$1`, created.UserID).Scan(&email); err != nil || email != nil {
		t.Fatalf("unverified email stored: %v err=%v", email, err)
	}
}

func TestOIDCJITPendingRejections(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	_, handleHash := jitHandle(40)
	if out := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-pending"}, handleHash, now, nil); out.Resolution != identity.OIDCLoginNeedsPending {
		t.Fatalf("outcome=%+v", out)
	}
	_, session := jitSession(4, now)
	create := func(at time.Time, issuer, client string) error {
		_, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
			HandleHash: handleHash, Issuer: issuer, ClientID: client, Login: "member-pending1", Session: session, Now: at,
		})
		return err
	}
	if err := create(now.Add(identity.OIDCPendingLifetime), jitIssuer, jitClient); !errors.Is(err, identity.ErrOIDCPendingNotFound) {
		t.Fatalf("expiry err=%v", err)
	}
	if err := create(now, "https://other.test", jitClient); !errors.Is(err, identity.ErrOIDCPendingNotFound) {
		t.Fatalf("issuer mismatch err=%v", err)
	}
	if err := create(now, jitIssuer, "other-client"); !errors.Is(err, identity.ErrOIDCPendingNotFound) {
		t.Fatalf("client mismatch err=%v", err)
	}
	if _, err := repo.ReadPendingLogin(ctx, handleHash, now, jitIssuer, jitClient); err != nil {
		t.Fatalf("valid pending unreadable: %v", err)
	}
	if err := create(now, jitIssuer, jitClient); err != nil {
		t.Fatalf("valid continuation failed: %v", err)
	}
}

func TestOIDCJITPendingAttemptsExhaustion(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	_, handleHash := jitHandle(41)
	jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-attempts"}, handleHash, now, nil)
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := repo.RecordPendingFailure(ctx, handleHash, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.ReadPendingLogin(ctx, handleHash, now, jitIssuer, jitClient); !errors.Is(err, identity.ErrOIDCPendingNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestOIDCJITConcurrentCallbacksSameSubjectCreateOneUser(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, _ := invitationFixture(t, ctx, admin)
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	// A verified email owner makes both callbacks race into row 2: the identity
	// primary key picks one insert, the loser retries and becomes row 1.
	if _, err := admin.Exec(ctx, `UPDATE user_accounts SET email='race@example.test', email_verified_at=$1, email_source='account_confirmed', email_issuer=$3 WHERE id=$2`, now, owner, jitIssuer); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([]identity.OIDCLoginOutcome, 2)
	errs := make([]error, 2)
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, session := jitSession(byte(50+index), now)
			results[index], errs[index] = repo.ResolveLogin(context.Background(), identity.OIDCLoginRequest{
				Verified:          identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-race", Email: "race@example.test", EmailVerified: true},
				Session:           session,
				PendingHandleHash: bytes32(byte(60 + index)),
				ClientID:          jitClient,
				Now:               now, PendingExpiresAt: now.Add(identity.OIDCPendingLifetime),
			})
		}(index)
	}
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("callback %d err=%v", index, err)
		}
		if results[index].Resolution != identity.OIDCLoginSignedIn {
			t.Fatalf("callback %d outcome=%+v", index, results[index])
		}
	}
	accounts, identities, sessions, _ := jitCounts(t, ctx, admin)
	if accounts != 1 || identities != 1 || sessions != 2 {
		t.Fatalf("accounts=%d identities=%d sessions=%d", accounts, identities, sessions)
	}
	// A later callback for the same subject is row 1 and signs in.
	outcome := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-race"}, bytes32(61), now, nil)
	if outcome.Resolution != identity.OIDCLoginSignedIn {
		t.Fatalf("row 1 outcome=%+v", outcome)
	}
}

func TestOIDCJITConcurrentPendingAccountDoubleSubmit(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	_, handleHash := jitHandle(80)
	jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-double"}, handleHash, now, nil)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, session := jitSession(byte(90+index), now)
			_, errs[index] = repo.CreatePendingAccount(context.Background(), identity.OIDCPendingAccountParams{
				HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient,
				Login: fmt.Sprintf("member-double%d", index), Session: session, Now: now,
			})
		}(index)
	}
	wg.Wait()
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, identity.ErrOIDCPendingNotFound) {
			t.Fatalf("unexpected err=%v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("succeeded=%d errs=%v", succeeded, errs)
	}
	if accounts, _, _, _ := jitCounts(t, ctx, admin); accounts != 1 {
		t.Fatalf("accounts=%d", accounts)
	}
}

func TestOIDCJITPrunesExpiredContinuations(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	_, expiredHandle := jitHandle(140)
	if _, err := admin.Exec(ctx, `INSERT INTO oidc_pending_logins(handle_hash,issuer,subject,email_verified,client_id,created_at,expires_at)
		VALUES($1,$2,'expired-subject',false,$3,$4,$5)`, expiredHandle, jitIssuer, jitClient, now.Add(-6*time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, freshHandle := jitHandle(141)
	if outcome := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "fresh-subject"}, freshHandle, now, nil); outcome.Resolution != identity.OIDCLoginNeedsPending {
		t.Fatalf("outcome=%+v", outcome)
	}
	var expired, fresh int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM oidc_pending_logins WHERE handle_hash=$1),
		(SELECT count(*) FROM oidc_pending_logins WHERE handle_hash=$2)`, expiredHandle, freshHandle).Scan(&expired, &fresh); err != nil {
		t.Fatal(err)
	}
	if expired != 0 || fresh != 1 {
		t.Fatalf("expired=%d fresh=%d", expired, fresh)
	}
}

func TestOIDCJITEmailTrustIsIssuerScoped(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	owner, _ := invitationFixture(t, ctx, admin)
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	const issuerA = "https://issuer-a.test"
	if _, err := admin.Exec(ctx, `UPDATE user_accounts SET email='anna@example.test', email_verified_at=$1, email_source='oidc_claim', email_issuer=$3 WHERE id=$2`, now, owner, issuerA); err != nil {
		t.Fatal(err)
	}
	// Same verified address from another issuer must not attach.
	_, otherHash := jitHandle(150)
	outcome := jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-other-issuer", Email: "anna@example.test", EmailVerified: true}, otherHash, now, nil)
	if outcome.Resolution != identity.OIDCLoginNeedsPending {
		t.Fatalf("cross-issuer outcome=%+v", outcome)
	}
	var identities int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM oidc_identities WHERE subject='subject-other-issuer'`).Scan(&identities); err != nil || identities != 0 {
		t.Fatalf("cross-issuer identities=%d err=%v", identities, err)
	}
	// The proving issuer still attaches.
	_, sameHash := jitHandle(151)
	outcome = jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: issuerA, Subject: "subject-same-issuer", Email: "anna@example.test", EmailVerified: true}, sameHash, now, nil)
	if outcome.Resolution != identity.OIDCLoginSignedIn {
		t.Fatalf("same-issuer outcome=%+v", outcome)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM oidc_identities WHERE issuer=$1 AND subject='subject-same-issuer' AND user_id=$2`, issuerA, owner).Scan(&identities); err != nil || identities != 1 {
		t.Fatalf("same-issuer identities=%d err=%v", identities, err)
	}
}

func TestOIDCJITAccountConflictConsumesPending(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	repo := jitRepo(app)
	now := time.Now().UTC().Truncate(time.Second)
	_, handleHash := jitHandle(160)
	jitResolve(t, repo, identity.OIDCVerifiedIdentity{Issuer: jitIssuer, Subject: "subject-late-link"}, handleHash, now, nil)
	var user string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('late_link_owner') RETURNING id::text`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES($1,'subject-late-link',$2,$3)`, jitIssuer, user, now); err != nil {
		t.Fatal(err)
	}
	_, session := jitSession(161, now)
	if _, err := repo.CreatePendingAccount(ctx, identity.OIDCPendingAccountParams{
		HandleHash: handleHash, Issuer: jitIssuer, ClientID: jitClient, Login: "member-lateconf1", Session: session, Now: now,
	}); !errors.Is(err, identity.ErrOIDCConflict) {
		t.Fatalf("err=%v", err)
	}
	var pending int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM oidc_pending_logins WHERE handle_hash=$1`, handleHash).Scan(&pending); err != nil || pending != 0 {
		t.Fatalf("conflict kept the continuation: %d err=%v", pending, err)
	}
}
