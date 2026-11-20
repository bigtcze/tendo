package postgres

import (
	"net/http"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
)

// requireListRejected asserts the next request is refused with the OAuth
// invalid_token challenge, which is what a revoked or ended grant must see.
func (f *mcpEndpointFixture) requireListRejected(t *testing.T, token, why string) {
	t.Helper()
	reply := f.post(token, mcpEndpointListCall(9))
	if reply.code != http.StatusUnauthorized || reply.body["error"] != "invalid_token" {
		t.Fatalf("%s: status=%d body=%s; want 401 invalid_token", why, reply.code, reply.raw)
	}
}

// TestMCPEndpointRejectsRevokedAndEndedGrantsOnTheNextRequest covers the four
// ways a grant ends: RFC 7009 revocation, password change, account deletion and
// membership removal (which must drop the household from the next list).
func TestMCPEndpointRejectsRevokedAndEndedGrantsOnTheNextRequest(t *testing.T) {
	t.Run("rfc7009 revocation", func(t *testing.T) {
		f := newMCPEndpointFixture(t)
		user := f.newUser()
		f.password(user)
		f.join(user, f.newHousehold("revoke home"), household.RoleAdmin)
		tokens := f.mint(t, user, identityapp.MCPScopeRead)
		if _, err := mcpEndpointParseList(f.post(tokens.AccessToken, mcpEndpointListCall(1))); err != nil {
			t.Fatal(err)
		}
		if err := f.mcp.RevokeToken(f.ctx, identityapp.MCPTokenRevocation{Token: tokens.AccessToken, ClientID: f.clientID}); err != nil {
			t.Fatalf("RevokeToken: %v", err)
		}
		f.requireListRejected(t, tokens.AccessToken, "after revocation")
	})

	t.Run("password change", func(t *testing.T) {
		f := newMCPEndpointFixture(t)
		user := f.newUser()
		f.password(user)
		f.join(user, f.newHousehold("password home"), household.RoleAdmin)
		tokens := f.mint(t, user, identityapp.MCPScopeRead)
		signed, err := f.sessions.Login(f.ctx, f.loginOf(user), mcpEndpointPassword)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.changes.ChangePassword(f.ctx, identityapp.ChangePasswordInput{
			Principal: signed.Principal, SignInMethod: signed.SignInMethod, SessionToken: signed.Token,
			CurrentPassword: mcpEndpointPassword, NewPassword: "a new correct horse",
		}); err != nil {
			t.Fatalf("ChangePassword: %v", err)
		}
		f.requireListRejected(t, tokens.AccessToken, "after password change")
	})

	t.Run("account deletion", func(t *testing.T) {
		f := newMCPEndpointFixture(t)
		actor := f.newUser()
		target := f.newUser()
		f.password(target)
		f.join(actor, f.newHousehold("deletion home"), household.RoleAdmin)
		f.grantInstanceAdmin(actor)
		f.join(target, f.newHousehold("deletion target home"), household.RoleMember)
		tokens := f.mint(t, target, identityapp.MCPScopeRead)
		if err := f.deletions.DeleteAccount(f.ctx, actor, target, f.loginOf(target)); err != nil {
			t.Fatalf("DeleteAccount: %v", err)
		}
		f.requireListRejected(t, tokens.AccessToken, "after account deletion")
	})

	t.Run("membership removal", func(t *testing.T) {
		f := newMCPEndpointFixture(t)
		actor := f.newUser()
		member := f.newUser()
		f.password(member)
		kept := f.newHousehold("kept home")
		removed := f.newHousehold("removed home")
		f.join(actor, kept, household.RoleAdmin)
		f.join(actor, removed, household.RoleAdmin)
		f.join(member, kept, household.RoleMember)
		f.join(member, removed, household.RoleMember)
		tokens := f.mint(t, member, identityapp.MCPScopeRead)
		if err := f.service.RemoveMember(f.ctx, actor, removed, member, 1); err != nil {
			t.Fatalf("RemoveMember: %v", err)
		}
		rows, err := mcpEndpointParseList(f.post(tokens.AccessToken, mcpEndpointListCall(2)))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0]["id"] != kept {
			t.Fatalf("list after removal=%v; want only the kept household", rows)
		}
	})
}
