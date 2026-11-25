package postgres

import (
	"errors"
	"slices"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// setPolicy changes stored policy as an administrator would (#457). The HTTP
// API that writes it arrives in slice B1b; enforcement is proven here.
func (e *mcpEnv) setPolicy(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := e.admin.Exec(e.ctx, sql, args...); err != nil {
		t.Fatalf("set policy: %v", err)
	}
}

// TestMCPPolicyEnforcedAgainstPostgres proves on the real schema that every
// policy term is read per request: each switch pauses verification and
// refresh without consuming or revoking, re-enabling restores the same tokens,
// and the installation switch answers as MCP disabled.
func TestMCPPolicyEnforcedAgainstPostgres(t *testing.T) {
	for _, tc := range []struct {
		name, pause, resume string
		disabled            bool
	}{
		{name: "installation off", disabled: true,
			pause:  `UPDATE mcp_installation_policy SET enabled=false WHERE $1::uuid IS NOT NULL`,
			resume: `UPDATE mcp_installation_policy SET enabled=true WHERE $1::uuid IS NOT NULL`},
		{name: "accounts off by default",
			pause:  `UPDATE mcp_installation_policy SET default_account_enabled=false WHERE $1::uuid IS NOT NULL`,
			resume: `UPDATE mcp_installation_policy SET default_account_enabled=true WHERE $1::uuid IS NOT NULL`},
		{name: "admin account off",
			pause:  `UPDATE mcp_account_policies SET admin_enabled=false WHERE user_id=$1::uuid`,
			resume: `UPDATE mcp_account_policies SET admin_enabled=NULL WHERE user_id=$1::uuid`},
		{name: "user off",
			pause:  `UPDATE mcp_account_policies SET user_enabled=false WHERE user_id=$1::uuid`,
			resume: `UPDATE mcp_account_policies SET user_enabled=true WHERE user_id=$1::uuid`},
		{name: "installation row missing", disabled: true,
			pause:  `DELETE FROM mcp_installation_policy WHERE $1::uuid IS NOT NULL`,
			resume: `INSERT INTO mcp_installation_policy(singleton) SELECT true WHERE $1::uuid IS NOT NULL`},
		{name: "account row missing",
			pause:  `DELETE FROM mcp_account_policies WHERE user_id=$1::uuid`,
			resume: `INSERT INTO mcp_account_policies(user_id) VALUES ($1::uuid)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newMCPEnv(t)
			defer env.admin.Exec(env.ctx, `INSERT INTO mcp_installation_policy(singleton) VALUES (true) ON CONFLICT (singleton) DO UPDATE SET enabled=true, default_account_enabled=true`) //nolint:errcheck // best-effort reset of the shared singleton
			user, session := env.newAccount(t, "mcp_policy_pause")
			tokens, err := env.exchange(env.issue(t, user, session))
			if err != nil {
				t.Fatal(err)
			}
			pendingCode := env.issue(t, user, session)

			env.setPolicy(t, tc.pause, user)
			wantVerify, wantGrant := identity.ErrMCPInvalidToken, identity.ErrMCPInvalidGrant
			if tc.disabled {
				wantVerify, wantGrant = identity.ErrMCPDisabled, identity.ErrMCPDisabled
			}
			if _, err := env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, wantVerify) {
				t.Fatalf("verify while paused err=%v want %v", err, wantVerify)
			}
			if _, err := env.refresh(tokens.RefreshToken); !errors.Is(err, wantGrant) {
				t.Fatalf("refresh while paused err=%v want %v", err, wantGrant)
			}
			if _, err := env.exchange(pendingCode); !errors.Is(err, wantGrant) {
				t.Fatalf("exchange while paused err=%v want %v", err, wantGrant)
			}
			g := env.grantOf(t, user)
			if g.revoked {
				t.Fatalf("pausing revoked the grant: %+v", g)
			}
			var consumed, used int
			if err := env.admin.QueryRow(env.ctx, `SELECT
				(SELECT count(*) FROM mcp_refresh_tokens r JOIN mcp_grants g ON g.id=r.grant_id WHERE g.user_id=$1::uuid AND r.consumed_at IS NOT NULL),
				(SELECT count(*) FROM mcp_authorization_codes WHERE user_id=$1::uuid AND used_at IS NOT NULL)`, user).Scan(&consumed, &used); err != nil {
				t.Fatal(err)
			}
			if consumed != 0 || used != 1 {
				t.Fatalf("paused refresh consumed=%d, used codes=%d (want 0 and only the first)", consumed, used)
			}

			env.setPolicy(t, tc.resume, user)
			access, err := env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken)
			if err != nil || !slices.Equal(access.Scopes, []string{identity.MCPScopeRead, identity.MCPScopeWrite}) {
				t.Fatalf("verify after resume: %+v %v", access, err)
			}
			if _, err := env.refresh(tokens.RefreshToken); err != nil {
				t.Fatalf("refresh after resume: %v", err)
			}
			if _, err := env.exchange(pendingCode); err != nil {
				t.Fatalf("exchange after resume: %v", err)
			}
		})
	}
}

// TestMCPPolicyNarrowsScopesAgainstPostgres proves the stored maximum and user
// scopes intersect the grant on verify, refresh and issuance, and a user
// setting cannot widen past the administrator's maximum.
func TestMCPPolicyNarrowsScopesAgainstPostgres(t *testing.T) {
	env := newMCPEnv(t)
	defer env.admin.Exec(env.ctx, `UPDATE mcp_installation_policy SET default_max_scopes=ARRAY['tendo:read','tendo:write']`) //nolint:errcheck // best-effort reset of the shared singleton
	user, session := env.newAccount(t, "mcp_policy_narrow")
	tokens, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatal(err)
	}
	read := []string{identity.MCPScopeRead}
	write := []string{identity.MCPScopeWrite}

	env.setPolicy(t, `UPDATE mcp_account_policies SET admin_max_scopes=ARRAY['tendo:read'], user_scopes=ARRAY['tendo:read','tendo:write'] WHERE user_id=$1::uuid`, user)
	if access, err := env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); err != nil || !slices.Equal(access.Scopes, read) || !slices.Equal(access.Allowed, read) {
		t.Fatalf("admin maximum read: %+v %v", access, err)
	}
	rotated, err := env.refresh(tokens.RefreshToken)
	if err != nil || !slices.Equal(rotated.Scopes, read) {
		t.Fatalf("refresh under admin maximum: %v %v", rotated.Scopes, err)
	}
	var stored []string
	if err := env.admin.QueryRow(env.ctx, `SELECT scopes FROM mcp_grants WHERE user_id=$1::uuid`, user).Scan(&stored); err != nil || len(stored) != 2 {
		t.Fatalf("grant scopes changed: %v %v", stored, err)
	}

	env.setPolicy(t, `UPDATE mcp_account_policies SET admin_max_scopes=NULL, user_scopes=ARRAY['tendo:write'] WHERE user_id=$1::uuid`, user)
	if access, err := env.svc.VerifyAccessToken(env.ctx, rotated.AccessToken); err != nil || !slices.Equal(access.Scopes, write) || !slices.Equal(access.Allowed, write) {
		t.Fatalf("user narrows to write: %+v %v", access, err)
	}

	env.setPolicy(t, `UPDATE mcp_account_policies SET user_scopes=ARRAY['tendo:read','tendo:write'] WHERE user_id=$1::uuid`, user)
	env.setPolicy(t, `UPDATE mcp_installation_policy SET default_max_scopes=ARRAY['tendo:write'] WHERE $1::uuid IS NOT NULL`, user)
	code := env.issue(t, user, session)
	var codeScopes []string
	if err := env.admin.QueryRow(env.ctx, `SELECT scopes FROM mcp_authorization_codes WHERE user_id=$1::uuid AND used_at IS NULL`, user).Scan(&codeScopes); err != nil || !slices.Equal(codeScopes, write) {
		t.Fatalf("issued code scopes=%v err=%v want write only", codeScopes, err)
	}
	if next, err := env.exchange(code); err != nil || !slices.Equal(next.Scopes, write) {
		t.Fatalf("exchange under default maximum: %v %v", next.Scopes, err)
	}
}

// TestMCPAdminRevocationDeniesNextRequest proves the new 'admin' reason is
// stored and audited with the administrator as actor, and the revoked grant's
// access and refresh tokens are refused at once.
func TestMCPAdminRevocationDeniesNextRequest(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_policy_admin_revoke_user")
	operator, _ := env.newAccount(t, "mcp_policy_admin_revoke_operator")
	tokens, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatal(err)
	}
	grantID := env.grantOf(t, user).id
	if err := withTx(t, env, func(tx identity.MCPTransaction) error {
		return tx.RevokeGrant(env.ctx, grantID, env.clock.now(), identity.MCPRevocation{Reason: identity.MCPRevokeAdmin, ActorUserID: operator})
	}); err != nil {
		t.Fatalf("admin revocation: %v", err)
	}
	g := env.grantOf(t, user)
	if !g.revoked || g.revokeReason != "admin" || !slices.Contains(g.events, "revoked:admin:"+operator) {
		t.Fatalf("grant after admin revocation: %+v", g)
	}
	if _, err := env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); !errors.Is(err, identity.ErrMCPInvalidToken) {
		t.Fatalf("verify after admin revocation err=%v", err)
	}
	if _, err := env.refresh(tokens.RefreshToken); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("refresh after admin revocation err=%v", err)
	}
}

// TestMCPConsentDeniedWhenNothingAllowedAgainstPostgres: with no requested
// scope allowed, an approval redirects with access_denied, consumes the pending
// request and stores no code.
func TestMCPConsentDeniedWhenNothingAllowedAgainstPostgres(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_policy_consent")
	env.setPolicy(t, `UPDATE mcp_account_policies SET user_enabled=false WHERE user_id=$1::uuid`, user)
	pending := env.begin(t, mcpTestRedirectURI, "s1")
	result, err := env.decide(user, session, pending, true)
	if err != nil || result.Code != "" || result.RedirectURI != mcpTestRedirectURI || result.State != "s1" {
		t.Fatalf("approval while off: %+v %v", result, err)
	}
	var codes, requests int
	if err := env.admin.QueryRow(env.ctx, `SELECT
		(SELECT count(*) FROM mcp_authorization_codes WHERE user_id=$1::uuid),
		(SELECT count(*) FROM mcp_authorization_requests)`, user).Scan(&codes, &requests); err != nil {
		t.Fatal(err)
	}
	if codes != 0 || requests != 0 {
		t.Fatalf("codes=%d pending requests=%d want 0 and 0", codes, requests)
	}
	if _, err := env.decide(user, session, pending, true); !errors.Is(err, identity.ErrMCPAuthorizationRequestNotFound) {
		t.Fatalf("second decision err=%v want the request consumed", err)
	}
}

// TestMCPConsentViewShowsAllowedScopesAgainstPostgres: the consent view names
// the requested scopes the signed-in account's stored policy allows, read
// fresh on every view: the administrator's maximum and the owner's own
// settings narrow it, switching off empties it, and another account reading
// the same request sees its own policy. The approval never grants more than
// the view showed, even after the policy widened.
func TestMCPConsentViewShowsAllowedScopesAgainstPostgres(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_policy_view")
	other, _ := env.newAccount(t, "mcp_policy_view_other")
	pending := env.begin(t, mcpTestRedirectURI, "s1")
	view := func(account string) []string {
		t.Helper()
		v, err := env.svc.GetAuthorizationRequest(env.ctx, account, pending.Handle, pending.BrowserBinding)
		if err != nil {
			t.Fatalf("GetAuthorizationRequest: %v", err)
		}
		if !slices.Equal(v.Scopes, []string{identity.MCPScopeRead, identity.MCPScopeWrite}) {
			t.Fatalf("requested scopes=%v", v.Scopes)
		}
		return v.AllowedScopes
	}
	if got := view(user); !slices.Equal(got, []string{identity.MCPScopeRead, identity.MCPScopeWrite}) {
		t.Fatalf("default allowed=%v", got)
	}
	env.setPolicy(t, `UPDATE mcp_account_policies SET admin_max_scopes=ARRAY['tendo:read'] WHERE user_id=$1::uuid`, user)
	if got := view(user); !slices.Equal(got, []string{identity.MCPScopeRead}) {
		t.Fatalf("admin maximum read: allowed=%v", got)
	}
	if got := view(other); !slices.Equal(got, []string{identity.MCPScopeRead, identity.MCPScopeWrite}) {
		t.Fatalf("another account read the first account's policy: allowed=%v", got)
	}
	env.setPolicy(t, `UPDATE mcp_account_policies SET admin_max_scopes=NULL, user_scopes=ARRAY['tendo:write'] WHERE user_id=$1::uuid`, user)
	if got := view(user); !slices.Equal(got, []string{identity.MCPScopeWrite}) {
		t.Fatalf("own settings write: allowed=%v", got)
	}
	env.setPolicy(t, `UPDATE mcp_account_policies SET user_enabled=false WHERE user_id=$1::uuid`, user)
	if got := view(user); len(got) != 0 {
		t.Fatalf("own switch off: allowed=%v want none", got)
	}
	env.setPolicy(t, `UPDATE mcp_account_policies SET user_enabled=true WHERE user_id=$1::uuid`, user)
	shown := view(user)
	// The owner widens their settings in another tab before answering: Allow
	// still grants only what the screen showed.
	env.setPolicy(t, `UPDATE mcp_account_policies SET user_scopes=ARRAY['tendo:read','tendo:write'] WHERE user_id=$1::uuid`, user)
	result, err := env.svc.DecideAuthorization(env.ctx, identity.MCPDecision{
		Principal: identity.Principal{UserID: user}, SessionToken: session,
		Handle: pending.Handle, BrowserBinding: pending.BrowserBinding, Approve: true, ShownScopes: shown,
	})
	if err != nil || result.Code == "" {
		t.Fatalf("approve: %+v %v", result, err)
	}
	var scopes []string
	if err := env.admin.QueryRow(env.ctx, `SELECT scopes FROM mcp_authorization_codes WHERE user_id=$1::uuid`, user).Scan(&scopes); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(scopes, []string{identity.MCPScopeWrite}) {
		t.Fatalf("code scopes=%v want what the view showed, not the widened policy", scopes)
	}
}

// TestMCPRefreshAcceptsTheAnsweredScopeAgainstPostgres: after the policy
// narrows a grant, a refresh naming the scope the last response returned
// succeeds; another subset is refused without consuming the token.
func TestMCPRefreshAcceptsTheAnsweredScopeAgainstPostgres(t *testing.T) {
	env := newMCPEnv(t)
	user, session := env.newAccount(t, "mcp_policy_refresh_scope")
	tokens, err := env.exchange(env.issue(t, user, session))
	if err != nil {
		t.Fatal(err)
	}
	env.setPolicy(t, `UPDATE mcp_account_policies SET user_scopes=ARRAY['tendo:read'] WHERE user_id=$1::uuid`, user)
	refreshWith := func(token string, scopes ...string) (identity.MCPTokens, error) {
		return env.svc.Refresh(env.ctx, identity.MCPRefresh{RefreshToken: token, ClientID: mcpTestClientID, Resource: mcpTestResource, Scopes: scopes})
	}
	if _, err := refreshWith(tokens.RefreshToken, identity.MCPScopeWrite); !errors.Is(err, identity.ErrMCPInvalidScope) {
		t.Fatalf("other subset err=%v want ErrMCPInvalidScope", err)
	}
	next, err := refreshWith(tokens.RefreshToken, identity.MCPScopeRead)
	if err != nil || !slices.Equal(next.Scopes, []string{identity.MCPScopeRead}) {
		t.Fatalf("answered scope: %v %v", next.Scopes, err)
	}
}
