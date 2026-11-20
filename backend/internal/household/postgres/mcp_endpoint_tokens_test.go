package postgres

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
)

// TestMCPEndpointRejectsExpiredAndForeignResourceTokens covers an access token
// past its ten-minute lifetime and a token minted for a different resource.
func TestMCPEndpointRejectsExpiredAndForeignResourceTokens(t *testing.T) {
	t.Run("expired access token", func(t *testing.T) {
		f := newMCPEndpointFixture(t)
		user := f.newUser()
		f.password(user)
		f.join(user, f.newHousehold("expiry home"), household.RoleAdmin)
		tokens := f.mint(t, user, identityapp.MCPScopeRead)
		f.clock.at = f.clock.at.Add(identityapp.MCPAccessTokenLifetime + time.Second)
		f.requireListRejected(t, tokens.AccessToken, "after access lifetime")
	})

	t.Run("token for another resource", func(t *testing.T) {
		f := newMCPEndpointFixture(t)
		user := f.newUser()
		f.password(user)
		f.join(user, f.newHousehold("resource home"), household.RoleAdmin)
		otherResource := "https://other.test/mcp"
		other, err := identityapp.NewMCPService(identitypostgres.NewMCPRepository(f.app), identityapp.MCPOptions{
			Enabled: true, Resource: otherResource,
		}, f.clock.now)
		if err != nil {
			t.Fatal(err)
		}
		foreign := f.mintVia(t, other, otherResource, user, identityapp.MCPScopeRead)
		f.requireListRejected(t, foreign.AccessToken, "foreign resource token at the canonical endpoint")
	})
}

// TestMCPEndpointRejectsNonAccessTokensAsBearer presents every other credential
// kind as the bearer token. Each must be a 401 invalid_token, never a session.
func TestMCPEndpointRejectsNonAccessTokensAsBearer(t *testing.T) {
	f := newMCPEndpointFixture(t)
	user := f.newUser()
	f.password(user)
	f.join(user, f.newHousehold("token kinds home"), household.RoleAdmin)
	tokens := f.mint(t, user, identityapp.MCPScopeRead)
	signed, err := f.sessions.Login(f.ctx, f.loginOf(user), mcpEndpointPassword)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh authorization code, never exchanged, is still a code.
	pending, err := f.mcp.BeginAuthorization(f.ctx, identityapp.MCPAuthorizationParams{
		ClientID: f.clientID, RedirectURI: mcpEndpointRedirect, CodeChallenge: mcpEndpointChallenge(),
		CodeChallengeMethod: "S256", Resource: mcpEndpointResource,
	})
	if err != nil {
		t.Fatal(err)
	}
	decided, err := f.mcp.DecideAuthorization(f.ctx, identityapp.MCPDecision{
		Principal: signed.Principal, SessionToken: signed.Token,
		Handle: pending.Handle, BrowserBinding: pending.BrowserBinding, Approve: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"refresh token":      tokens.RefreshToken,
		"authorization code": decided.Code,
		"session token":      signed.Token,
		"setup-token shape":  strings.Repeat("A", 43) + "=",
		"jwt-shaped":         "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ4In0.c2lnbmF0dXJl",
	}
	for name, token := range cases {
		reply := f.post(token, mcpEndpointListCall(4))
		if reply.code != http.StatusUnauthorized || reply.body["error"] != "invalid_token" {
			t.Fatalf("%s as bearer: status=%d body=%s; want 401 invalid_token", name, reply.code, reply.raw)
		}
	}
}

// TestMCPEndpointRefusesReadToolWithWriteOnlyGrant holds a real grant with only
// tendo:write. The front check must answer 403 insufficient_scope.
func TestMCPEndpointRefusesReadToolWithWriteOnlyGrant(t *testing.T) {
	f := newMCPEndpointFixture(t)
	user := f.newUser()
	f.password(user)
	f.join(user, f.newHousehold("write home"), household.RoleAdmin)
	tokens := f.mint(t, user, identityapp.MCPScopeWrite)
	reply := f.post(tokens.AccessToken, mcpEndpointListCall(5))
	if reply.code != http.StatusForbidden || reply.body["error"] != "insufficient_scope" {
		t.Fatalf("write-only grant: status=%d body=%s; want 403 insufficient_scope", reply.code, reply.raw)
	}
}
