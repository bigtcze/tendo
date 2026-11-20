package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// authorizationRecord is a valid pending consent for the test client. Hashes are
// SHA-256 digests as the service stores them.
func authorizationRecord(env *mcpEnv, requestHash, bindingHash []byte, state string) identity.MCPAuthorizationRequestRecord {
	now := env.clock.now()
	return identity.MCPAuthorizationRequestRecord{
		RequestHash: requestHash, BindingHash: bindingHash, ClientID: mcpTestClientID,
		RedirectURI: mcpTestRedirectURI, CodeChallenge: mcpTestChallenge(), Scopes: []string{identity.MCPScopeRead},
		Resource: mcpTestResource, State: state, CreatedAt: now, ExpiresAt: now.Add(identity.MCPAuthorizationRequestLifetime),
	}
}

// withTx runs one repository transaction and returns its error.
func withTx(t *testing.T, env *mcpEnv, work func(identity.MCPTransaction) error) error {
	t.Helper()
	return NewMCPRepository(env.app).WithMCPTransaction(env.ctx, work)
}

// countAuthorizationRequests counts pending consents with this handle digest.
func countAuthorizationRequests(t *testing.T, env *mcpEnv, requestHash []byte) int {
	t.Helper()
	var n int
	if err := env.admin.QueryRow(env.ctx, `SELECT count(*) FROM mcp_authorization_requests WHERE request_hash=$1`, requestHash).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAuthorizationRequestRoundTripAndSingleUse creates a consent, reads it back
// with the right binding, consumes it, and then refuses a second consume.
func TestAuthorizationRequestRoundTripAndSingleUse(t *testing.T) {
	env := newMCPEnv(t)
	requestHash, bindingHash := sha256Sum(randomMCPSecret(t)), sha256Sum(randomMCPSecret(t))
	record := authorizationRecord(env, requestHash, bindingHash, "client-state")
	if err := withTx(t, env, func(tx identity.MCPTransaction) error { return tx.CreateAuthorizationRequest(env.ctx, record) }); err != nil {
		t.Fatalf("create: %v", err)
	}

	var found identity.MCPAuthorizationRequestRecord
	if err := withTx(t, env, func(tx identity.MCPTransaction) error {
		var e error
		found, e = tx.FindAuthorizationRequest(env.ctx, requestHash, bindingHash, env.clock.now())
		return e
	}); err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.ClientName != "Test MCP client" || found.RedirectURI != mcpTestRedirectURI || found.State != "client-state" ||
		found.CodeChallenge != record.CodeChallenge || found.Resource != mcpTestResource || len(found.Scopes) != 1 {
		t.Fatalf("found=%+v", found)
	}

	var consumed identity.MCPAuthorizationRequestRecord
	if err := withTx(t, env, func(tx identity.MCPTransaction) error {
		var e error
		consumed, e = tx.ConsumeAuthorizationRequest(env.ctx, requestHash, bindingHash, env.clock.now())
		return e
	}); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if consumed.ClientID != mcpTestClientID || consumed.State != "client-state" {
		t.Fatalf("consumed=%+v", consumed)
	}
	if n := countAuthorizationRequests(t, env, requestHash); n != 0 {
		t.Fatalf("consent rows after consume=%d", n)
	}
	err := withTx(t, env, func(tx identity.MCPTransaction) error {
		_, e := tx.ConsumeAuthorizationRequest(env.ctx, requestHash, bindingHash, env.clock.now())
		return e
	})
	if !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("second consume err=%v want ErrNotFound", err)
	}
}

// TestAuthorizationRequestWrongBindingIsNotConsumed proves a wrong binding finds
// and deletes nothing, so the real browser can still decide.
func TestAuthorizationRequestWrongBindingIsNotConsumed(t *testing.T) {
	env := newMCPEnv(t)
	requestHash, bindingHash := sha256Sum(randomMCPSecret(t)), sha256Sum(randomMCPSecret(t))
	record := authorizationRecord(env, requestHash, bindingHash, "")
	if err := withTx(t, env, func(tx identity.MCPTransaction) error { return tx.CreateAuthorizationRequest(env.ctx, record) }); err != nil {
		t.Fatal(err)
	}
	wrong := sha256Sum(randomMCPSecret(t))
	err := withTx(t, env, func(tx identity.MCPTransaction) error {
		_, e := tx.ConsumeAuthorizationRequest(env.ctx, requestHash, wrong, env.clock.now())
		return e
	})
	if !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("wrong-binding consume err=%v want ErrNotFound", err)
	}
	err = withTx(t, env, func(tx identity.MCPTransaction) error {
		_, e := tx.FindAuthorizationRequest(env.ctx, requestHash, wrong, env.clock.now())
		return e
	})
	if !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("wrong-binding find err=%v want ErrNotFound", err)
	}
	if n := countAuthorizationRequests(t, env, requestHash); n != 1 {
		t.Fatalf("wrong binding changed the row: rows=%d want 1", n)
	}
	// The real binding still consumes it afterwards.
	if err := withTx(t, env, func(tx identity.MCPTransaction) error {
		_, e := tx.ConsumeAuthorizationRequest(env.ctx, requestHash, bindingHash, env.clock.now())
		return e
	}); err != nil {
		t.Fatalf("right-binding consume after wrong attempt: %v", err)
	}
}

// TestAuthorizationRequestExpiredIsNotFoundAndNotDeleted proves an expired
// consent is invisible to find and consume, and is left for the sweep.
func TestAuthorizationRequestExpiredIsNotFoundAndNotDeleted(t *testing.T) {
	env := newMCPEnv(t)
	requestHash, bindingHash := sha256Sum(randomMCPSecret(t)), sha256Sum(randomMCPSecret(t))
	record := authorizationRecord(env, requestHash, bindingHash, "")
	if err := withTx(t, env, func(tx identity.MCPTransaction) error { return tx.CreateAuthorizationRequest(env.ctx, record) }); err != nil {
		t.Fatal(err)
	}
	later := record.ExpiresAt.Add(time.Second)
	err := withTx(t, env, func(tx identity.MCPTransaction) error {
		_, e := tx.FindAuthorizationRequest(env.ctx, requestHash, bindingHash, later)
		return e
	})
	if !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("expired find err=%v want ErrNotFound", err)
	}
	err = withTx(t, env, func(tx identity.MCPTransaction) error {
		_, e := tx.ConsumeAuthorizationRequest(env.ctx, requestHash, bindingHash, later)
		return e
	})
	if !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("expired consume err=%v want ErrNotFound", err)
	}
	if n := countAuthorizationRequests(t, env, requestHash); n != 1 {
		t.Fatalf("expired consume deleted the row: rows=%d want 1", n)
	}
}

// TestAuthorizationRequestMissingClientIsInvalidClient proves a consent whose
// client row is gone is refused as an unknown client, not as a storage fault.
func TestAuthorizationRequestMissingClientIsInvalidClient(t *testing.T) {
	env := newMCPEnv(t)
	record := authorizationRecord(env, sha256Sum(randomMCPSecret(t)), sha256Sum(randomMCPSecret(t)), "")
	record.ClientID = "mcp_missing_client_0001"
	err := withTx(t, env, func(tx identity.MCPTransaction) error { return tx.CreateAuthorizationRequest(env.ctx, record) })
	if !errors.Is(err, identity.ErrMCPInvalidClient) {
		t.Fatalf("insert for missing client err=%v want ErrMCPInvalidClient", err)
	}
	if n := countAuthorizationRequests(t, env, record.RequestHash); n != 0 {
		t.Fatalf("refused insert left a row: %d", n)
	}
}

// TestFindTokenGrantOwnerAccessRefreshConsumedAndUnknown proves the revocation
// pre-read names the grant of an access token, of an unused refresh token, and
// of a consumed refresh token; an unknown digest is ErrNotFound.
func TestFindTokenGrantOwnerAccessRefreshConsumedAndUnknown(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_owner_lookup_user")
	code := env.issue(t, user, session)
	tokens, err := env.exchange(code)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	rotated, err := env.refresh(tokens.RefreshToken)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	grant := env.grantOf(t, user).id

	lookup := func(token string) (string, string, error) {
		var userID, grantID string
		err := withTx(t, env, func(tx identity.MCPTransaction) error {
			var e error
			userID, grantID, e = tx.FindTokenGrantOwner(env.ctx, sha256Sum(token))
			return e
		})
		return userID, grantID, err
	}
	for _, tc := range []struct {
		name, token string
	}{
		{"access token", rotated.AccessToken},
		{"unused refresh token", rotated.RefreshToken},
		{"consumed refresh token", tokens.RefreshToken},
	} {
		userID, grantID, err := lookup(tc.token)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if userID != user || grantID != grant {
			t.Fatalf("%s: owner=%s grant=%s want user=%s grant=%s", tc.name, userID, grantID, user, grant)
		}
	}
	if _, _, err := lookup(randomMCPSecret(t)); !errors.Is(err, identity.ErrNotFound) {
		t.Fatalf("unknown token err=%v want ErrNotFound", err)
	}
}

// TestSweepAuthorizationRequestsDeletesOnlyExpired runs the sweep step over one
// expired and one live consent. Only the expired row goes.
func TestSweepAuthorizationRequestsDeletesOnlyExpired(t *testing.T) {
	env := newMCPEnv(t)
	expiredHash, liveHash := sha256Sum(randomMCPSecret(t)), sha256Sum(randomMCPSecret(t))
	// The expired consent runs its full 5-minute lifetime from now. The live one
	// starts 4 minutes later, so it still runs past the sweep cutoff below and
	// stays inside the 10-minute CHECK.
	expired := authorizationRecord(env, expiredHash, sha256Sum(randomMCPSecret(t)), "")
	live := authorizationRecord(env, liveHash, sha256Sum(randomMCPSecret(t)), "")
	live.CreatedAt = expired.CreatedAt.Add(4 * time.Minute)
	live.ExpiresAt = live.CreatedAt.Add(identity.MCPAuthorizationRequestLifetime)
	for _, record := range []identity.MCPAuthorizationRequestRecord{expired, live} {
		if err := withTx(t, env, func(tx identity.MCPTransaction) error { return tx.CreateAuthorizationRequest(env.ctx, record) }); err != nil {
			t.Fatal(err)
		}
	}
	// Cutoff after the expired row's expiry (5m) and before the live row's (9m).
	cutoff := expired.ExpiresAt.Add(time.Minute)
	deleted, err := NewMCPRepository(env.app).SweepStep(env.ctx, identity.MCPSweepAuthorizationRequestsExpired, cutoff)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted=%d want 1", deleted)
	}
	if n := countAuthorizationRequests(t, env, expiredHash); n != 0 {
		t.Fatalf("expired consent survived the sweep")
	}
	if n := countAuthorizationRequests(t, env, liveHash); n != 1 {
		t.Fatalf("live consent deleted by the sweep")
	}
}

// TestSweepKeepsGrantlessClientWithPendingRequest proves a client that holds
// only a pending consent is not swept, even when it is older than the grantless
// lifetime, and that the client goes once the consent is gone.
func TestSweepKeepsGrantlessClientWithPendingRequest(t *testing.T) {
	env := newMCPEnv(t)
	if _, err := env.admin.Exec(env.ctx, `UPDATE mcp_clients SET created_at=$1 WHERE client_id=$2`, env.clock.now().Add(-48*time.Hour), mcpTestClientID); err != nil {
		t.Fatal(err)
	}
	requestHash := sha256Sum(randomMCPSecret(t))
	record := authorizationRecord(env, requestHash, sha256Sum(randomMCPSecret(t)), "")
	if err := withTx(t, env, func(tx identity.MCPTransaction) error { return tx.CreateAuthorizationRequest(env.ctx, record) }); err != nil {
		t.Fatal(err)
	}
	repo := NewMCPRepository(env.app)
	if _, err := repo.SweepStep(env.ctx, identity.MCPSweepGrantlessClients, env.clock.now()); err != nil {
		t.Fatalf("client sweep: %v", err)
	}
	if n := clientRows(t, env, mcpTestClientID); n != 1 {
		t.Fatal("grantless client with a pending consent was swept")
	}
	// Once the consent expires and is swept, the client is grantless again and
	// goes on the next pass.
	if _, err := repo.SweepStep(env.ctx, identity.MCPSweepAuthorizationRequestsExpired, record.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SweepStep(env.ctx, identity.MCPSweepGrantlessClients, env.clock.now()); err != nil {
		t.Fatal(err)
	}
	if n := clientRows(t, env, mcpTestClientID); n != 0 {
		t.Fatal("client with no grant and no consent survived the sweep")
	}
}

func clientRows(t *testing.T, env *mcpEnv, clientID string) int {
	t.Helper()
	var n int
	if err := env.admin.QueryRow(env.ctx, `SELECT count(*) FROM mcp_clients WHERE client_id=$1`, clientID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAuthorizationRequestStateBytesRoundTrip keeps the printable state bytes the
// service admits (space and tilde at the ends of VSCHAR) intact through the row.
func TestAuthorizationRequestStateBytesRoundTrip(t *testing.T) {
	env := newMCPEnv(t)
	requestHash, bindingHash := sha256Sum(randomMCPSecret(t)), sha256Sum(randomMCPSecret(t))
	state := " ~state~ "
	record := authorizationRecord(env, requestHash, bindingHash, state)
	if err := withTx(t, env, func(tx identity.MCPTransaction) error { return tx.CreateAuthorizationRequest(env.ctx, record) }); err != nil {
		t.Fatal(err)
	}
	var got identity.MCPAuthorizationRequestRecord
	if err := withTx(t, env, func(tx identity.MCPTransaction) error {
		var e error
		got, e = tx.ConsumeAuthorizationRequest(env.ctx, requestHash, bindingHash, env.clock.now())
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if got.State != state {
		t.Fatalf("state=%q want %q", got.State, state)
	}
}
