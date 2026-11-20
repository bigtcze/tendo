package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identitydb "github.com/bigtcze/tendo/backend/internal/identity/postgres/dbgen"
)

// Lock-ordering tests for the decision (ADR 0023): a consent decision against
// account deletion and against a password replacement, in both orders, and the
// authorize step against the client retention sweep.

// eraseAccountAdmin deletes an account the way the household deletion does,
// through an admin transaction that holds the account row first.
func eraseAccountAdmin(t *testing.T, env *mcpEnv, user, actor string) {
	t.Helper()
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
}

// TestMCPDecisionRacesAccountDeletion: deletion first refuses the decision with
// ErrUnauthenticated and no code survives. Decision first mints a code that the
// deletion then erases.
func TestMCPDecisionRacesAccountDeletion(t *testing.T) {
	t.Run("deletion holds the account row first", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_dec_delete_first")
		actor := env.newActor(t)
		pending := env.begin(t, mcpTestRedirectURI, "")

		tx, err := env.admin.Begin(env.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(context.Background()) }()
		if _, err = NewAccountEraser(tx).LockDeletableAccount(env.ctx, user); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() {
			_, e := env.decide(user, session, pending, true)
			done <- e
		}()
		waitForBlockedBackend(t, env.ctx, env.observer)
		if err = eraseInTx(env.ctx, tx, user, actor, env.clock.now()); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(env.ctx); err != nil {
			t.Fatal(err)
		}
		if err = <-done; !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("decision racing a deletion err=%v, want ErrUnauthenticated", err)
		}
		env.requireNoMCPSecrets(t, user)
	})

	t.Run("decision commits first and the deletion erases the code", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_dec_decide_first")
		actor := env.newActor(t)
		pending := env.begin(t, mcpTestRedirectURI, "")
		if _, err := env.decide(user, session, pending, true); err != nil {
			t.Fatalf("decision: %v", err)
		}
		if c := env.counts(t, user); c.codes != 1 {
			t.Fatalf("codes before deletion=%d, want 1", c.codes)
		}
		eraseAccountAdmin(t, env, user, actor)
		env.requireNoMCPSecrets(t, user)
	})
}

// TestMCPDecisionRacesPasswordReplacement: a password change first refuses the
// decision (its session is gone) and mints no code. A decision that commits
// first leaves a code the password change then removes.
func TestMCPDecisionRacesPasswordReplacement(t *testing.T) {
	t.Run("replacement holds the credential row first", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_dec_pw_first")
		pending := env.begin(t, mcpTestRedirectURI, "")
		revocation := identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: user}
		locked, release, done := env.startPasswordReplacement(user, revocation)
		awaitLocked(t, locked, done)

		decided := make(chan error, 1)
		go func() {
			_, e := env.decide(user, session, pending, true)
			decided <- e
		}()
		waitForBlockedBackend(t, env.ctx, env.observer)
		close(release)
		if err := <-done; err != nil {
			t.Fatalf("password replacement commit: %v", err)
		}
		if err := <-decided; !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatalf("decision racing a password change err=%v, want ErrUnauthenticated", err)
		}
		env.requireNoMCPSecrets(t, user)
	})

	t.Run("decision commits first and the password change removes the code", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_dec_pw_decide")
		pending := env.begin(t, mcpTestRedirectURI, "")
		if _, err := env.decide(user, session, pending, true); err != nil {
			t.Fatalf("decision: %v", err)
		}
		repo := NewPasswordChangeRepository(env.app)
		revocation := identity.MCPRevocation{Reason: identity.MCPRevokePasswordChange, ActorUserID: user}
		if err := repo.WithCredentialReplacement(env.ctx, func(tx identity.CredentialReplacementTransaction) error {
			if _, e := tx.LockLocalCredential(env.ctx, user); e != nil {
				return e
			}
			_, e := tx.ReplaceLocalPassword(env.ctx, user, "hash:dec-replaced", env.clock.now(), revocation)
			return e
		}); err != nil {
			t.Fatalf("password replacement: %v", err)
		}
		env.requireNoMCPSecrets(t, user)
	})
}

// TestMCPAuthorizeRacesClientSweep covers both orders of the authorize step and
// the retention sweep for a client older than 24h with no grant.
//
// The in-flight case holds the client FOR KEY SHARE through an open transaction,
// the same lock BeginAuthorization takes in FindClient, while the sweep runs.
// The sweep must skip the client (FOR UPDATE SKIP LOCKED), and the request must
// commit afterwards. The sweep-first case must make BeginAuthorization return
// ErrMCPInvalidClient, not a storage error.
func TestMCPAuthorizeRacesClientSweep(t *testing.T) {
	ageClient := func(t *testing.T, env *mcpEnv) {
		t.Helper()
		if _, err := env.admin.Exec(env.ctx, `UPDATE mcp_clients SET created_at=$1 WHERE client_id=$2`,
			env.clock.now().Add(-48*time.Hour), mcpTestClientID); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("authorize holds the client when the sweep runs", func(t *testing.T) {
		env := newMCPEnv(t)
		ageClient(t, env)
		holder, err := env.admin.Begin(env.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Rollback(context.Background()) }()
		// The same statement BeginAuthorization runs through FindClient: FOR KEY SHARE.
		if _, err = identitydb.New(holder).FindMCPClient(env.ctx, mcpTestClientID); err != nil {
			t.Fatal(err)
		}

		svc, err := identity.NewMCPService(NewMCPRepository(env.app), identity.MCPOptions{Enabled: true, Resource: mcpTestResource}, env.clock.now)
		if err != nil {
			t.Fatal(err)
		}
		report, err := svc.Sweep(env.ctx, env.clock.now())
		if err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if report.Deleted[identity.MCPSweepGrantlessClients] != 0 {
			t.Fatalf("sweep deleted a client an authorize holds: deleted=%v", report.Deleted)
		}
		if err = holder.Commit(env.ctx); err != nil {
			t.Fatal(err)
		}
		if n := clientRows(t, env, mcpTestClientID); n != 1 {
			t.Fatal("client swept while its authorize held it")
		}
		pending := env.begin(t, mcpTestRedirectURI, "")
		if n := countAuthorizationRequests(t, env, sha256Sum(pending.Handle)); n != 1 {
			t.Fatalf("request not committed after the sweep: rows=%d", n)
		}
	})

	t.Run("sweep deletes the client first", func(t *testing.T) {
		env := newMCPEnv(t)
		ageClient(t, env)
		if _, err := env.svc.Sweep(env.ctx, env.clock.now()); err != nil {
			t.Fatalf("Sweep: %v", err)
		}
		if n := clientRows(t, env, mcpTestClientID); n != 0 {
			t.Fatal("grantless client older than 24h survived the sweep")
		}
		_, err := env.svc.BeginAuthorization(env.ctx, authorizeParams(mcpTestRedirectURI, ""))
		if !errors.Is(err, identity.ErrMCPInvalidClient) {
			t.Fatalf("authorize for a swept client err=%v, want ErrMCPInvalidClient", err)
		}
	})
}
