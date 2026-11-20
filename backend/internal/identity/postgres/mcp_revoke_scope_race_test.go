package postgres

import (
	"errors"
	"sync"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// Service-level tests for RFC 7009 revocation and refresh scope narrowing (ADR
// 0023): the client-initiated revocation of a grant, its race with a refresh,
// and the refused scope subset that leaves the refresh token usable.

// revokeToken revokes a token as the given client.
func (e *mcpEnv) revokeToken(token, clientID string) error {
	return e.svc.RevokeToken(e.ctx, identity.MCPTokenRevocation{Token: token, ClientID: clientID})
}

// revokedEvents counts the 'revoked' audit rows of one grant with a given reason.
func (e *mcpEnv) revokedEvents(t *testing.T, grantID, reason string) int {
	t.Helper()
	var n int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*) FROM mcp_grant_events WHERE grant_id=$1::uuid AND event='revoked' AND reason=$2`,
		grantID, reason).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// requireClientRevoked asserts the grant is revoked with reason 'client', no
// actor, and exactly one 'revoked' event (no 'replay' event either).
func (e *mcpEnv) requireClientRevoked(t *testing.T, user string) {
	t.Helper()
	grant := e.grantOf(t, user)
	if !grant.revoked || grant.revokeReason != "client" {
		t.Fatalf("grant=%+v, want revoked client", grant)
	}
	var revokedAny, byActor int
	if err := e.admin.QueryRow(e.ctx, `SELECT count(*), count(actor_user_id) FROM mcp_grant_events WHERE grant_id=$1::uuid AND event='revoked'`,
		grant.id).Scan(&revokedAny, &byActor); err != nil {
		t.Fatal(err)
	}
	if revokedAny != 1 || byActor != 0 {
		t.Fatalf("revoked events=%d with actor=%d, want exactly one without actor", revokedAny, byActor)
	}
	if n := e.revokedEvents(t, grant.id, "replay"); n != 0 {
		t.Fatalf("replay events=%d, want none", n)
	}
}

// TestMCPRevokeAccessTokenRevokesWholeGrant: revoking an access token revokes
// the grant with reason client, actor NULL, exactly one revoked event, and every
// code and token row of the grant gone.
func TestMCPRevokeAccessTokenRevokesWholeGrant(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_revoke_access")
	tokens, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if err = env.revokeToken(tokens.AccessToken, mcpTestClientID); err != nil {
		t.Fatalf("revoke access token: %v", err)
	}
	env.requireClientRevoked(t, user)
	env.requireNoMCPSecrets(t, user)
	if _, err = env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("revoked access token err=%v, want ErrMCPInvalidToken", err)
	}
}

// TestMCPRevokeConsumedRefreshTokenRevokesWithoutReplay: a consumed refresh
// token still names its grant, so revoking it revokes the grant with reason
// client and no replay event.
func TestMCPRevokeConsumedRefreshTokenRevokesWithoutReplay(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_revoke_consumed")
	first, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if _, err = env.refresh(first.RefreshToken); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err = env.revokeToken(first.RefreshToken, mcpTestClientID); err != nil {
		t.Fatalf("revoke consumed refresh token: %v", err)
	}
	env.requireClientRevoked(t, user)
	env.requireNoMCPSecrets(t, user)
}

// TestMCPRevokeOtherClientLeavesGrantLive: a token presented by a different
// client is a silent success that changes nothing: the grant stays live, with no
// event, and the token still verifies.
func TestMCPRevokeOtherClientLeavesGrantLive(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_revoke_other")
	tokens, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	before := env.grantOf(t, user)
	if err = env.revokeToken(tokens.AccessToken, "mcp_other_client_0001"); err != nil {
		t.Fatalf("foreign-client revoke: %v", err)
	}
	after := env.grantOf(t, user)
	if after.revoked || len(after.events) != len(before.events) {
		t.Fatalf("foreign-client revoke changed the grant: before=%+v after=%+v", before, after)
	}
	if _, err = env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); err != nil {
		t.Fatalf("access token after a foreign-client revoke: %v", err)
	}
}

// TestMCPRevokeRacesRefresh races a revocation against a refresh of the same
// grant, twenty times. Either the refresh fails, or its new pair is dead once the
// revocation commits. Afterwards no live access token of the grant may verify.
func TestMCPRevokeRacesRefresh(t *testing.T) {
	env := newMCPEnv(t)
	for i := 0; i < 20; i++ {
		user, session := env.newAccount(t, mcpRaceLogin("mcp_revoke_race", i))
		first, err := env.exchange(env.issue(t, user, session))
		if err != nil {
			t.Fatalf("iteration %d exchange: %v", i, err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		var refreshed identity.MCPTokens
		var refreshErr, revokeErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			refreshed, refreshErr = env.refresh(first.RefreshToken)
		}()
		go func() {
			defer wg.Done()
			<-start
			revokeErr = env.revokeToken(first.RefreshToken, mcpTestClientID)
		}()
		close(start)
		wg.Wait()
		if revokeErr != nil {
			t.Fatalf("iteration %d: revoke: %v", i, revokeErr)
		}
		if refreshErr != nil && !errors.Is(refreshErr, identity.ErrMCPInvalidGrant) {
			t.Fatalf("iteration %d: refresh: unexpected error %v", i, refreshErr)
		}
		env.requireClientRevoked(t, user)
		env.requireNoMCPSecrets(t, user)
		if refreshErr == nil {
			// The refresh won: its pair must be dead after the revocation.
			if _, err = env.svc.VerifyAccessToken(env.ctx, refreshed.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
				t.Fatalf("iteration %d: access token minted by a refresh that raced a revoke verifies: err=%v", i, err)
			}
		}
	}
}

// TestMCPRefreshScopeSubsetRefusedWithoutConsumingToken: a refresh that names a
// strict subset of the grant's scopes is ErrMCPInvalidScope and does not consume
// the presented refresh token, which still works afterwards with the full set.
func TestMCPRefreshScopeSubsetRefusedWithoutConsumingToken(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_scope_subset")
	first, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	_, err = env.svc.Refresh(env.ctx, identity.MCPRefresh{
		RefreshToken: first.RefreshToken, ClientID: mcpTestClientID, Resource: mcpTestResource,
		Scopes: []string{identity.MCPScopeRead},
	})
	if !errors.Is(err, identity.ErrMCPInvalidScope) {
		t.Fatalf("subset refresh err=%v, want ErrMCPInvalidScope", err)
	}
	var consumed *string
	if err = env.admin.QueryRow(env.ctx, `SELECT consumed_at::text FROM mcp_refresh_tokens WHERE token_hash=$1`, sha256Sum(first.RefreshToken)).Scan(&consumed); err != nil {
		t.Fatal(err)
	}
	if consumed != nil {
		t.Fatal("refused scope subset consumed the refresh token")
	}
	if _, err = env.svc.Refresh(env.ctx, identity.MCPRefresh{
		RefreshToken: first.RefreshToken, ClientID: mcpTestClientID, Resource: mcpTestResource,
		Scopes: []string{identity.MCPScopeWrite, identity.MCPScopeRead},
	}); err != nil {
		t.Fatalf("full-set refresh after the refusal: %v", err)
	}
	if grant := env.grantOf(t, user); grant.revoked {
		t.Fatalf("grant revoked by a refused subset: %+v", grant)
	}
}
