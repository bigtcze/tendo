package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/identity/oidcprovider"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/bigtcze/tendo/backend/internal/testoidc"
)

type captureOIDCProvider struct {
	*oidcprovider.Provider
	last    identity.OIDCAuthorizationParams
	lastURL string
}

func (p *captureOIDCProvider) AuthorizationURL(ctx context.Context, a identity.OIDCAuthorizationParams) (string, error) {
	p.last = a
	u, e := p.Provider.AuthorizationURL(ctx, a)
	p.lastURL = u
	return u, e
}
func (p *captureOIDCProvider) Exchange(ctx context.Context, x identity.OIDCExchangeParams) (identity.OIDCVerifiedIdentity, error) {
	return p.Provider.Exchange(ctx, x)
}

func TestOIDCPostgresCancelledLinkRollsBackWithIndependentCleanup(t *testing.T) {
	ctx, admin, _, observer := invitationPools(t)
	var user, session string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("oidc_cancel_%d", time.Now().UnixNano())).Scan(&user); err != nil {
		t.Fatal(err)
	}
	oldHash := bytes32(210)
	if err := admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,$2,now(),now()+interval '1 day') RETURNING id::text`, user, oldHash).Scan(&session); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE OR REPLACE FUNCTION tendo_oidc_test_delay_session_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(10); RETURN NEW; END $$; CREATE TRIGGER tendo_oidc_test_delay_session_insert BEFORE INSERT ON user_sessions FOR EACH ROW EXECUTE FUNCTION tendo_oidc_test_delay_session_insert()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = admin.Exec(cleanup, `DROP TRIGGER IF EXISTS tendo_oidc_test_delay_session_insert ON user_sessions; DROP FUNCTION IF EXISTS tendo_oidc_test_delay_session_insert()`)
	})
	done := make(chan error, 2)
	now := time.Now().UTC().Truncate(time.Second)
	workerConfig, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	workerConfig.ConnConfig.RuntimeParams["application_name"] = "tendo_oidc_cancel_rollback_test"
	workerCtx, workerCancel := context.WithTimeout(context.Background(), 30*time.Second)
	worker, err := pgxpool.NewWithConfig(workerCtx, workerConfig)
	if err != nil {
		workerCancel()
		t.Fatal(err)
	}
	defer worker.Close()
	defer workerCancel()
	workerRepo := New(worker, nil)
	go func() {
		_, e := workerRepo.LinkAndCreateSession(workerCtx, identity.OIDCFlow{UserID: user, SessionID: session}, identity.OIDCVerifiedIdentity{Issuer: "https://issuer.test", Subject: "cancelled-subject"}, identity.NewSession{UserID: user, TokenHash: bytes32(211), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, oldHash)
		done <- e
	}()
	deadline := time.Now().Add(8 * time.Second)
	observed := false
	var workerErr error
	for !observed {
		var active bool
		err := observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND usename=current_user AND application_name='tendo_oidc_cancel_rollback_test' AND state='active' AND query ILIKE '%INSERT INTO user_sessions%' AND wait_event_type='Timeout' AND wait_event='PgSleep')`).Scan(&active)
		if err != nil {
			workerCancel()
			t.Fatalf("observe runtime OIDC worker: %v", err)
		}
		if active {
			observed = true
			break
		}
		select {
		case workerErr = <-done:
			workerCancel()
			t.Fatalf("OIDC worker completed before reaching the delayed session insert: %v", workerErr)
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			workerCancel()
			t.Fatal("timed out waiting for runtime observer to see the session insert trigger")
		}
	}
	workerCancel()
	select {
	case workerErr = <-done:
		if workerErr == nil {
			t.Fatal("cancelled link unexpectedly succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled transaction failed to clean up promptly")
	}
	var identities, sessions int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM oidc_identities WHERE subject='cancelled-subject'`).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE id=$1`, session).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if identities != 0 || sessions != 1 {
		t.Fatalf("rollback identity=%d initiating sessions=%d", identities, sessions)
	}
	var originalSession int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE id=$1 AND token_hash=$2`, session, oldHash).Scan(&originalSession); err != nil || originalSession != 1 {
		t.Fatalf("initiating session changed or disappeared: count=%d err=%v", originalSession, err)
	}
	var replacementSession int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE token_hash=$1`, bytes32(211)).Scan(&replacementSession); err != nil || replacementSession != 0 {
		t.Fatalf("replacement session survived canceled link: count=%d err=%v", replacementSession, err)
	}
}

func TestOIDCPostgresServiceAdmission(t *testing.T) {
	ctx, admin, app, _ := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts,household_memberships,households CASCADE`); err != nil {
		t.Fatal(err)
	}
	login := fmt.Sprintf("oidc_admission_%d", time.Now().UnixNano())
	var user, house string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, login).Scan(&user); err != nil {
		t.Fatal(err)
	}
	passwordHash, err := security.HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1,$2)`, user, passwordHash); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES($1,'UTC') RETURNING id::text`, "OIDC admission "+login).Scan(&house); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1,$2,'owner')`, user, house); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2 WHERE id=$1`, user, house); err != nil {
		t.Fatal(err)
	}
	initialTokenHash := bytes32(201)
	var initialSession string
	if err = admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,$2,now(),now()+interval '1 day') RETURNING id::text`, user, initialTokenHash).Scan(&initialSession); err != nil {
		t.Fatal(err)
	}
	fake, err := testoidc.New()
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	repo := New(app, nil)
	sessions, err := identity.NewSessionService(repo, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	sessions.SetPasswordVerifier(security.NewPasswordGate(2).VerifyPassword)
	adapter := &captureOIDCProvider{Provider: oidcprovider.New(fake.Issuer(), fake.ClientID, fake.ClientSecret, &http.Client{Timeout: 3 * time.Second}, time.Now, true)}
	service, err := identity.NewOIDCService(repo, adapter, fake.Issuer(), fake.ClientID, "Test", "http://tendo.test", time.Now, nil, sessions.VerifyCurrentPassword)
	if err != nil {
		t.Fatal(err)
	}
	counts := func() (accounts, members, sessionsN int, defaultID string) {
		if e := admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts`).Scan(&accounts); e != nil {
			t.Fatal(e)
		}
		if e := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships`).Scan(&members); e != nil {
			t.Fatal(e)
		}
		if e := admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions`).Scan(&sessionsN); e != nil {
			t.Fatal(e)
		}
		if e := admin.QueryRow(ctx, `SELECT COALESCE(default_household_id::text,'') FROM user_accounts WHERE id=$1`, user).Scan(&defaultID); e != nil {
			t.Fatal(e)
		}
		return
	}
	start := func(subject string, purpose identity.OIDCPurpose, session string) identity.OIDCStartResult {
		fake.Subject = subject
		fake.Subjects = []string{subject}
		params := identity.OIDCStartParams{Purpose: purpose}
		if purpose == identity.OIDCPurposeLink {
			params.Principal = identity.Principal{UserID: user}
			params.Session = identity.SessionInfo{ID: session, Principal: identity.Principal{UserID: user}}
			params.CurrentPassword = "correct horse battery"
		}
		result, e := service.Start(ctx, params)
		if e != nil {
			t.Fatal(e)
		}
		return result
	}
	callback := func(st identity.OIDCStartResult, token string) identity.OIDCCallbackResult {
		auth, e := url.Parse(adapter.lastURL)
		if e != nil {
			t.Fatal(e)
		}
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, e := client.Get(auth.String())
		if e != nil {
			t.Fatal(e)
		}
		location, e := url.Parse(response.Header.Get("Location"))
		response.Body.Close()
		if e != nil {
			t.Fatal(e)
		}
		result, e := service.Callback(ctx, url.Values{"state": {adapter.last.State}, "code": {location.Query().Get("code")}}, st.BrowserToken, token)
		if e != nil {
			t.Fatal(e)
		}
		return result
	}
	beforeA, beforeM, beforeS, beforeD := counts()
	unknown := start("unlinked-subject", identity.OIDCPurposeLogin, "")
	unknownResult := callback(unknown, "")
	if unknownResult.Destination != "/login#oidcError=identity_not_linked" {
		t.Fatalf("unknown=%+v", unknownResult)
	}
	a, m, s, d := counts()
	if a != beforeA || m != beforeM || s != beforeS || d != beforeD {
		t.Fatalf("unknown identity changed state: %d/%d/%d/%s", a, m, s, d)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES($1,'known-subject',$2,now())`, fake.Issuer(), user); err != nil {
		t.Fatal(err)
	}
	known := start("known-subject", identity.OIDCPurposeLogin, "")
	knownResult := callback(known, "")
	if knownResult.Destination != "/" || knownResult.SessionToken == "" {
		t.Fatalf("known=%+v", knownResult)
	}
	a, m, s, d = counts()
	if a != beforeA || m != beforeM || s != beforeS+1 || d != beforeD {
		t.Fatalf("login changed admission state: %d/%d/%d/%s", a, m, s, d)
	}
	loginHash := sha256.Sum256([]byte(knownResult.SessionToken))
	var loginSessionID string
	if err = admin.QueryRow(ctx, `SELECT id::text FROM user_sessions WHERE token_hash=$1`, loginHash[:]).Scan(&loginSessionID); err != nil {
		t.Fatal(err)
	}
	linkRaw := make([]byte, 32)
	for i := range linkRaw {
		linkRaw[i] = 212
	}
	linkToken := base64.RawURLEncoding.EncodeToString(linkRaw)
	linkHash := sha256.Sum256([]byte(linkToken))
	var linkSessionID string
	if err = admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,$2,now(),now()+interval '1 day') RETURNING id::text`, user, linkHash[:]).Scan(&linkSessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `DELETE FROM oidc_identities WHERE issuer=$1 AND subject='known-subject' AND user_id=$2`, fake.Issuer(), user); err != nil {
		t.Fatal(err)
	}
	link := start("new-linked-subject", identity.OIDCPurposeLink, linkSessionID)
	linkResult := callback(link, linkToken)
	if linkResult.Destination != "/account#oidc=connected" || linkResult.SessionToken == "" {
		t.Fatalf("link=%+v", linkResult)
	}
	a, m, s, d = counts()
	if a != beforeA || m != beforeM || s != beforeS+2 || d != beforeD {
		t.Fatalf("link changed admission state: %d/%d/%d/%s", a, m, s, d)
	}
	var initiating, linkedIdentity int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE id=$1`, linkSessionID).Scan(&initiating); err != nil {
		t.Fatal(err)
	}
	if initiating != 0 {
		t.Fatalf("link initiator still present: %d", initiating)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM oidc_identities WHERE issuer=$1 AND subject='new-linked-subject' AND user_id=$2`, fake.Issuer(), user).Scan(&linkedIdentity); err != nil || linkedIdentity != 1 {
		t.Fatalf("linked identity=%d err=%v", linkedIdentity, err)
	}
	_ = initialSession
	_ = loginSessionID
}
