package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
	"github.com/jackc/pgx/v5/pgconn"
)

// Slice B1b3 of #457: the administrator grant list and revocations against the
// real schema, the runtime role and the ADR 0023 lock order.

// adminRevokedEvents returns "grant:reason:actor" for every revoked event of the
// account, in grant id order.
func (e *mcpEnv) adminRevokedEvents(t *testing.T, user string) []string {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT grant_id::text||':'||reason||':'||COALESCE(actor_user_id::text,'')
		FROM mcp_grant_events WHERE user_id=$1::uuid AND event='revoked' ORDER BY grant_id`, user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// grantIDs lists the account's grant ids, newest first.
func (e *mcpEnv) grantIDs(t *testing.T, user string) []string {
	t.Helper()
	rows, err := e.admin.Query(e.ctx, `SELECT id::text FROM mcp_grants WHERE user_id=$1::uuid ORDER BY id DESC`, user)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

// adminFixture is an instance administrator and a target account with two
// exchanged grants.
type adminFixture struct {
	env      *mcpEnv
	policies *identity.MCPPolicyService
	admin    string
	user     string
	tokens   []identity.MCPTokens
	grants   []string // newest first
}

func newAdminFixture(t *testing.T, prefix string) adminFixture {
	t.Helper()
	env := newMCPEnv(t)
	fx := adminFixture{env: env, policies: env.policyService(t)}
	fx.admin, _ = env.newAccount(t, prefix+"_admin")
	env.grantInstanceAdmin(t, fx.admin)
	var session string
	fx.user, session = env.newAccount(t, prefix+"_target")
	for range 2 {
		tokens, err := env.exchange(env.issue(t, fx.user, session))
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		fx.tokens = append(fx.tokens, tokens)
	}
	fx.grants = env.grantIDs(t, fx.user)
	if len(fx.grants) != 2 {
		t.Fatalf("fixture grants = %v, want two", fx.grants)
	}
	return fx
}

// TestMCPAdminGrantsListRevokeAndRevokeAll: the list shows both live grants;
// one admin revoke ends that grant's next request and refresh only; revoke-all
// ends the other; each revocation is one admin event with the administrator
// as actor, and no secret row survives.
func TestMCPAdminGrantsListRevokeAndRevokeAll(t *testing.T) {
	fx := newAdminFixture(t, "mcp_admin_grants")
	env := fx.env

	page, err := fx.policies.AccountGrants(env.ctx, fx.admin, fx.user, 1, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != fx.grants[0] || page.NextCursor == "" {
		t.Fatalf("first page=%+v err=%v", page, err)
	}
	next, err := fx.policies.AccountGrants(env.ctx, fx.admin, fx.user, 1, page.NextCursor)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != fx.grants[1] || next.NextCursor != "" {
		t.Fatalf("second page=%+v err=%v", next, err)
	}

	// fx.tokens[1] belongs to the newer grant, fx.grants[0].
	if err = fx.policies.RevokeAccountGrant(env.ctx, fx.admin, fx.user, fx.grants[0]); err != nil {
		t.Fatalf("admin revoke: %v", err)
	}
	if _, err = env.svc.VerifyAccessToken(env.ctx, fx.tokens[1].AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("revoked grant's next request err=%v", err)
	}
	if _, err = env.refresh(fx.tokens[1].RefreshToken); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("revoked grant's refresh err=%v", err)
	}
	if _, err = env.svc.VerifyAccessToken(env.ctx, fx.tokens[0].AccessToken); err != nil {
		t.Fatalf("the other grant was touched: %v", err)
	}
	if err = fx.policies.RevokeAccountGrant(env.ctx, fx.admin, fx.user, fx.grants[0]); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("second revoke err=%v, want ErrNotFound", err)
	}

	if err = fx.policies.RevokeAccountGrants(env.ctx, fx.admin, fx.user); err != nil {
		t.Fatalf("admin revoke-all: %v", err)
	}
	if _, err = env.refresh(fx.tokens[0].RefreshToken); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("refresh after revoke-all err=%v", err)
	}
	env.requireNoMCPSecrets(t, fx.user)
	want := []string{fx.grants[0] + ":admin:" + fx.admin, fx.grants[1] + ":admin:" + fx.admin}
	slices.Sort(want)
	if got := env.adminRevokedEvents(t, fx.user); !slices.Equal(got, want) {
		t.Fatalf("revoked events=%v want %v", got, want)
	}

	// A revoke-all with nothing left is a no-op: no event.
	if err = fx.policies.RevokeAccountGrants(env.ctx, fx.admin, fx.user); err != nil {
		t.Fatalf("empty revoke-all: %v", err)
	}
	if got := env.adminRevokedEvents(t, fx.user); len(got) != 2 {
		t.Fatalf("empty revoke-all wrote events: %v", got)
	}
	empty, err := fx.policies.AccountGrants(env.ctx, fx.admin, fx.user, 0, "")
	if err != nil || len(empty.Items) != 0 || empty.Items == nil {
		t.Fatalf("list after revoke-all=%+v err=%v", empty, err)
	}
}

// TestMCPAdminGrantsRefusals: a non-administrator, a grant named under another
// account and an unknown or deleted account change nothing.
func TestMCPAdminGrantsRefusals(t *testing.T) {
	fx := newAdminFixture(t, "mcp_admin_refuse")
	env := fx.env
	other, _ := env.newAccount(t, "mcp_admin_refuse_other")

	if err := fx.policies.RevokeAccountGrants(env.ctx, fx.user, fx.user); !errors.Is(err, identity.ErrInstanceAdminRequired) {
		t.Fatalf("non-admin revoke-all err=%v", err)
	}
	if _, err := fx.policies.AccountGrants(env.ctx, fx.user, fx.user, 0, ""); !errors.Is(err, identity.ErrInstanceAdminRequired) {
		t.Fatalf("non-admin list err=%v", err)
	}
	if err := fx.policies.RevokeAccountGrant(env.ctx, fx.admin, other, fx.grants[0]); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("grant under another account err=%v", err)
	}
	unknown := mcpSeedGrantID(0xfeed)
	if err := fx.policies.RevokeAccountGrants(env.ctx, fx.admin, unknown); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("revoke-all of an unknown account err=%v", err)
	}
	if _, err := fx.policies.AccountGrants(env.ctx, fx.admin, unknown, 0, ""); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("list of an unknown account err=%v", err)
	}
	if got := env.adminRevokedEvents(t, fx.user); len(got) != 0 {
		t.Fatalf("refusals wrote events: %v", got)
	}
	if c := env.counts(t, fx.user); c.access != 2 || c.refresh != 2 {
		t.Fatalf("refusals deleted secrets: %+v", c)
	}

	if _, err := env.admin.Exec(env.ctx, policyTombstoneSQL, fx.user); err != nil {
		t.Fatal(err)
	}
	if err := fx.policies.RevokeAccountGrant(env.ctx, fx.admin, fx.user, fx.grants[0]); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("revoke on a deleted account err=%v", err)
	}
	if err := fx.policies.RevokeAccountGrants(env.ctx, fx.admin, fx.user); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("revoke-all on a deleted account err=%v", err)
	}
	if _, err := fx.policies.AccountGrants(env.ctx, fx.admin, fx.user, 0, ""); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("list of a deleted account err=%v", err)
	}
}

// TestMCPAdminRevokeWaitsForRefresh: a refresh rotation that holds the account
// and the grant makes the administrator revoke queue in LockGrant. After
// the refresh commits the revoke wins, and both the presented and the rotated
// pair are gone.
func TestMCPAdminRevokeWaitsForRefresh(t *testing.T) {
	fx := newAdminFixture(t, "mcp_admin_race_refresh")
	env := fx.env
	grantID, presented := fx.grants[0], fx.tokens[1]

	tx, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	held := &mcpTx{q: identitydb.New(tx)}
	digest := sha256.Sum256([]byte(presented.RefreshToken))
	if err = held.LockLiveAccount(env.ctx, fx.user); err != nil {
		t.Fatal(err)
	}
	if _, err = held.LockGrant(env.ctx, grantID); err != nil {
		t.Fatal(err)
	}
	if _, err = held.LockRefreshToken(env.ctx, digest[:]); err != nil {
		t.Fatal(err)
	}
	if err = held.ConsumeRefreshToken(env.ctx, digest[:], env.clock.now()); err != nil {
		t.Fatal(err)
	}
	rotated := mintMCPPair(t)
	if err = held.CreateTokens(env.ctx, identity.MCPTokenIssue{
		GrantID: grantID, AccessHash: rotated.accessHash, RefreshHash: rotated.refreshHash, CreatedAt: env.clock.now(),
		AccessExpiresAt:  env.clock.now().Add(identity.MCPAccessTokenLifetime),
		RefreshExpiresAt: env.clock.now().Add(identity.MCPRefreshIdleLifetime),
	}); err != nil {
		t.Fatal(err)
	}

	revoked := make(chan error, 1)
	go func() { revoked <- fx.policies.RevokeAccountGrant(env.ctx, fx.admin, fx.user, grantID) }()
	waitForLockWaiters(t, env.ctx, env.observer, "%FOR UPDATE%", 1)
	select {
	case err := <-revoked:
		t.Fatalf("admin revoke returned while the refresh held the grant: %v", err)
	default:
	}
	if err = tx.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-revoked; err != nil {
		t.Fatalf("admin revoke after the refresh: %v", err)
	}
	if reason := env.requireOneRevocation(t, grantID); reason != string(identity.MCPRevokeAdmin) {
		t.Fatalf("revocation reason=%q want admin", reason)
	}
	for _, token := range []string{presented.RefreshToken, rotated.refresh} {
		if _, err = env.refresh(token); !errors.Is(err, identity.ErrMCPInvalidGrant) {
			t.Fatalf("refresh after admin revoke err=%v", err)
		}
	}
	if _, err = env.svc.VerifyAccessToken(env.ctx, rotated.access); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("rotated access token after admin revoke err=%v", err)
	}
}

// TestMCPAdminRevokeAllWaitsForPasswordChange: a password replacement that
// holds the credential row makes the administrator revoke-all queue on the
// credential share lock. The replacement revokes every grant first, so the
// revoke-all finds nothing and writes no event: each grant has exactly one
// revocation, password_change.
func TestMCPAdminRevokeAllWaitsForPasswordChange(t *testing.T) {
	fx := newAdminFixture(t, "mcp_admin_race_password")
	env := fx.env
	locked, release, done := env.startPasswordReplacement(fx.user, identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: fx.user})
	awaitLocked(t, locked, done)

	revoked := make(chan error, 1)
	go func() { revoked <- fx.policies.RevokeAccountGrants(env.ctx, fx.admin, fx.user) }()
	waitForBlockedStatement(t, env.ctx, env.observer, "%local_credentials%FOR SHARE%")
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("password replacement: %v", err)
	}
	if err := <-revoked; err != nil {
		t.Fatalf("admin revoke-all after the password change: %v", err)
	}
	for _, id := range fx.grants {
		if reason := env.requireOneRevocation(t, id); reason != string(identity.MCPRevokePasswordChange) {
			t.Fatalf("grant %s revocation=%q want password_change", id, reason)
		}
	}
	env.requireNoMCPSecrets(t, fx.user)
}

// TestMCPAdminRevokeAllFirstThenPasswordChange: the revoke-all holds the
// account and credential share locks; the replacement waits, then finds no
// live grant. Each grant has exactly one revocation, admin.
func TestMCPAdminRevokeAllFirstThenPasswordChange(t *testing.T) {
	fx := newAdminFixture(t, "mcp_admin_race_first")
	env := fx.env
	repo := NewMCPRepository(env.app)

	inside, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unpause := func() { releaseOnce.Do(func() { close(release) }) }
	defer unpause()
	revoked := make(chan error, 1)
	go func() {
		revoked <- repo.WithMCPPolicyTransaction(env.ctx, func(tx identity.MCPPolicyTransaction) error {
			if err := tx.RequireInstanceAdmin(env.ctx, fx.admin); err != nil {
				return err
			}
			if err := tx.LockAccountForGrants(env.ctx, fx.user); err != nil {
				return err
			}
			if err := tx.RevokeAccountGrants(env.ctx, fx.user, env.clock.now(), identity.MCPRevocation{Reason: identity.MCPRevokeAdmin, ActorUserID: fx.admin}); err != nil {
				return err
			}
			close(inside)
			<-release
			return nil
		})
	}()
	select {
	case <-inside:
	case err := <-revoked:
		t.Fatalf("revoke-all failed before the pause: %v", err)
	}
	locked, releasePW, done := env.startPasswordReplacement(fx.user, identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: fx.user})
	var releasePWOnce sync.Once
	unpausePW := func() { releasePWOnce.Do(func() { close(releasePW) }) }
	defer unpausePW()
	waitForBlockedStatement(t, env.ctx, env.observer, "%local_credentials%FOR UPDATE%")
	unpause()
	if err := <-revoked; err != nil {
		t.Fatalf("admin revoke-all: %v", err)
	}
	awaitLocked(t, locked, done)
	unpausePW()
	if err := <-done; err != nil {
		t.Fatalf("password replacement after the revoke-all: %v", err)
	}
	for _, id := range fx.grants {
		if reason := env.requireOneRevocation(t, id); reason != string(identity.MCPRevokeAdmin) {
			t.Fatalf("grant %s revocation=%q want admin", id, reason)
		}
	}
	env.requireNoMCPSecrets(t, fx.user)
}

// TestMCPAdminRevokeWaitsForPasswordChange: a single administrator revoke
// queued on the credential share lock behind a password replacement finds the
// grant revoked password_change and answers not found, with no admin event.
func TestMCPAdminRevokeWaitsForPasswordChange(t *testing.T) {
	fx := newAdminFixture(t, "mcp_admin_race_pw_one")
	env := fx.env
	locked, release, done := env.startPasswordReplacement(fx.user, identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: fx.user})
	awaitLocked(t, locked, done)

	revoked := make(chan error, 1)
	go func() { revoked <- fx.policies.RevokeAccountGrant(env.ctx, fx.admin, fx.user, fx.grants[0]) }()
	waitForBlockedStatement(t, env.ctx, env.observer, "%local_credentials%FOR SHARE%")
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("password replacement: %v", err)
	}
	if err := <-revoked; !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("admin revoke after the password change err=%v, want ErrNotFound", err)
	}
	for _, id := range fx.grants {
		if reason := env.requireOneRevocation(t, id); reason != string(identity.MCPRevokePasswordChange) {
			t.Fatalf("grant %s revocation=%q want password_change", id, reason)
		}
	}
	env.requireNoMCPSecrets(t, fx.user)
}

// lockInstallationForDeletion takes the deletion's first lock,
// installation_state FOR UPDATE, on a raw transaction.
func (e *mcpEnv) lockInstallationForDeletion(t *testing.T, tx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) {
	t.Helper()
	if _, err := tx.Exec(e.ctx, `SELECT 1 FROM installation_state WHERE singleton FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
}

// TestMCPAdminGrantsQueueBehindAccountDeletion: an account deletion that holds
// installation_state makes both administrator revocations queue on its
// FOR SHARE. After the tombstone commits both are not found, and each grant
// keeps its one account_deletion revocation.
func TestMCPAdminGrantsQueueBehindAccountDeletion(t *testing.T) {
	fx := newAdminFixture(t, "mcp_admin_race_delete")
	env := fx.env
	actor := env.newActor(t)

	tx, err := env.admin.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	env.lockInstallationForDeletion(t, tx)
	if _, err = NewAccountEraser(tx).LockDeletableAccount(env.ctx, fx.user); err != nil {
		t.Fatal(err)
	}
	if err = eraseInTx(env.ctx, tx, fx.user, actor, env.clock.now()); err != nil {
		t.Fatal(err)
	}
	one, all := make(chan error, 1), make(chan error, 1)
	go func() { one <- fx.policies.RevokeAccountGrant(env.ctx, fx.admin, fx.user, fx.grants[0]) }()
	go func() { all <- fx.policies.RevokeAccountGrants(env.ctx, fx.admin, fx.user) }()
	waitForLockWaiters(t, env.ctx, env.observer, "%installation_state%FOR SHARE%", 2)
	if err = tx.Commit(env.ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-one; !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("admin revoke after deletion err=%v, want ErrNotFound", err)
	}
	if err = <-all; !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("admin revoke-all after deletion err=%v, want ErrNotFound", err)
	}
	for _, id := range fx.grants {
		if reason := env.requireOneRevocation(t, id); reason != "account_deletion" {
			t.Fatalf("grant %s revocation=%q want account_deletion", id, reason)
		}
	}
	env.requireNoMCPSecrets(t, fx.user)
}

// TestMCPAdminRevokeAllFirstThenAccountDeletion: a revoke-all holding
// installation_state FOR SHARE makes a deletion queue on its FOR UPDATE. The
// deletion then finds no live grant: each grant keeps its one admin event.
func TestMCPAdminRevokeAllFirstThenAccountDeletion(t *testing.T) {
	fx := newAdminFixture(t, "mcp_admin_race_first_delete")
	env := fx.env
	actor := env.newActor(t)
	repo := NewMCPRepository(env.app)

	inside, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unpause := func() { releaseOnce.Do(func() { close(release) }) }
	defer unpause()
	revoked := make(chan error, 1)
	go func() {
		revoked <- repo.WithMCPPolicyTransaction(env.ctx, func(tx identity.MCPPolicyTransaction) error {
			if err := tx.RequireInstanceAdmin(env.ctx, fx.admin); err != nil {
				return err
			}
			if err := tx.LockAccountForGrants(env.ctx, fx.user); err != nil {
				return err
			}
			if err := tx.RevokeAccountGrants(env.ctx, fx.user, env.clock.now(), identity.MCPRevocation{Reason: identity.MCPRevokeAdmin, ActorUserID: fx.admin}); err != nil {
				return err
			}
			close(inside)
			<-release
			return nil
		})
	}()
	select {
	case <-inside:
	case err := <-revoked:
		t.Fatalf("revoke-all failed before the pause: %v", err)
	}
	deleted := make(chan error, 1)
	go func() {
		deleted <- func() error {
			// The runtime pool, as in production: the observer sees only the
			// runtime role's statements.
			tx, err := env.app.Begin(env.ctx)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			if _, err = tx.Exec(env.ctx, `SELECT 1 FROM installation_state WHERE singleton FOR UPDATE`); err != nil {
				return err
			}
			if _, err = NewAccountEraser(tx).LockDeletableAccount(env.ctx, fx.user); err != nil {
				return err
			}
			if err = eraseInTx(env.ctx, tx, fx.user, actor, env.clock.now()); err != nil {
				return err
			}
			return tx.Commit(env.ctx)
		}()
	}()
	waitForBlockedStatement(t, env.ctx, env.observer, "%installation_state%FOR UPDATE%")
	unpause()
	if err := <-revoked; err != nil {
		t.Fatalf("admin revoke-all: %v", err)
	}
	if err := <-deleted; err != nil {
		t.Fatalf("deletion after the revoke-all: %v", err)
	}
	for _, id := range fx.grants {
		if reason := env.requireOneRevocation(t, id); reason != string(identity.MCPRevokeAdmin) {
			t.Fatalf("grant %s revocation=%q want admin", id, reason)
		}
	}
	env.requireNoMCPSecrets(t, fx.user)
}
