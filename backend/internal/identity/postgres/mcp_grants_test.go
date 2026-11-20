package postgres

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	mcpTestResource    = "https://tendo.test/mcp"
	mcpTestClientID    = "mcp_test_client_0001"
	mcpTestRedirectURI = "https://client.test/callback"
)

// mcpTestVerifier is a PKCE verifier; its S256 challenge is derived below.
var mcpTestVerifier = "mcp_verifier_" + strings.Repeat("x", 40)

// mcpTestChallenge is BASE64URL(SHA256(verifier)) per RFC 7636.
func mcpTestChallenge() string {
	sum := sha256Sum(mcpTestVerifier)
	return base64.RawURLEncoding.EncodeToString(sum)
}

// mcpClock is a settable clock shared by the service and the fixtures.
type mcpClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *mcpClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *mcpClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// mcpEnv is one isolated database with a registered client and an MCP service
// wired to the runtime pool. Admin writes bypass the runtime role's privileges.
type mcpEnv struct {
	ctx      context.Context
	admin    *pgxpool.Pool
	app      *pgxpool.Pool
	observer *pgxpool.Pool
	clock    *mcpClock
	svc      *identity.MCPService
}

func newMCPEnv(t *testing.T) *mcpEnv {
	t.Helper()
	ctx, admin, app, observer := invitationPools(t)
	if _, err := admin.Exec(ctx, `TRUNCATE mcp_clients, user_accounts, household_memberships, households CASCADE`); err != nil {
		t.Fatal(err)
	}
	return bindMCPEnv(t, ctx, admin, app, observer)
}

// bindMCPEnv registers the test client (idempotently) and builds the MCP service
// over the runtime pool. Fixtures that truncate user_accounts call it afterwards.
func bindMCPEnv(t *testing.T, ctx context.Context, admin, app, observer *pgxpool.Pool) *mcpEnv {
	t.Helper()
	if _, err := admin.Exec(ctx, `INSERT INTO mcp_clients(client_id, client_name, redirect_uris, created_at)
		VALUES($1, 'Test MCP client', ARRAY[$2]::text[], now()) ON CONFLICT (client_id) DO NOTHING`, mcpTestClientID, mcpTestRedirectURI); err != nil {
		t.Fatal(err)
	}
	clock := &mcpClock{at: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	svc, err := identity.NewMCPService(NewMCPRepository(app), identity.MCPOptions{Enabled: true, Resource: mcpTestResource}, clock.now)
	if err != nil {
		t.Fatal(err)
	}
	return &mcpEnv{ctx: ctx, admin: admin, app: app, observer: observer, clock: clock, svc: svc}
}

func randomMCPSecret(t *testing.T) string {
	t.Helper()
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

// newAccount creates a live account with a local credential and a password
// session that is valid for a day. It returns the account ID and the session
// token plaintext.
func (e *mcpEnv) newAccount(t *testing.T, login string) (string, string) {
	t.Helper()
	user := newLiveAccount(t, e.ctx, e.admin, login)
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO local_credentials(user_id, password_hash) VALUES($1::uuid, 'hash:mcp-test')`, user); err != nil {
		t.Fatal(err)
	}
	session := randomMCPSecret(t)
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO user_sessions(user_id, token_hash, created_at, expires_at, sign_in_method)
		VALUES($1::uuid, $2, $3, $4, 'password')`, user, sha256Sum(session), e.clock.now(), e.clock.now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	return user, session
}

func (e *mcpEnv) issue(t *testing.T, user, session string) string {
	t.Helper()
	issued, err := e.svc.IssueAuthorizationCode(e.ctx, identity.MCPAuthorization{
		Principal:           identity.Principal{UserID: user},
		SessionToken:        session,
		ClientID:            mcpTestClientID,
		RedirectURI:         mcpTestRedirectURI,
		CodeChallenge:       mcpTestChallenge(),
		CodeChallengeMethod: "S256",
		Resource:            mcpTestResource,
		Scopes:              []string{identity.MCPScopeRead, identity.MCPScopeWrite},
	})
	if err != nil {
		t.Fatalf("issue authorization code: %v", err)
	}
	return issued.Code
}

func (e *mcpEnv) exchange(code string) (identity.MCPTokens, error) {
	return e.svc.ExchangeCode(e.ctx, identity.MCPCodeExchange{
		Code: code, CodeVerifier: mcpTestVerifier, ClientID: mcpTestClientID,
		RedirectURI: mcpTestRedirectURI, Resource: mcpTestResource,
	})
}

func (e *mcpEnv) refresh(token string) (identity.MCPTokens, error) {
	return e.svc.Refresh(e.ctx, identity.MCPRefresh{RefreshToken: token, ClientID: mcpTestClientID, Resource: mcpTestResource})
}

// mcpCounts reports the secret rows still stored for one account.
type mcpCounts struct{ codes, access, refresh int }

func (e *mcpEnv) counts(t *testing.T, user string) mcpCounts {
	t.Helper()
	var c mcpCounts
	if err := e.admin.QueryRow(e.ctx, `SELECT
		(SELECT count(*) FROM mcp_authorization_codes WHERE user_id=$1::uuid),
		(SELECT count(*) FROM mcp_access_tokens a JOIN mcp_grants g ON g.id=a.grant_id WHERE g.user_id=$1::uuid),
		(SELECT count(*) FROM mcp_refresh_tokens r JOIN mcp_grants g ON g.id=r.grant_id WHERE g.user_id=$1::uuid)`,
		user).Scan(&c.codes, &c.access, &c.refresh); err != nil {
		t.Fatal(err)
	}
	return c
}

// grantState is the secret-free grant row plus its audit events.
type grantState struct {
	id, revokeReason string
	revoked          bool
	events           []string // "event:reason:actor" in occurred order
}

func (e *mcpEnv) grantOf(t *testing.T, user string) grantState {
	t.Helper()
	var g grantState
	var reason *string
	if err := e.admin.QueryRow(e.ctx, `SELECT id::text, revoked_at IS NOT NULL, revoke_reason FROM mcp_grants WHERE user_id=$1::uuid`, user).
		Scan(&g.id, &g.revoked, &reason); err != nil {
		t.Fatalf("grant of %s: %v", user, err)
	}
	if reason != nil {
		g.revokeReason = *reason
	}
	rows, err := e.admin.Query(e.ctx, `SELECT event, COALESCE(reason,''), COALESCE(actor_user_id::text,'') FROM mcp_grant_events WHERE grant_id=$1::uuid ORDER BY occurred_at, event`, g.id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var event, r, a string
		if err = rows.Scan(&event, &r, &a); err != nil {
			t.Fatal(err)
		}
		g.events = append(g.events, fmt.Sprintf("%s:%s:%s", event, r, a))
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return g
}

// requireNoMCPSecrets fails unless the account has zero code, access and refresh rows.
func (e *mcpEnv) requireNoMCPSecrets(t *testing.T, user string) {
	t.Helper()
	if c := e.counts(t, user); c != (mcpCounts{}) {
		t.Fatalf("surviving MCP secret rows for %s: %+v", user, c)
	}
}

// TestMCPHappyPathEndToEnd covers issue, exchange, verify, rotation, the
// still-valid old access token, the created audit event, and the digest-only
// storage invariant (no plaintext secret in any MCP table).
func TestMCPHappyPathEndToEnd(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_happy")

	code := env.issue(t, user, session)
	first, err := env.exchange(code)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	access, err := env.svc.VerifyAccessToken(env.ctx, first.AccessToken)
	if err != nil || access.UserID != user || access.ClientID != mcpTestClientID {
		t.Fatalf("verify first access token: access=%+v err=%v", access, err)
	}
	if len(access.Scopes) != 2 || access.Scopes[0] != identity.MCPScopeRead || access.Scopes[1] != identity.MCPScopeWrite {
		t.Fatalf("scopes = %v", access.Scopes)
	}

	rotated, err := env.refresh(first.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if rotated.AccessToken == first.AccessToken || rotated.RefreshToken == first.RefreshToken {
		t.Fatal("refresh did not rotate both tokens")
	}
	if _, err = env.svc.VerifyAccessToken(env.ctx, rotated.AccessToken); err != nil {
		t.Fatalf("verify rotated access token: %v", err)
	}
	// The access token issued before the rotation stays valid until its own expiry.
	if _, err = env.svc.VerifyAccessToken(env.ctx, first.AccessToken); err != nil {
		t.Fatalf("old access token before its expiry: %v", err)
	}

	grant := env.grantOf(t, user)
	if grant.revoked || len(grant.events) != 1 || grant.events[0] != "created::"+user {
		t.Fatalf("grant=%+v, want one created event with actor=user", grant)
	}
	if c := env.counts(t, user); c.codes != 1 || c.access != 2 || c.refresh != 2 {
		// The used code row is kept (it names the grant); consumed refresh digests
		// are kept until the grant is revoked.
		t.Fatalf("secret rows = %+v, want codes=1 access=2 refresh=2", c)
	}

	// Digest-only storage: each secret is stored as its SHA-256 digest only.
	checks := []struct {
		name, query, secret string
	}{
		{"code", `SELECT count(*) FROM mcp_authorization_codes WHERE code_hash=$1 AND octet_length(code_hash)=32`, code},
		{"access", `SELECT count(*) FROM mcp_access_tokens WHERE token_hash=$1`, rotated.AccessToken},
		{"refresh", `SELECT count(*) FROM mcp_refresh_tokens WHERE token_hash=$1`, rotated.RefreshToken},
	}
	for _, check := range checks {
		var n int
		if err = env.admin.QueryRow(env.ctx, check.query, sha256Sum(check.secret)).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s digest stored count=%d err=%v", check.name, n, err)
		}
	}
	plaintexts := []string{code, first.AccessToken, first.RefreshToken, rotated.AccessToken, rotated.RefreshToken, session}
	for _, table := range []string{"mcp_clients", "mcp_grants", "mcp_authorization_codes", "mcp_access_tokens", "mcp_refresh_tokens", "mcp_grant_events"} {
		for _, secret := range plaintexts {
			var n int
			query := fmt.Sprintf(`SELECT count(*) FROM %s t WHERE position($1::bytea in convert_to(t::text, 'UTF8')) > 0`, table)
			if err = env.admin.QueryRow(env.ctx, query, []byte(secret)).Scan(&n); err != nil || n != 0 {
				t.Fatalf("plaintext secret found in %s: count=%d err=%v", table, n, err)
			}
		}
	}
}

// freshAdminConn opens a connection outside the test pools, so a committed
// result is observed from a session that shares no state with the writer.
func freshAdminConn(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, os.Getenv("TEST_DATABASE_ADMIN_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// TestMCPCodeReuseRevokesGrantAndDeletesEverySecret: a second presentation of a
// used code fails as invalid_grant, revokes the grant it issued (code_reuse),
// and leaves zero code, access or refresh rows, as seen from a fresh connection.
func TestMCPCodeReuseRevokesGrantAndDeletesEverySecret(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_reuse")
	code := env.issue(t, user, session)
	first, err := env.exchange(code)
	if err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if _, err = env.exchange(code); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("code reuse err=%v, want ErrMCPInvalidGrant", err)
	}

	conn := freshAdminConn(t, env.ctx)
	var revoked bool
	var reason string
	if err = conn.QueryRow(env.ctx, `SELECT revoked_at IS NOT NULL, revoke_reason FROM mcp_grants WHERE user_id=$1::uuid`, user).Scan(&revoked, &reason); err != nil {
		t.Fatal(err)
	}
	if !revoked || reason != "code_reuse" {
		t.Fatalf("grant revoked=%v reason=%q, want code_reuse", revoked, reason)
	}
	var codes, access, refresh int
	if err = conn.QueryRow(env.ctx, `SELECT
		(SELECT count(*) FROM mcp_authorization_codes WHERE user_id=$1::uuid),
		(SELECT count(*) FROM mcp_access_tokens a JOIN mcp_grants g ON g.id=a.grant_id WHERE g.user_id=$1::uuid),
		(SELECT count(*) FROM mcp_refresh_tokens r JOIN mcp_grants g ON g.id=r.grant_id WHERE g.user_id=$1::uuid)`,
		user).Scan(&codes, &access, &refresh); err != nil {
		t.Fatal(err)
	}
	if codes != 0 || access != 0 || refresh != 0 {
		t.Fatalf("surviving rows codes=%d access=%d refresh=%d", codes, access, refresh)
	}
	var revokedEvents int
	if err = conn.QueryRow(env.ctx, `SELECT count(*) FROM mcp_grant_events e JOIN mcp_grants g ON g.id=e.grant_id
		WHERE g.user_id=$1::uuid AND e.event='revoked' AND e.reason='code_reuse' AND e.actor_user_id IS NULL`, user).Scan(&revokedEvents); err != nil || revokedEvents != 1 {
		t.Fatalf("revoked events=%d err=%v", revokedEvents, err)
	}
	if _, err = env.svc.VerifyAccessToken(env.ctx, first.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("access token of reused grant err=%v", err)
	}
}

// TestMCPRefreshReplayRevokesGrantAndDeletesEverySecret: presenting a consumed
// refresh token revokes the grant (replay), and the newest refresh token fails
// too; no token row survives.
func TestMCPRefreshReplayRevokesGrantAndDeletesEverySecret(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_replay")
	first, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	env.clock.advance(time.Minute)
	second, err := env.refresh(first.RefreshToken)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	if _, err = env.refresh(first.RefreshToken); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("replayed refresh err=%v, want ErrMCPInvalidGrant", err)
	}
	if _, err = env.refresh(second.RefreshToken); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("newest refresh after replay err=%v, want ErrMCPInvalidGrant", err)
	}
	grant := env.grantOf(t, user)
	if !grant.revoked || grant.revokeReason != "replay" {
		t.Fatalf("grant=%+v, want revoked replay", grant)
	}
	env.requireNoMCPSecrets(t, user)
	if _, err = env.svc.VerifyAccessToken(env.ctx, second.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("access token after replay err=%v", err)
	}
}

// TestMCPVerifyRefusesRevokedExpiredDeletedAndDisabled: an access token is
// refused when its grant is revoked, when it has expired, when its account is
// tombstoned (the read path alone, with the rows still present), and whenever
// the service is disabled.
func TestMCPVerifyRefusesRevokedExpiredDeletedAndDisabled(t *testing.T) {
	t.Run("revoked grant", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_verify_revoked")
		tokens, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); err != nil {
			t.Fatalf("live token: %v", err)
		}
		if _, err = env.admin.Exec(env.ctx, `UPDATE mcp_grants SET revoked_at=now(), revoke_reason='user' WHERE user_id=$1::uuid`, user); err != nil {
			t.Fatal(err)
		}
		if _, err = env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
			t.Fatalf("revoked grant err=%v", err)
		}
	})

	t.Run("expired access token", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_verify_expired")
		tokens, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatal(err)
		}
		env.clock.advance(identity.MCPAccessTokenLifetime + time.Second)
		if _, err = env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
			t.Fatalf("expired access token err=%v", err)
		}
	})

	t.Run("tombstoned account with rows still present", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_verify_deleted")
		tokens, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatal(err)
		}
		tombstoneAccount(t, env.ctx, env.admin, user, tombstoneOptions{keepCredentials: true, keepSessions: true, keepOIDCIdentities: true, keepInstanceAdmin: true})
		if _, err = env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
			t.Fatalf("tombstoned account err=%v", err)
		}
	})

	t.Run("disabled service", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_verify_disabled")
		tokens, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatal(err)
		}
		disabled, err := identity.NewMCPService(NewMCPRepository(env.app), identity.MCPOptions{Enabled: false}, env.clock.now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = disabled.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPDisabled) {
			t.Fatalf("disabled verify err=%v", err)
		}
	})
}

// newActor creates the signed-in administrator who performs a deletion or reset.
func (e *mcpEnv) newActor(t *testing.T) string {
	t.Helper()
	return newLiveAccount(t, e.ctx, e.admin, fmt.Sprintf("mcp_actor_%d", time.Now().UnixNano()))
}

// eraseInTx runs the deletion's identity writes in the caller's transaction,
// in the order the household deletion uses: access first, then the tombstone.
func eraseInTx(ctx context.Context, tx pgx.Tx, user, actor string, now time.Time) error {
	eraser := NewAccountEraser(tx)
	if err := eraser.EraseAccountAccess(ctx, user, actor, now); err != nil {
		return err
	}
	return eraser.AnonymizeAccount(ctx, user, now)
}

// countGrants reports the grant rows of one account, revoked or not.
func (e *mcpEnv) countGrants(t *testing.T, user string) int {
	t.Helper()
	var n int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM mcp_grants WHERE user_id=$1::uuid`, user).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// requireDeletedGrant asserts the single grant of a deleted account is revoked
// with the account_deletion cause, attributed to the deleting administrator.
func (e *mcpEnv) requireDeletedGrant(t *testing.T, user, actor string) {
	t.Helper()
	grant := e.grantOf(t, user)
	if !grant.revoked || grant.revokeReason != "account_deletion" {
		t.Fatalf("grant=%+v, want revoked account_deletion", grant)
	}
	var revokedByActor int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM mcp_grant_events WHERE grant_id=$1::uuid AND event='revoked' AND reason='account_deletion' AND actor_user_id=$2::uuid`,
		grant.id, actor).Scan(&revokedByActor); err != nil || revokedByActor != 1 {
		t.Fatalf("account_deletion revocation events by actor=%d err=%v", revokedByActor, err)
	}
}

// TestMCPRaceAccountDeletionAndCodeExchange pins the ADR 0019 lock order for
// the exchange: the deletion holds the account row, the exchange blocks on its
// FOR SHARE, and after the tombstone commits the exchange must refuse without
// leaving a grant or token row behind.
func TestMCPRaceAccountDeletionAndCodeExchange(t *testing.T) {
	t.Run("deletion holds the account row first", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_race_delete_exchange")
		actor := env.newActor(t)
		code := env.issue(t, user, session)

		tx, err := env.admin.Begin(env.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err = NewAccountEraser(tx).LockDeletableAccount(env.ctx, user); err != nil {
			t.Fatal(err)
		}

		type outcome struct {
			tokens identity.MCPTokens
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			tokens, err := env.exchange(code)
			done <- outcome{tokens, err}
		}()
		waitForBlockedBackend(t, env.ctx, env.observer)
		if err = eraseInTx(env.ctx, tx, user, actor, env.clock.now()); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(env.ctx); err != nil {
			t.Fatal(err)
		}
		got := <-done
		if !errors.Is(got.err, identity.ErrMCPInvalidGrant) {
			t.Fatalf("exchange racing a deletion: tokens=%+v err=%v, want ErrMCPInvalidGrant", got.tokens, got.err)
		}
		env.requireNoMCPSecrets(t, user)
		if n := env.countGrants(t, user); n != 0 {
			t.Fatalf("grants created for a deleted account: %d", n)
		}
	})

	t.Run("exchange commits first and the deletion revokes it", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_race_exchange_delete")
		actor := env.newActor(t)
		if _, err := env.exchange(env.issue(t, user, session)); err != nil {
			t.Fatalf("exchange: %v", err)
		}
		tx, err := env.admin.Begin(env.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err = NewAccountEraser(tx).LockDeletableAccount(env.ctx, user); err != nil {
			t.Fatal(err)
		}
		if err = eraseInTx(env.ctx, tx, user, actor, env.clock.now()); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(env.ctx); err != nil {
			t.Fatal(err)
		}
		env.requireNoMCPSecrets(t, user)
		env.requireDeletedGrant(t, user, actor)
	})
}

// TestMCPRaceAccountDeletionAndRefresh is the same shape for refresh: the
// deletion holds the account row, the refresh blocks on its share lock, and the
// refresh must refuse after the tombstone commits with zero token rows left.
func TestMCPRaceAccountDeletionAndRefresh(t *testing.T) {
	t.Run("deletion holds the account row first", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_race_delete_refresh")
		actor := env.newActor(t)
		first, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}

		tx, err := env.admin.Begin(env.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err = NewAccountEraser(tx).LockDeletableAccount(env.ctx, user); err != nil {
			t.Fatal(err)
		}

		type outcome struct {
			tokens identity.MCPTokens
			err    error
		}
		done := make(chan outcome, 1)
		go func() {
			tokens, err := env.refresh(first.RefreshToken)
			done <- outcome{tokens, err}
		}()
		waitForBlockedBackend(t, env.ctx, env.observer)
		if err = eraseInTx(env.ctx, tx, user, actor, env.clock.now()); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(env.ctx); err != nil {
			t.Fatal(err)
		}
		got := <-done
		if !errors.Is(got.err, identity.ErrMCPInvalidGrant) {
			t.Fatalf("refresh racing a deletion: tokens=%+v err=%v, want ErrMCPInvalidGrant", got.tokens, got.err)
		}
		env.requireNoMCPSecrets(t, user)
		env.requireDeletedGrant(t, user, actor)
	})
}

// startPasswordReplacement runs the shared credential replacement in a
// goroutine: it locks local_credentials FOR UPDATE, replaces the password and
// revokes MCP access, then parks until release is closed and only then commits.
// locked closes once the lock is held and the revocation is written.
func (e *mcpEnv) startPasswordReplacement(user string, revocation identity.MCPRevocation) (<-chan struct{}, chan<- struct{}, <-chan error) {
	locked, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		repo := NewPasswordChangeRepository(e.app)
		done <- repo.WithCredentialReplacement(e.ctx, func(tx identity.CredentialReplacementTransaction) error {
			if _, err := tx.LockLocalCredential(e.ctx, user); err != nil {
				return err
			}
			if err := tx.RevokeOutstandingPasswordResets(e.ctx, user, e.clock.now()); err != nil {
				return err
			}
			if _, err := tx.ReplaceLocalPassword(e.ctx, user, "hash:mcp-replaced", e.clock.now(), revocation); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	return locked, release, done
}

// awaitLocked waits until the paused replacement holds its lock, failing fast if
// the replacement returned an error before reaching that point.
func awaitLocked(t *testing.T, locked <-chan struct{}, done <-chan error) {
	t.Helper()
	select {
	case <-locked:
	case err := <-done:
		t.Fatalf("password replacement failed before the pause: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("password replacement never reached its pause")
	}
}

// tryIssue is issue without the test failure, for a racing goroutine.
func (e *mcpEnv) tryIssue(user, session string) (identity.MCPIssuedCode, error) {
	return e.svc.IssueAuthorizationCode(e.ctx, identity.MCPAuthorization{
		Principal:           identity.Principal{UserID: user},
		SessionToken:        session,
		ClientID:            mcpTestClientID,
		RedirectURI:         mcpTestRedirectURI,
		CodeChallenge:       mcpTestChallenge(),
		CodeChallengeMethod: "S256",
		Resource:            mcpTestResource,
		Scopes:              []string{identity.MCPScopeRead},
	})
}

// TestMCPRaceCredentialReplacementAndRefresh: a refresh that reaches the
// credential share lock while a password replacement holds it must refuse after
// the replacement commits, and the grant must be revoked password_change with
// zero token rows. The other order (refresh commits first) is also checked: the
// replacement then revokes the freshly rotated tokens.
func TestMCPRaceCredentialReplacementAndRefresh(t *testing.T) {
	t.Run("replacement holds the credential row first", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_race_pw_refresh")
		first, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		locked, release, done := env.startPasswordReplacement(user, identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: user})
		awaitLocked(t, locked, done)

		type outcome struct {
			tokens identity.MCPTokens
			err    error
		}
		refreshed := make(chan outcome, 1)
		go func() {
			tokens, err := env.refresh(first.RefreshToken)
			refreshed <- outcome{tokens, err}
		}()
		waitForBlockedBackend(t, env.ctx, env.observer)
		close(release)
		if err = <-done; err != nil {
			t.Fatalf("password replacement commit: %v", err)
		}
		got := <-refreshed
		if !errors.Is(got.err, identity.ErrMCPInvalidGrant) {
			t.Fatalf("refresh racing a password change: tokens=%+v err=%v, want ErrMCPInvalidGrant", got.tokens, got.err)
		}
		env.requireNoMCPSecrets(t, user)
		if grant := env.grantOf(t, user); !grant.revoked || grant.revokeReason != "password_change" {
			t.Fatalf("grant=%+v, want revoked password_change", grant)
		}
	})

	t.Run("refresh commits first and the replacement revokes the rotated tokens", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_race_refresh_pw")
		first, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		if _, err = env.refresh(first.RefreshToken); err != nil {
			t.Fatalf("refresh before the password change: %v", err)
		}
		repo := NewPasswordChangeRepository(env.app)
		if err = repo.WithCredentialReplacement(env.ctx, func(tx identity.CredentialReplacementTransaction) error {
			if _, e := tx.LockLocalCredential(env.ctx, user); e != nil {
				return e
			}
			_, e := tx.ReplaceLocalPassword(env.ctx, user, "hash:mcp-replaced", env.clock.now(),
				identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: user})
			return e
		}); err != nil {
			t.Fatalf("password replacement: %v", err)
		}
		env.requireNoMCPSecrets(t, user)
		if grant := env.grantOf(t, user); !grant.revoked || grant.revokeReason != "password_change" {
			t.Fatalf("grant=%+v, want revoked password_change", grant)
		}
	})
}

// TestMCPRaceCredentialReplacementAndIssue: the password change commits before
// the consent issues a code. The issue must see its session gone and fail with
// ErrUnauthenticated, leaving no code row.
func TestMCPRaceCredentialReplacementAndIssue(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_race_pw_issue")
	locked, release, done := env.startPasswordReplacement(user, identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: user})
	awaitLocked(t, locked, done)

	issued := make(chan error, 1)
	go func() {
		_, err := env.tryIssue(user, session)
		issued <- err
	}()
	waitForBlockedBackend(t, env.ctx, env.observer)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("password replacement commit: %v", err)
	}
	if err := <-issued; !errors.Is(err, identity.ErrUnauthenticated) {
		t.Fatalf("issue after the password change err=%v, want ErrUnauthenticated", err)
	}
	if c := env.counts(t, user); c != (mcpCounts{}) {
		t.Fatalf("code issued after the password change: %+v", c)
	}
}

// waitForBlockedStatement waits until a backend is blocked on a lock while
// running a statement matching pattern. Filtering on the statement text makes
// the wait specific to the intended competitor, not any lock waiter.
func waitForBlockedStatement(t *testing.T, ctx context.Context, observer *pgxpool.Pool, pattern string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := observer.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid() AND query ILIKE $1`, pattern).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no backend blocked on a lock running %q", pattern)
}

// sessionFor inserts a password session for an existing account.
func (e *mcpEnv) sessionFor(t *testing.T, user string) string {
	t.Helper()
	session := randomMCPSecret(t)
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO user_sessions(user_id, token_hash, created_at, expires_at, sign_in_method)
		VALUES($1::uuid, $2, $3, $4, 'password')`, user, sha256Sum(session), e.clock.now(), e.clock.now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	return session
}

// waitForLockWaiters waits until at least n backends are blocked on a lock, so
// the test can queue competitors deterministically.
func waitForLockWaiters(t *testing.T, ctx context.Context, observer *pgxpool.Pool, pattern string, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := observer.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid() AND query ILIKE $1`, pattern).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("fewer than %d backends blocked on locks", n)
}

// TestMCPRaceAdminResetRedemptionAndRefresh: an admin reset redemption holds the
// credential row while a refresh queues behind it on the credential share lock.
// Whichever order the locks resolve in, the final state must be a grant revoked
// admin_reset with the issuing admin as actor and zero token rows.
func TestMCPRaceAdminResetRedemptionAndRefresh(t *testing.T) {
	env := newMCPEnv(t)
	owner, target, house := passwordResetFixture(t, env.ctx, env.admin)
	resets := newPasswordResetService(t, env.app, &passwordResetTestClock{at: env.clock.now()})
	first, err := env.exchange(env.issue(t, target, env.sessionFor(t, target)))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	issued, err := resets.IssuePasswordReset(env.ctx, owner, house, target)
	if err != nil {
		t.Fatalf("issue reset: %v", err)
	}

	pause, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pause.Rollback(context.Background()) }()
	if _, err = pause.Exec(env.ctx, `SELECT 1 FROM local_credentials WHERE user_id=$1::uuid FOR UPDATE`, target); err != nil {
		t.Fatal(err)
	}

	redeemed := make(chan error, 1)
	go func() {
		_, err := resets.RedeemPasswordReset(env.ctx, issued.Token, "a brand new long password")
		redeemed <- err
	}()
	waitForLockWaiters(t, env.ctx, env.observer, "%FOR UPDATE%", 1)
	refreshed := make(chan error, 1)
	go func() {
		_, err := env.refresh(first.RefreshToken)
		refreshed <- err
	}()
	waitForLockWaiters(t, env.ctx, env.observer, "%FOR SHARE%", 1)
	if err = pause.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-redeemed; err != nil {
		t.Fatalf("redemption: %v", err)
	}
	// Redemption queued on the credential lock first, so it wins and the refresh
	// must be refused. A successful refresh here would mean the lock queue order
	// is not what the protocol assumes.
	if err = <-refreshed; !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("refresh queued behind a reset redemption: err=%v, want ErrMCPInvalidGrant", err)
	}
	env.requireNoMCPSecrets(t, target)
	env.requireGrantRevoked(t, target, "admin_reset", owner)
}

// requireGrantRevoked asserts the account's grant is revoked with a reason and,
// when actor is set, that the revocation event names that actor.
func (e *mcpEnv) requireGrantRevoked(t *testing.T, user, reason, actor string) {
	t.Helper()
	grant := e.grantOf(t, user)
	if !grant.revoked || grant.revokeReason != reason {
		t.Fatalf("grant=%+v, want revoked %s", grant, reason)
	}
	var n int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM mcp_grant_events WHERE grant_id=$1::uuid AND event='revoked' AND reason=$2 AND actor_user_id IS NOT DISTINCT FROM NULLIF($3,'')::uuid`,
		grant.id, reason, actor).Scan(&n); err != nil || n != 1 {
		t.Fatalf("revoked %s events with actor %q = %d, err=%v", reason, actor, n, err)
	}
}

// TestMCPConcurrentRefreshReplayExactlyOneWinner races two refreshes of one
// refresh token, twenty times. Exactly one may succeed; the loser must get
// invalid_grant, the grant ends revoked (replay), and no token row survives.
// Any SQLSTATE 40P01 surfaces as a non-invalid_grant error and fails the test.
func TestMCPConcurrentRefreshReplayExactlyOneWinner(t *testing.T) {
	env := newMCPEnv(t)
	for i := 0; i < 20; i++ {
		user, session := env.newAccount(t, fmt.Sprintf("mcp_replay_race_%02d", i))
		first, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatalf("iteration %d exchange: %v", i, err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		for j := range errs {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				<-start
				_, errs[j] = env.refresh(first.RefreshToken)
			}(j)
		}
		close(start)
		wg.Wait()
		wins, losses := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				wins++
			case errors.Is(err, identity.ErrMCPInvalidGrant):
				losses++
			default:
				t.Fatalf("iteration %d: unexpected refresh error (deadlock or infrastructure?): %v", i, err)
			}
		}
		if wins != 1 || losses != 1 {
			t.Fatalf("iteration %d: wins=%d losses=%d, want 1 and 1", i, wins, losses)
		}
		env.requireGrantRevoked(t, user, "replay", "")
		env.requireNoMCPSecrets(t, user)
	}
}

// issuancePrefix holds the locks an issuance takes before its inserts: the live
// account FOR SHARE, then the credential row FOR SHARE, as the production
// LockLiveAccount does. It returns the still-open transaction.
func issuancePrefix(t *testing.T, ctx context.Context, app *pgxpool.Pool, user string) pgx.Tx {
	t.Helper()
	tx, err := app.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	q := identitydb.New(tx)
	var id pgtype.UUID
	if err = id.Scan(user); err != nil {
		t.Fatal(err)
	}
	if _, err = q.LockLiveMCPAccount(ctx, id); err != nil {
		t.Fatalf("share live account: %v", err)
	}
	if _, err = q.ShareLocalCredential(ctx, id); err != nil {
		t.Fatalf("share credential: %v", err)
	}
	return tx
}

// TestMCPReverseRaceDeletionWaitsForIssuance: an issuance holds the account FOR
// SHARE and has written a grant, its creation event, an access token, a
// refresh token and the used code. A deletion that arrives meanwhile must wait,
// then erase everything the issuance committed: zero MCP rows, the grant
// revoked account_deletion, and the issued access token refused.
func TestMCPReverseRaceDeletionWaitsForIssuance(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_reverse_delete_issue")
	actor := env.newActor(t)
	code := env.issue(t, user, session)

	// The issuance prefix: the account and credential share locks, then the
	// production statements of the exchange's writes, in one open transaction S.
	s := issuancePrefix(t, env.ctx, env.app, user)
	defer func() { _ = s.Rollback(context.Background()) }()
	q := identitydb.New(s)
	var userID pgtype.UUID
	if err := userID.Scan(user); err != nil {
		t.Fatal(err)
	}
	now := env.clock.now()
	codeHash := sha256Sum(code)
	access, refresh := randomMCPSecret(t), randomMCPSecret(t)
	grantID, err := q.CreateMCPGrant(env.ctx, identitydb.CreateMCPGrantParams{
		UserID: userID, ClientID: mcpTestClientID, Scopes: []string{identity.MCPScopeRead, identity.MCPScopeWrite},
		Resource: mcpTestResource, CreatedAt: ts(now), ExpiresAt: ts(now.Add(identity.MCPGrantLifetime)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = q.RecordMCPGrantCreated(env.ctx, grantID); err != nil {
		t.Fatal(err)
	}
	if _, err = q.MarkMCPAuthorizationCodeUsed(env.ctx, identitydb.MarkMCPAuthorizationCodeUsedParams{
		CodeHash: codeHash, UsedAt: ts(now), IssuedGrantID: grantID,
	}); err != nil {
		t.Fatal(err)
	}
	if err = q.CreateMCPAccessToken(env.ctx, identitydb.CreateMCPAccessTokenParams{
		TokenHash: sha256Sum(access), GrantID: grantID, CreatedAt: ts(now), ExpiresAt: ts(now.Add(identity.MCPAccessTokenLifetime)),
	}); err != nil {
		t.Fatal(err)
	}
	if err = q.CreateMCPRefreshToken(env.ctx, identitydb.CreateMCPRefreshTokenParams{
		TokenHash: sha256Sum(refresh), GrantID: grantID, CreatedAt: ts(now), ExpiresAt: ts(now.Add(identity.MCPRefreshIdleLifetime)),
	}); err != nil {
		t.Fatal(err)
	}

	deletion := env.startDeletion(user, actor)
	env.awaitBlocked(t, deletion)
	if err = s.Commit(env.ctx); err != nil {
		t.Fatalf("issuance commit: %v", err)
	}
	<-deletion.finished
	if deletion.err != nil {
		t.Fatalf("deletion after the issuance committed: %v", deletion.err)
	}

	env.requireNoMCPSecrets(t, user)
	env.requireDeletedGrant(t, user, actor)
	if _, err = env.svc.VerifyAccessToken(env.ctx, access); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("access token issued before the deletion err=%v, want ErrMCPInvalidToken", err)
	}
	if _, err = env.refresh(refresh); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("refresh token issued before the deletion err=%v, want ErrMCPInvalidGrant", err)
	}
}

// refreshPrefix runs the production refresh writes in an open transaction S
// that holds the account and credential FOR SHARE locks: the unlocked owner
// pre-read, the grant's code locks, the grant row FOR UPDATE, the presented
// token's row FOR UPDATE, then the consume and the rotated pair. It returns the
// open transaction and the rotated access and refresh secrets.
func refreshPrefix(t *testing.T, ctx context.Context, env *mcpEnv, user, refreshToken string) (pgx.Tx, string, string) {
	t.Helper()
	refreshHash := sha256Sum(refreshToken)
	pre, err := identitydb.New(env.app).FindMCPRefreshTokenOwner(ctx, refreshHash)
	if err != nil {
		t.Fatalf("refresh owner pre-read: %v", err)
	}
	if pre.UserID.String() != user {
		t.Fatalf("refresh owner=%s, want %s", pre.UserID.String(), user)
	}
	s := issuancePrefix(t, ctx, env.app, user)
	q := identitydb.New(s)
	if err = q.LockMCPGrantCodes(ctx, pre.GrantID); err != nil {
		t.Fatal(err)
	}
	if _, err = q.LockMCPGrant(ctx, pre.GrantID); err != nil {
		t.Fatal(err)
	}
	if _, err = q.LockMCPRefreshToken(ctx, refreshHash); err != nil {
		t.Fatal(err)
	}
	now := env.clock.now()
	if _, err = q.ConsumeMCPRefreshToken(ctx, identitydb.ConsumeMCPRefreshTokenParams{TokenHash: refreshHash, ConsumedAt: ts(now)}); err != nil {
		t.Fatal(err)
	}
	access, refresh := randomMCPSecret(t), randomMCPSecret(t)
	if err = q.CreateMCPAccessToken(ctx, identitydb.CreateMCPAccessTokenParams{
		TokenHash: sha256Sum(access), GrantID: pre.GrantID, CreatedAt: ts(now), ExpiresAt: ts(now.Add(identity.MCPAccessTokenLifetime)),
	}); err != nil {
		t.Fatal(err)
	}
	if err = q.CreateMCPRefreshToken(ctx, identitydb.CreateMCPRefreshTokenParams{
		TokenHash: sha256Sum(refresh), GrantID: pre.GrantID, CreatedAt: ts(now), ExpiresAt: ts(now.Add(identity.MCPRefreshIdleLifetime)),
	}); err != nil {
		t.Fatal(err)
	}
	return s, access, refresh
}

// TestMCPReverseRaceDeletionWaitsForRefresh: a refresh holds the account and
// credential FOR SHARE, has consumed the presented token and inserted a rotated
// pair. A deletion arriving meanwhile must wait, then erase the grant and both
// rotated tokens with the account_deletion cause.
func TestMCPReverseRaceDeletionWaitsForRefresh(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_reverse_delete_refresh")
	actor := env.newActor(t)
	first, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	s, rotatedAccess, rotatedRefresh := refreshPrefix(t, env.ctx, env, user, first.RefreshToken)
	defer func() { _ = s.Rollback(context.Background()) }()

	deletion := env.startDeletion(user, actor)
	env.awaitBlocked(t, deletion)
	if err = s.Commit(env.ctx); err != nil {
		t.Fatalf("refresh commit: %v", err)
	}
	<-deletion.finished
	if deletion.err != nil {
		t.Fatalf("deletion after the refresh committed: %v", deletion.err)
	}

	env.requireNoMCPSecrets(t, user)
	env.requireDeletedGrant(t, user, actor)
	if _, err = env.svc.VerifyAccessToken(env.ctx, rotatedAccess); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("rotated access token err=%v, want ErrMCPInvalidToken", err)
	}
	if _, err = env.refresh(rotatedRefresh); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("rotated refresh token err=%v, want ErrMCPInvalidGrant", err)
	}
}

// TestMCPReverseRacePasswordReplacementWaitsForRefresh: the refresh prefix holds
// local_credentials FOR SHARE with the rotated pair written. A password
// replacement must queue on the credential row (FOR UPDATE), then after the
// refresh commits revoke the rotated tokens as password_change, leaving zero rows.
func TestMCPReverseRacePasswordReplacementWaitsForRefresh(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_reverse_pw_refresh")
	first, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	s, rotatedAccess, rotatedRefresh := refreshPrefix(t, env.ctx, env, user, first.RefreshToken)
	defer func() { _ = s.Rollback(context.Background()) }()

	replaced := make(chan error, 1)
	go func() {
		replaced <- NewPasswordChangeRepository(env.app).WithCredentialReplacement(env.ctx, func(tx identity.CredentialReplacementTransaction) error {
			if _, e := tx.LockLocalCredential(env.ctx, user); e != nil {
				return e
			}
			_, e := tx.ReplaceLocalPassword(env.ctx, user, "hash:reverse-replaced", env.clock.now(),
				identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: user})
			return e
		})
	}()
	waitForBlockedBackend(t, env.ctx, env.observer)
	if err = s.Commit(env.ctx); err != nil {
		t.Fatalf("refresh commit: %v", err)
	}
	if err = <-replaced; err != nil {
		t.Fatalf("password replacement after the refresh committed: %v", err)
	}

	env.requireNoMCPSecrets(t, user)
	env.requireGrantRevoked(t, user, "password_change", user)
	if _, err = env.svc.VerifyAccessToken(env.ctx, rotatedAccess); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("rotated access token err=%v, want ErrMCPInvalidToken", err)
	}
	if _, err = env.refresh(rotatedRefresh); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("rotated refresh token err=%v, want ErrMCPInvalidGrant", err)
	}
}

// deletionRun is an account deletion running in its own goroutine. finished
// closes after err is written, so a waiter can tell an early failure from a
// block.
type deletionRun struct {
	finished chan struct{}
	err      error
}

// startDeletion runs the deletion's identity transaction (the account row FOR NO
// KEY UPDATE, then the access erasure and the tombstone) on the runtime pool,
// the role production deletion runs as. The observer is the same role, so it
// can see this backend's lock wait; an admin-role backend's wait is hidden
// from pg_stat_activity (insufficient privilege).
func (e *mcpEnv) startDeletion(user, actor string) *deletionRun {
	run := &deletionRun{finished: make(chan struct{})}
	go func() {
		defer close(run.finished)
		tx, err := e.app.Begin(e.ctx)
		if err != nil {
			run.err = err
			return
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err = NewAccountEraser(tx).LockDeletableAccount(e.ctx, user); err != nil {
			run.err = err
			return
		}
		if err = eraseInTx(e.ctx, tx, user, actor, e.clock.now()); err != nil {
			run.err = err
			return
		}
		run.err = tx.Commit(e.ctx)
	}()
	return run
}

// awaitBlocked waits until the deletion is blocked on a lock. If it finishes
// first, the test fails with the deletion's own error rather than a timeout.
func (e *mcpEnv) awaitBlocked(t *testing.T, run *deletionRun) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-run.finished:
			t.Fatalf("deletion finished before blocking on the account lock: err=%v", run.err)
		default:
		}
		var waiting int
		if err := e.observer.QueryRow(e.ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid() AND query ILIKE '%FOR NO KEY UPDATE%'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, err := e.observer.Query(e.ctx, `SELECT pid, COALESCE(application_name,''), COALESCE(state,''), COALESCE(wait_event_type,''), COALESCE(wait_event,''), left(COALESCE(query,''), 90)
		FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var pid int
		var app, state, wt, we, query string
		if err = rows.Scan(&pid, &app, &state, &wt, &we, &query); err != nil {
			t.Fatal(err)
		}
		t.Logf("activity pid=%d app=%q state=%q wait=%s/%s query=%q", pid, app, state, wt, we, query)
	}
	t.Fatal("deletion never blocked on the account row lock")
}

// requireRevokedNotBeforeCreated asserts the account's grant was revoked at or
// after the moment it was created (the mcp_grants CHECK, clamped by the SQL).
func (e *mcpEnv) requireRevokedNotBeforeCreated(t *testing.T, user string) {
	t.Helper()
	var ok bool
	if err := e.admin.QueryRow(e.ctx, `SELECT revoked_at >= created_at FROM mcp_grants WHERE user_id=$1::uuid`, user).Scan(&ok); err != nil || !ok {
		t.Fatalf("revoked_at before created_at: ok=%v err=%v", ok, err)
	}
}

// TestMCPSubSecondGrantSurvivesPasswordChange: the grant is created at T+0.4s,
// then the password change runs with its clock at T+0.7s. ChangePassword
// truncates now to the second (T+0), which is earlier than created_at. Under the
// old SQL the revocation wrote revoked_at = T+0 and violated the CHECK
// (SQLSTATE 23514). The clamp must let the change commit with revoked_at >= created_at.
func TestMCPSubSecondGrantSurvivesPasswordChange(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_subsec_pw")
	base := env.clock.now()

	env.clock.at = base.Add(400 * time.Millisecond)
	if _, err := env.exchange(env.issue(t, user, session)); err != nil {
		t.Fatalf("exchange at T+0.4s: %v", err)
	}

	env.clock.at = base.Add(700 * time.Millisecond)
	passwords, err := identity.NewPasswordChangeService(NewPasswordChangeRepository(env.app), env.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	passwords.SetPasswordHasher(func(p string) (string, error) { return "hash:" + p, nil })
	passwords.SetPasswordVerifier(func(stored, current string) (bool, error) { return stored == "hash:"+current, nil })
	if _, err = passwords.ChangePassword(env.ctx, identity.ChangePasswordInput{
		Principal:       identity.Principal{UserID: user},
		SignInMethod:    identity.SignInPassword,
		SessionToken:    session,
		CurrentPassword: "mcp-test",
		NewPassword:     "a brand new long password",
	}); err != nil {
		t.Fatalf("ChangePassword at T+0.7s: %v", err)
	}

	env.requireNoMCPSecrets(t, user)
	env.requireGrantRevoked(t, user, "password_change", user)
	env.requireRevokedNotBeforeCreated(t, user)
}

// subSecondGrant creates a grant at T+0.4s through the real exchange, so its
// created_at sits between the truncated and the untruncated clock readings a
// later credential replacement will see.
func (e *mcpEnv) subSecondGrant(t *testing.T, user, session string) {
	t.Helper()
	base := e.clock.now()
	e.clock.at = base.Add(400 * time.Millisecond)
	if _, err := e.exchange(e.issue(t, user, session)); err != nil {
		t.Fatalf("exchange at T+0.4s: %v", err)
	}
	e.clock.at = base.Add(700 * time.Millisecond)
}

// TestMCPSubSecondGrantSurvivesAdminReset: redemption of an admin reset at
// T+0.7s revokes a grant created at T+0.4s with admin_reset and the issuing admin
// as actor. Without the clamp the revocation wrote T+0 and violated the CHECK.
func TestMCPSubSecondGrantSurvivesAdminReset(t *testing.T) {
	env := newMCPEnv(t)
	owner, target, house := passwordResetFixture(t, env.ctx, env.admin)
	env.subSecondGrant(t, target, env.sessionFor(t, target))
	// Built after subSecondGrant, so the reset service reads T+0.7s like the spec requires.
	resets := newPasswordResetService(t, env.app, &passwordResetTestClock{at: env.clock.now()})

	issued, err := resets.IssuePasswordReset(env.ctx, owner, house, target)
	if err != nil {
		t.Fatalf("issue reset: %v", err)
	}
	if _, err = resets.RedeemPasswordReset(env.ctx, issued.Token, "a brand new long password"); err != nil {
		t.Fatalf("redeem at T+0.7s: %v", err)
	}

	env.requireNoMCPSecrets(t, target)
	env.requireGrantRevoked(t, target, "admin_reset", owner)
	env.requireRevokedNotBeforeCreated(t, target)
}

// TestMCPSubSecondGrantSurvivesCLISetPassword: the operator CLI reset at T+0.7s
// revokes a grant created at T+0.4s with cli_reset and no actor.
func TestMCPSubSecondGrantSurvivesCLISetPassword(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_subsec_cli")
	env.subSecondGrant(t, user, session)

	admin, err := identity.NewAdminPasswordService(NewPasswordChangeRepository(env.app), env.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	admin.SetPasswordHasher(func(p string) (string, error) { return "hash:" + p, nil })
	if _, err = admin.SetPassword(env.ctx, "mcp_subsec_cli", "a brand new long password"); err != nil {
		t.Fatalf("SetPassword at T+0.7s: %v", err)
	}

	env.requireNoMCPSecrets(t, user)
	env.requireGrantRevoked(t, user, "cli_reset", "")
	env.requireRevokedNotBeforeCreated(t, user)
}

// TestMCPConcurrentCodeReuseExactlyOneWinner: two goroutines exchange the same
// code, twenty times. Exactly one succeeds; the loser gets ErrMCPInvalidGrant;
// the grant ends revoked code_reuse with zero secret rows. Any 40P01 fails.
func TestMCPConcurrentCodeReuseExactlyOneWinner(t *testing.T) {
	env := newMCPEnv(t)
	// The late service (env.svc, clock T+1s) and the early service (clock T+0.5s)
	// race on one code. Whichever wins, the loser's revocation time must not
	// precede the winner's created_at; the old SQL violated the CHECK when the
	// early clock lost.
	early := &mcpClock{}
	earlySvc, err := identity.NewMCPService(NewMCPRepository(env.app), identity.MCPOptions{Enabled: true, Resource: mcpTestResource}, early.now)
	if err != nil {
		t.Fatal(err)
	}
	redeem := func(svc *identity.MCPService, code string) error {
		_, err := svc.ExchangeCode(env.ctx, identity.MCPCodeExchange{
			Code: code, CodeVerifier: mcpTestVerifier, ClientID: mcpTestClientID,
			RedirectURI: mcpTestRedirectURI, Resource: mcpTestResource,
		})
		return err
	}
	for i := 0; i < 20; i++ {
		user, session := env.newAccount(t, fmt.Sprintf("mcp_code_race_%02d", i))
		code := env.issue(t, user, session)
		base := env.clock.now()
		env.clock.at = base.Add(time.Second)
		early.at = base.Add(500 * time.Millisecond)
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		for j := range errs {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				<-start
				if j == 0 {
					errs[j] = redeem(env.svc, code)
				} else {
					errs[j] = redeem(earlySvc, code)
				}
			}(j)
		}
		close(start)
		wg.Wait()
		wins, losses := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				wins++
			case errors.Is(err, identity.ErrMCPInvalidGrant):
				losses++
			default:
				t.Fatalf("iteration %d: unexpected exchange error (deadlock or constraint?): %v", i, err)
			}
		}
		if wins != 1 || losses != 1 {
			t.Fatalf("iteration %d: wins=%d losses=%d, want 1 and 1", i, wins, losses)
		}
		env.requireGrantRevoked(t, user, "code_reuse", "")
		env.requireNoMCPSecrets(t, user)
	}
}

// TestMCPCodeReuseWithEarlierLoserClock: the winner exchanges at T+1s, then the
// loser presents the same code from a service whose clock reads T+0.5s, earlier
// than the grant's created_at. The code_reuse revocation must commit (the clamp
// keeps revoked_at >= created_at); the old SQL violated the CHECK here.
func TestMCPCodeReuseWithEarlierLoserClock(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_code_skew")
	code := env.issue(t, user, session)
	base := env.clock.now()

	env.clock.at = base.Add(time.Second)
	if _, err := env.exchange(code); err != nil {
		t.Fatalf("winner exchange at T+1s: %v", err)
	}

	loserClock := &mcpClock{at: base.Add(500 * time.Millisecond)}
	loser, err := identity.NewMCPService(NewMCPRepository(env.app), identity.MCPOptions{Enabled: true, Resource: mcpTestResource}, loserClock.now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = loser.ExchangeCode(env.ctx, identity.MCPCodeExchange{
		Code: code, CodeVerifier: mcpTestVerifier, ClientID: mcpTestClientID,
		RedirectURI: mcpTestRedirectURI, Resource: mcpTestResource,
	})
	if !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("loser exchange err=%v, want ErrMCPInvalidGrant", err)
	}

	env.requireGrantRevoked(t, user, "code_reuse", "")
	env.requireRevokedNotBeforeCreated(t, user)
	env.requireNoMCPSecrets(t, user)
}

// newOIDCAccount creates a live account with no local credential and an OIDC
// session. It has no local_credentials row, so issuance and exchange take only
// the account share lock.
func (e *mcpEnv) newOIDCAccount(t *testing.T, login string) (string, string) {
	t.Helper()
	user := newLiveAccount(t, e.ctx, e.admin, login)
	session := randomMCPSecret(t)
	if _, err := e.admin.Exec(e.ctx, `INSERT INTO user_sessions(user_id, token_hash, created_at, expires_at, sign_in_method)
		VALUES($1::uuid, $2, $3, $4, 'oidc')`, user, sha256Sum(session), e.clock.now(), e.clock.now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	return user, session
}

// TestMCPOIDCOnlyDeletionRacesExchange: an OIDC-only account (no credential row).
// The deletion holds FOR NO KEY UPDATE; the exchange blocks on the account
// share lock; after the tombstone commits the exchange is refused with zero rows.
func TestMCPOIDCOnlyDeletionRacesExchange(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newOIDCAccount(t, "mcp_oidc_delete_exchange")
	actor := env.newActor(t)
	code := env.issue(t, user, session)

	tx, err := env.app.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = NewAccountEraser(tx).LockDeletableAccount(env.ctx, user); err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		tokens identity.MCPTokens
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		tokens, err := env.exchange(code)
		done <- outcome{tokens, err}
	}()
	waitForBlockedBackend(t, env.ctx, env.observer)
	if err = eraseInTx(env.ctx, tx, user, actor, env.clock.now()); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if !errors.Is(got.err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("OIDC-only exchange racing a deletion: tokens=%+v err=%v, want ErrMCPInvalidGrant", got.tokens, got.err)
	}
	env.requireNoMCPSecrets(t, user)
	if n := env.countGrants(t, user); n != 0 {
		t.Fatalf("grants created for a deleted OIDC-only account: %d", n)
	}
}

// TestMCPOIDCOnlyDeletionWaitsForIssuance: the reverse order for an OIDC-only
// account. The issuance prefix holds the account share lock (there is no
// credential row to share); the deletion waits, then erases the grant and the
// tokens the issuance committed.
func TestMCPOIDCOnlyDeletionWaitsForIssuance(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newOIDCAccount(t, "mcp_oidc_issue_delete")
	actor := env.newActor(t)
	code := env.issue(t, user, session)

	s := issuancePrefix(t, env.ctx, env.app, user)
	defer func() { _ = s.Rollback(context.Background()) }()
	q := identitydb.New(s)
	var userID pgtype.UUID
	if err := userID.Scan(user); err != nil {
		t.Fatal(err)
	}
	now := env.clock.now()
	access, refresh := randomMCPSecret(t), randomMCPSecret(t)
	grantID, err := q.CreateMCPGrant(env.ctx, identitydb.CreateMCPGrantParams{
		UserID: userID, ClientID: mcpTestClientID, Scopes: []string{identity.MCPScopeRead},
		Resource: mcpTestResource, CreatedAt: ts(now), ExpiresAt: ts(now.Add(identity.MCPGrantLifetime)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = q.RecordMCPGrantCreated(env.ctx, grantID); err != nil {
		t.Fatal(err)
	}
	if _, err = q.MarkMCPAuthorizationCodeUsed(env.ctx, identitydb.MarkMCPAuthorizationCodeUsedParams{
		CodeHash: sha256Sum(code), UsedAt: ts(now), IssuedGrantID: grantID,
	}); err != nil {
		t.Fatal(err)
	}
	if err = q.CreateMCPAccessToken(env.ctx, identitydb.CreateMCPAccessTokenParams{
		TokenHash: sha256Sum(access), GrantID: grantID, CreatedAt: ts(now), ExpiresAt: ts(now.Add(identity.MCPAccessTokenLifetime)),
	}); err != nil {
		t.Fatal(err)
	}
	if err = q.CreateMCPRefreshToken(env.ctx, identitydb.CreateMCPRefreshTokenParams{
		TokenHash: sha256Sum(refresh), GrantID: grantID, CreatedAt: ts(now), ExpiresAt: ts(now.Add(identity.MCPRefreshIdleLifetime)),
	}); err != nil {
		t.Fatal(err)
	}

	deletion := env.startDeletion(user, actor)
	env.awaitBlocked(t, deletion)
	if err = s.Commit(env.ctx); err != nil {
		t.Fatalf("issuance commit: %v", err)
	}
	<-deletion.finished
	if deletion.err != nil {
		t.Fatalf("deletion after the OIDC issuance committed: %v", deletion.err)
	}
	env.requireNoMCPSecrets(t, user)
	env.requireDeletedGrant(t, user, actor)
	if _, err = env.svc.VerifyAccessToken(env.ctx, access); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("OIDC access token issued before the deletion err=%v, want ErrMCPInvalidToken", err)
	}
}

// TestMCPIssuancePrefixVersusPasswordReplacement: an issuance holds its prefix
// locks (account and credential FOR SHARE) and has inserted a code row through
// CreateMCPAuthorizationCode. A password replacement queues on the credential
// row. After the issuance commits, the replacement must revoke the grant set and
// leave zero code rows.
func TestMCPIssuancePrefixVersusPasswordReplacement(t *testing.T) {
	env := newMCPEnv(t)
	user, _ := env.newAccount(t, "mcp_issue_prefix_pw")

	s := issuancePrefix(t, env.ctx, env.app, user)
	defer func() { _ = s.Rollback(context.Background()) }()
	var userID pgtype.UUID
	if err := userID.Scan(user); err != nil {
		t.Fatal(err)
	}
	now := env.clock.now()
	code := randomMCPSecret(t)
	if err := identitydb.New(s).CreateMCPAuthorizationCode(env.ctx, identitydb.CreateMCPAuthorizationCodeParams{
		CodeHash: sha256Sum(code), UserID: userID, ClientID: mcpTestClientID, RedirectUri: mcpTestRedirectURI,
		CodeChallenge: mcpTestChallenge(), Scopes: []string{identity.MCPScopeRead}, Resource: mcpTestResource,
		CreatedAt: ts(now), ExpiresAt: ts(now.Add(identity.MCPCodeLifetime)),
	}); err != nil {
		t.Fatalf("prefix code insert: %v", err)
	}

	replaced := make(chan error, 1)
	go func() {
		replaced <- NewPasswordChangeRepository(env.app).WithCredentialReplacement(env.ctx, func(tx identity.CredentialReplacementTransaction) error {
			if _, e := tx.LockLocalCredential(env.ctx, user); e != nil {
				return e
			}
			_, e := tx.ReplaceLocalPassword(env.ctx, user, "hash:prefix-replaced", now,
				identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: user})
			return e
		})
	}()
	waitForBlockedStatement(t, env.ctx, env.observer, "%FOR UPDATE%")
	if err := s.Commit(env.ctx); err != nil {
		t.Fatalf("issuance commit: %v", err)
	}
	if err := <-replaced; err != nil {
		t.Fatalf("password replacement after the issuance committed: %v", err)
	}

	// The prefix wrote only a code (no grant yet), so the replacement's
	// DeleteAccountMCPCodes is what must leave zero rows.
	env.requireNoMCPSecrets(t, user)
}

// requireAllowed reports (without FailNow, so it is safe from goroutines) any
// error outside the expected outcome set, and every SQLSTATE 40P01 (deadlock)
// regardless of the set.
func requireAllowed(t *testing.T, iteration int, who string, err error, allowed ...error) {
	t.Helper()
	if err == nil {
		return
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
		t.Errorf("iteration %d %s: deadlock detected: %v", iteration, who, err)
		return
	}
	for _, want := range allowed {
		if errors.Is(err, want) {
			return
		}
	}
	t.Errorf("iteration %d %s: unexpected error: %v", iteration, who, err)
}

// requireNoRevokedSecrets asserts that no revoked grant anywhere keeps a code,
// access or refresh row.
func (e *mcpEnv) requireNoRevokedSecrets(t *testing.T, iteration int) {
	t.Helper()
	var revokedAccess, revokedRefresh, revokedCodes int
	if err := e.admin.QueryRow(e.ctx, `SELECT
		(SELECT count(*) FROM mcp_access_tokens a JOIN mcp_grants g ON g.id=a.grant_id WHERE g.revoked_at IS NOT NULL),
		(SELECT count(*) FROM mcp_refresh_tokens r JOIN mcp_grants g ON g.id=r.grant_id WHERE g.revoked_at IS NOT NULL),
		(SELECT count(*) FROM mcp_authorization_codes c JOIN mcp_grants g ON g.id=c.issued_grant_id WHERE g.revoked_at IS NOT NULL)`,
	).Scan(&revokedAccess, &revokedRefresh, &revokedCodes); err != nil {
		t.Fatal(err)
	}
	if revokedAccess != 0 || revokedRefresh != 0 || revokedCodes != 0 {
		t.Fatalf("iteration %d: revoked grants keep secrets access=%d refresh=%d codes=%d", iteration, revokedAccess, revokedRefresh, revokedCodes)
	}
}

// TestMCPDeadlockFreedomSmoke runs deletion, password change, refresh and
// exchange (plus a new issuance) concurrently for thirty rounds across two
// accounts. No round may produce SQLSTATE 40P01 or an unexpected error, a
// deleted account keeps zero secret rows, and no revoked grant keeps a secret.
func TestMCPDeadlockFreedomSmoke(t *testing.T) {
	env := newMCPEnv(t)
	actor := env.newActor(t)
	for i := 0; i < 30; i++ {
		userA, sessionA := env.newAccount(t, fmt.Sprintf("mcp_smoke_a_%02d", i))
		userB, sessionB := env.newAccount(t, fmt.Sprintf("mcp_smoke_b_%02d", i))
		tokA, err := env.exchange(env.issue(t, userA, sessionA))
		if err != nil {
			t.Fatalf("iteration %d exchange A: %v", i, err)
		}
		tokB, err := env.exchange(env.issue(t, userB, sessionB))
		if err != nil {
			t.Fatalf("iteration %d exchange B: %v", i, err)
		}
		codeA, codeB := env.issue(t, userA, sessionA), env.issue(t, userB, sessionB)
		newSessionA, newSessionB := env.sessionFor(t, userA), env.sessionFor(t, userB)

		start := make(chan struct{})
		var wg sync.WaitGroup
		spawn := func(fn func()) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				fn()
			}()
		}
		spawn(func() { // deletion of A
			tx, err := env.admin.Begin(env.ctx)
			if err != nil {
				requireAllowed(t, i, "delete begin", err)
				return
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err = NewAccountEraser(tx).LockDeletableAccount(env.ctx, userA); err != nil {
				requireAllowed(t, i, "delete lock", err, household.ErrNotFound)
				return
			}
			err = eraseInTx(env.ctx, tx, userA, actor, env.clock.now())
			requireAllowed(t, i, "delete erase", err)
			requireAllowed(t, i, "delete commit", tx.Commit(env.ctx))
		})
		spawn(func() { // password change of B
			err := NewPasswordChangeRepository(env.app).WithCredentialReplacement(env.ctx, func(tx identity.CredentialReplacementTransaction) error {
				if _, e := tx.LockLocalCredential(env.ctx, userB); e != nil {
					return e
				}
				_, e := tx.ReplaceLocalPassword(env.ctx, userB, "hash:smoke", env.clock.now(),
					identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: userB})
				return e
			})
			requireAllowed(t, i, "password change B", err)
		})
		refreshChain := func(who, start string) {
			spawn(func() { // each rotation carries the newest refresh token forward
				current := start
				for r := 0; r < 3; r++ {
					next, err := env.refresh(current)
					requireAllowed(t, i, who, err, identity.ErrMCPInvalidGrant)
					if err != nil {
						return
					}
					current = next.RefreshToken
				}
			})
		}
		refreshChain("refresh A", tokA.RefreshToken)
		refreshChain("refresh B", tokB.RefreshToken)
		spawn(func() {
			_, err := env.exchange(codeA)
			requireAllowed(t, i, "exchange A", err, identity.ErrMCPInvalidGrant)
		})
		spawn(func() {
			_, err := env.exchange(codeB)
			requireAllowed(t, i, "exchange B", err, identity.ErrMCPInvalidGrant)
		})
		spawn(func() {
			_, err := env.tryIssue(userA, newSessionA)
			requireAllowed(t, i, "issue A", err, identity.ErrUnauthenticated)
		})
		spawn(func() {
			_, err := env.tryIssue(userB, newSessionB)
			requireAllowed(t, i, "issue B", err, identity.ErrUnauthenticated)
		})
		close(start)
		wg.Wait()

		if c := env.counts(t, userA); c != (mcpCounts{}) {
			t.Fatalf("iteration %d: deleted account A keeps secret rows %+v", i, c)
		}
		env.requireNoRevokedSecrets(t, i)
	}
}
