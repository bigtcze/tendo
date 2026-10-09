package postgres

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgconn"
)

func oidcTestFlow(now time.Time, suffix byte) identity.OIDCFlow {
	return identity.OIDCFlow{StateHash: bytes32(suffix), BrowserTokenHash: bytes32(suffix + 1), Issuer: "https://issuer.test", ClientID: "test-client", Nonce: "nonce", PKCEVerifier: "1234567890123456789012345678901234567890123", Purpose: identity.OIDCPurposeLogin, CreatedAt: now, ExpiresAt: now.Add(5 * time.Minute)}
}
func bytes32(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestOIDCPostgresRepositoryAndConstraints(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	repo := New(app, nil)
	flow := oidcTestFlow(now, 1)
	if err := repo.InsertFlow(ctx, flow); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ConsumeFlow(ctx, flow.StateHash, flow.BrowserTokenHash, now, flow.Issuer, flow.ClientID)
	if err != nil || got.Nonce != flow.Nonce || got.PKCEVerifier != flow.PKCEVerifier {
		t.Fatalf("consume=%+v err=%v", got, err)
	}
	if _, err = repo.ConsumeFlow(ctx, flow.StateHash, flow.BrowserTokenHash, now, flow.Issuer, flow.ClientID); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("second consume=%v", err)
	}

	// Build one account for FK-backed constraint and linking cases.
	var uid string
	if err = admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("oidc_user_%d", time.Now().UnixNano())).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	bad := []string{
		`INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES('https://issuer.test','bad sub','` + uid + `',now())`,
		`INSERT INTO oidc_identities(issuer,subject,user_id,created_at) VALUES('https://issuer.test','bad家','` + uid + `',now())`,
		`INSERT INTO oidc_flows(state_hash,browser_token_hash,issuer,client_id,nonce,pkce_verifier,purpose,created_at,expires_at) VALUES(decode(repeat('01',32),'hex'),decode(repeat('02',31),'hex'),'i','c','n','1234567890123456789012345678901234567890123','login',now(),now()+interval '1 minute')`,
		`INSERT INTO oidc_flows(state_hash,browser_token_hash,issuer,client_id,nonce,pkce_verifier,purpose,created_at,expires_at) VALUES(decode(repeat('09',31),'hex'),decode(repeat('10',32),'hex'),'i','c','n','1234567890123456789012345678901234567890123','login',now(),now()+interval '1 minute')`,
		`INSERT INTO oidc_flows(state_hash,browser_token_hash,issuer,client_id,nonce,pkce_verifier,purpose,user_id,session_id,created_at,expires_at) VALUES(decode(repeat('03',32),'hex'),decode(repeat('04',32),'hex'),'i','c','n','1234567890123456789012345678901234567890123','login','` + uid + `',(SELECT id FROM user_sessions LIMIT 1),now(),now()+interval '1 minute')`,
		`INSERT INTO oidc_flows(state_hash,browser_token_hash,issuer,client_id,nonce,pkce_verifier,purpose,created_at,expires_at) VALUES(decode(repeat('05',32),'hex'),decode(repeat('06',32),'hex'),'i','c','n','1234567890123456789012345678901234567890123','link',now(),now()+interval '1 minute')`,
		`INSERT INTO oidc_flows(state_hash,browser_token_hash,issuer,client_id,nonce,pkce_verifier,purpose,created_at,expires_at) VALUES(decode(repeat('07',32),'hex'),decode(repeat('08',32),'hex'),'i','c','n','1234567890123456789012345678901234567890123','login',now(),now()+interval '11 minutes')`,
	}
	for _, stmt := range bad {
		_, err = admin.Exec(ctx, stmt)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" {
			t.Fatalf("constraint err=%v stmt=%s", err, stmt)
		}
	}

	// A wrong browser, issuer, client, or expiry cannot consume a flow.
	for i, args := range []struct {
		browser        []byte
		at             time.Time
		issuer, client string
	}{
		{bytes32(99), now, flow.Issuer, flow.ClientID}, {flow.BrowserTokenHash, now, "wrong", flow.ClientID}, {flow.BrowserTokenHash, now, flow.Issuer, "wrong"}, {flow.BrowserTokenHash, now.Add(6 * time.Minute), flow.Issuer, flow.ClientID},
	} {
		f := oidcTestFlow(now, byte(10+i*2))
		if err = repo.InsertFlow(ctx, f); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.ConsumeFlow(ctx, f.StateHash, args.browser, args.at, args.issuer, args.client); !errors.Is(err, identity.ErrNotFound) {
			t.Fatalf("mismatch consumed: %v", err)
		}
		var n int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM oidc_flows WHERE state_hash=$1`, f.StateHash).Scan(&n); err != nil || n != 1 {
			t.Fatalf("flow removed n=%d err=%v", n, err)
		}
	}

	f := oidcTestFlow(now, 30)
	if err = repo.InsertFlow(ctx, f); err != nil {
		t.Fatal(err)
	}
	f2 := oidcTestFlow(now, 32)
	f2.BrowserTokenHash = f.BrowserTokenHash
	if err = repo.InsertFlow(ctx, f2); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM oidc_flows WHERE browser_token_hash=$1`, f.BrowserTokenHash).Scan(&n); err != nil || n != 1 {
		t.Fatalf("browser replace n=%d err=%v", n, err)
	}
	old := oidcTestFlow(now.Add(-6*time.Minute), 40)
	old.ExpiresAt = now.Add(-time.Minute)
	if err = repo.InsertFlow(ctx, old); err != nil {
		t.Fatal(err)
	}
	fresh := oidcTestFlow(now, 42)
	if err = repo.InsertFlow(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM oidc_flows WHERE state_hash=$1`, old.StateHash).Scan(&n); err != nil || n != 0 {
		t.Fatalf("expired flow remains n=%d err=%v", n, err)
	}

	// Runtime permissions are intentionally asymmetric for identities and flows.
	for _, stmt := range []string{`UPDATE oidc_identities SET subject=subject`, `TRUNCATE oidc_identities`, `DELETE FROM oidc_identities`, `UPDATE oidc_flows SET nonce=nonce`, `TRUNCATE oidc_flows`} {
		_, err = app.Exec(ctx, stmt)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Fatalf("runtime statement %q err=%v", stmt, err)
		}
	}
	if _, err = app.Exec(ctx, `DELETE FROM oidc_flows`); err != nil {
		t.Fatalf("runtime flow delete: %v", err)
	}
}

func TestOIDCPostgresConcurrentConsume(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	repo := New(app, nil)
	now := time.Now().UTC().Truncate(time.Second)
	f := oidcTestFlow(now, 70)
	if err := repo.InsertFlow(ctx, f); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.ConsumeFlow(ctx, f.StateHash, f.BrowserTokenHash, now, f.Issuer, f.ClientID)
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, identity.ErrNotFound) {
				t.Errorf("consume: %v", err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("winners=%d", wins.Load())
	}
	_ = admin
}

func TestOIDCPostgresLinkingAndAdmissionInvariants(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	var user1, user2, householdID, sessionID string
	for i, dest := range []*string{&user1, &user2} {
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("oidc_link_%d_%d", time.Now().UnixNano(), i)).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('oidc invariant','UTC') RETURNING id::text`).Scan(&householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1,$3,'owner'),($2,$3,'member')`, user1, user2, householdID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$3 WHERE id IN ($1,$2)`, user1, user2, householdID); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,decode(repeat('e1',32),'hex'),now(),now()+interval '1 day') RETURNING id::text`, user1).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	repo := New(app, nil)
	invariant := func() string {
		var result string
		err := admin.QueryRow(ctx, `SELECT (SELECT string_agg(user_id::text||':'||household_id::text||':'||role,',' ORDER BY user_id,household_id) FROM household_memberships)||'|'||(SELECT string_agg(id::text||':'||COALESCE(default_household_id::text,'NULL'),',' ORDER BY id) FROM user_accounts)`).Scan(&result)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := invariant()
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := repo.CreateOIDCLoginSession(ctx, identity.NewSession{UserID: user1, TokenHash: bytes32(110), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if invariant() != before {
		t.Fatal("OIDC login changed membership/default-household admission state")
	}
	verified := identity.OIDCVerifiedIdentity{Issuer: "https://issuer.test", Subject: "linked-subject"}
	link := identity.OIDCFlow{UserID: user1, SessionID: sessionID}
	token, err := repo.LinkAndCreateSession(ctx, link, verified, identity.NewSession{UserID: user1, TokenHash: bytes32(112), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, bytes32(1))
	if err != nil || token == "" {
		t.Fatalf("link token=%q err=%v", token, err)
	}
	if invariant() != before {
		t.Fatal("OIDC link changed membership/default-household admission state")
	}
	var count int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM oidc_identities WHERE issuer=$1 AND subject=$2 AND user_id=$3`, verified.Issuer, verified.Subject, user1).Scan(&count); err != nil || count != 1 {
		t.Fatalf("identity count=%d err=%v", count, err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE id=$1::uuid`, sessionID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("initiating session count=%d err=%v", count, err)
	}
	var nextSession string
	if err = admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,decode(repeat('e2',32),'hex'),now(),now()+interval '1 day') RETURNING id::text`, user1).Scan(&nextSession); err != nil {
		t.Fatal(err)
	}
	link.SessionID = nextSession
	if _, err = repo.LinkAndCreateSession(ctx, link, verified, identity.NewSession{UserID: user1, TokenHash: bytes32(114), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, bytes32(2)); err != nil {
		t.Fatalf("idempotent link: %v", err)
	}
	var otherSession string
	if err = admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,decode(repeat('e3',32),'hex'),now(),now()+interval '1 day') RETURNING id::text`, user2).Scan(&otherSession); err != nil {
		t.Fatal(err)
	}
	conflictFlow := identity.OIDCFlow{UserID: user2, SessionID: otherSession}
	if _, err = repo.LinkAndCreateSession(ctx, conflictFlow, verified, identity.NewSession{UserID: user2, TokenHash: bytes32(116), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, bytes32(3)); !errors.Is(err, identity.ErrOIDCConflict) {
		t.Fatalf("identity transfer err=%v", err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE id=$1::uuid`, otherSession).Scan(&count); err != nil || count != 1 {
		t.Fatalf("conflict initiating session count=%d err=%v", count, err)
	}
	var u1session string
	if err = admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,decode(repeat('e4',32),'hex'),now(),now()+interval '1 day') RETURNING id::text`, user1).Scan(&u1session); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.LinkAndCreateSession(ctx, identity.OIDCFlow{UserID: user1, SessionID: u1session}, identity.OIDCVerifiedIdentity{Issuer: verified.Issuer, Subject: "another-subject"}, identity.NewSession{UserID: user1, TokenHash: bytes32(118), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, bytes32(4)); !errors.Is(err, identity.ErrOIDCConflict) {
		t.Fatalf("second subject err=%v", err)
	}
	var rollbackSession string
	if err = admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,decode(repeat('e5',32),'hex'),now(),now()+interval '1 day') RETURNING id::text`, user1).Scan(&rollbackSession); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.LinkAndCreateSession(ctx, identity.OIDCFlow{UserID: user1, SessionID: rollbackSession}, identity.OIDCVerifiedIdentity{Issuer: verified.Issuer, Subject: "rollback-subject"}, identity.NewSession{UserID: user1, TokenHash: bytes32(112), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, bytes32(5)); err == nil {
		t.Fatal("invalid new-session user unexpectedly linked")
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM oidc_identities WHERE subject='rollback-subject'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback identity count=%d err=%v", count, err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM user_sessions WHERE id=$1::uuid`, rollbackSession).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback initiating session count=%d err=%v", count, err)
	}
	var beforeUnknown string
	if err = admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_accounts)||':'||(SELECT count(*) FROM local_credentials)||':'||(SELECT count(*) FROM households)||':'||(SELECT count(*) FROM household_memberships)||':'||(SELECT count(*) FROM user_sessions)||':'||(SELECT count(*) FROM household_invitations)||':'||(SELECT count(*) FROM oidc_identities)||':'||(SELECT count(*) FROM oidc_flows)`).Scan(&beforeUnknown); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.FindIdentity(ctx, verified.Issuer, "missing"); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("unknown identity err=%v", err)
	}
	var afterUnknown string
	if err = admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM user_accounts)||':'||(SELECT count(*) FROM local_credentials)||':'||(SELECT count(*) FROM households)||':'||(SELECT count(*) FROM household_memberships)||':'||(SELECT count(*) FROM user_sessions)||':'||(SELECT count(*) FROM household_invitations)||':'||(SELECT count(*) FROM oidc_identities)||':'||(SELECT count(*) FROM oidc_flows)`).Scan(&afterUnknown); err != nil || afterUnknown != beforeUnknown {
		t.Fatalf("unknown identity wrote rows: before=%s after=%s err=%v", beforeUnknown, afterUnknown, err)
	}
}

func TestOIDCPostgresConcurrentLinkAndDeleteCascade(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	var first, second string
	for i, dest := range []*string{&first, &second} {
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("oidc_race_%d_%d", time.Now().UnixNano(), i)).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	repo := New(app, nil)
	now := time.Now().UTC().Truncate(time.Second)
	identityKey := identity.OIDCVerifiedIdentity{Issuer: "https://issuer.test", Subject: "race-subject"}
	var wg sync.WaitGroup
	var wins atomic.Int32
	errs := make(chan error, 2)
	for i, user := range []string{first, second} {
		wg.Add(1)
		go func(i int, user string) {
			defer wg.Done()
			var session string
			if err := admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,$2,now(),now()+interval '1 day') RETURNING id::text`, user, bytes32(byte(130+i))).Scan(&session); err != nil {
				errs <- err
				return
			}
			_, err := repo.LinkAndCreateSession(ctx, identity.OIDCFlow{UserID: user, SessionID: session}, identityKey, identity.NewSession{UserID: user, TokenHash: bytes32(byte(140 + i)), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, bytes32(byte(150+i)))
			if err == nil {
				wins.Add(1)
			} else if !errors.Is(err, identity.ErrOIDCConflict) {
				errs <- err
			}
		}(i, user)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if wins.Load() != 1 {
		t.Fatalf("concurrent link winners=%d", wins.Load())
	}
	// Deleting the owning account removes both linked identities and outstanding flows.
	var owner string
	if err := admin.QueryRow(ctx, `SELECT user_id::text FROM oidc_identities WHERE issuer=$1 AND subject=$2`, identityKey.Issuer, identityKey.Subject).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	var flowSession string
	if err := admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,decode(repeat('f1',32),'hex'),now(),now()+interval '1 day') RETURNING id::text`, owner).Scan(&flowSession); err != nil {
		t.Fatal(err)
	}
	f := oidcTestFlow(now, 160)
	f.Purpose, f.UserID, f.SessionID = identity.OIDCPurposeLink, owner, flowSession
	if err := repo.InsertFlow(ctx, f); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `DELETE FROM user_accounts WHERE id=$1::uuid`, owner); err != nil {
		t.Fatal(err)
	}
	var identities, flows int
	if err := admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM oidc_identities WHERE user_id=$1::uuid),(SELECT count(*) FROM oidc_flows WHERE state_hash=$2)`, owner, f.StateHash).Scan(&identities, &flows); err != nil || identities != 0 || flows != 0 {
		t.Fatalf("cascade identities=%d flows=%d err=%v", identities, flows, err)
	}
}

func TestOIDCPostgresMigrationUpgrade(t *testing.T) {
	ctx, admin, app := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	var uid, hid, sid string
	login := fmt.Sprintf("oidc_upgrade_%d", time.Now().UnixNano())
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, login).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO local_credentials(user_id,password_hash) VALUES($1,'hash')`, uid); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('oidc upgrade','UTC') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1,$2,'owner')`, uid, hid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `UPDATE user_accounts SET default_household_id=$2 WHERE id=$1`, uid, hid); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_sessions(user_id,token_hash,created_at,expires_at) VALUES($1,decode(repeat('ab',32),'hex'),now(),now()+interval '1 day') RETURNING id::text`, uid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_invitations(household_id,created_by_user_id,creation_key,token_hash,created_at,expires_at) VALUES($2,$1,'upgrade',decode(repeat('cd',32),'hex'),now(),now()+interval '1 day')`, uid, hid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `DROP TABLE IF EXISTS oidc_flows; DROP TABLE IF EXISTS oidc_identities; DELETE FROM tendo_schema_migrations WHERE version=11`); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, admin); err == nil {
		t.Fatal("accepted schema 10")
	}
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM user_accounts u JOIN local_credentials c ON c.user_id=u.id JOIN household_memberships m ON m.user_id=u.id JOIN user_sessions s ON s.user_id=u.id JOIN household_invitations i ON i.created_by_user_id=u.id WHERE u.id=$1`, uid).Scan(&count); err != nil || count != 1 {
		t.Fatalf("preservation count=%d err=%v", count, err)
	}
	var version int
	var dirty bool
	if err := admin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&version, &dirty); err != nil || version != 11 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", version, dirty, err)
	}
	for _, table := range []string{"oidc_identities", "oidc_flows"} {
		if _, err := app.Exec(ctx, "SELECT * FROM "+table+" LIMIT 0"); err != nil {
			t.Fatalf("runtime SELECT %s: %v", table, err)
		}
	}
	_ = sid
}
