package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// Service-level real-DB tests for the A3a authorization flow (ADR 0023): the
// consent request, the decision, the code exchange with its loopback port rule,
// and the revocation. They run over the runtime pool, as production does.

const (
	mcpLoopbackRegistered = "http://127.0.0.1/cb"
	mcpLoopbackRequested  = "http://127.0.0.1:53211/cb"
	mcpLoopbackOtherPort  = "http://127.0.0.1:53212/cb"
)

// useLoopbackClient registers the port-free loopback redirect on the test client.
func (e *mcpEnv) useLoopbackClient(t *testing.T) {
	t.Helper()
	if _, err := e.admin.Exec(e.ctx, `UPDATE mcp_clients SET redirect_uris=ARRAY[$1]::text[] WHERE client_id=$2`, mcpLoopbackRegistered, mcpTestClientID); err != nil {
		t.Fatal(err)
	}
}

// authorizeParams is a valid authorization request for the test client.
func authorizeParams(redirect, state string) identity.MCPAuthorizationParams {
	return identity.MCPAuthorizationParams{
		ClientID: mcpTestClientID, RedirectURI: redirect, CodeChallenge: mcpTestChallenge(),
		CodeChallengeMethod: "S256", Resource: mcpTestResource, State: state,
	}
}

// begin stores a pending consent for redirect and returns it.
func (e *mcpEnv) begin(t *testing.T, redirect, state string) identity.MCPPendingAuthorization {
	t.Helper()
	pending, err := e.svc.BeginAuthorization(e.ctx, authorizeParams(redirect, state))
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	return pending
}

// decide answers a pending consent as the signed-in user from the consenting browser.
func (e *mcpEnv) decide(user, session string, pending identity.MCPPendingAuthorization, approve bool) (identity.MCPDecisionResult, error) {
	return e.svc.DecideAuthorization(e.ctx, identity.MCPDecision{
		Principal: identity.Principal{UserID: user}, SessionToken: session,
		Handle: pending.Handle, BrowserBinding: pending.BrowserBinding, Approve: approve,
	})
}

// exchangeAt redeems a code presenting the given redirect URI.
func (e *mcpEnv) exchangeAt(code, redirect string) (identity.MCPTokens, error) {
	return e.svc.ExchangeCode(e.ctx, identity.MCPCodeExchange{
		Code: code, CodeVerifier: mcpTestVerifier, ClientID: mcpTestClientID,
		RedirectURI: redirect, Resource: mcpTestResource,
	})
}

// raceDecisions runs two decisions on one handle from a shared start gate.
func (e *mcpEnv) raceDecisions(user, session string, pending identity.MCPPendingAuthorization, first, second bool) ([2]identity.MCPDecisionResult, [2]error) {
	var results [2]identity.MCPDecisionResult
	var errs [2]error
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, approve := range []bool{first, second} {
		wg.Add(1)
		go func(i int, approve bool) {
			defer wg.Done()
			<-start
			results[i], errs[i] = e.decide(user, session, pending, approve)
		}(i, approve)
	}
	close(start)
	wg.Wait()
	return results, errs
}

// TestMCPAuthorizeLoopbackPortHappyPath: a loopback registration matches a
// requested port. The code is bound to the requested URI, so an exchange with a
// different port is refused and the code stays redeemable with the right one.
func TestMCPAuthorizeLoopbackPortHappyPath(t *testing.T) {
	env := newMCPEnv(t)
	env.useLoopbackClient(t)
	user, session := env.newAccount(t, "mcp_auth_happy")
	pending := env.begin(t, mcpLoopbackRequested, "state-happy")

	view, err := env.svc.GetAuthorizationRequest(env.ctx, user, pending.Handle, pending.BrowserBinding)
	if err != nil {
		t.Fatalf("GetAuthorizationRequest: %v", err)
	}
	if view.ClientName != "Test MCP client" || view.RedirectURI != mcpLoopbackRequested || len(view.Scopes) != 2 {
		t.Fatalf("view=%+v", view)
	}
	result, err := env.decide(user, session, pending, true)
	if err != nil || result.Code == "" || result.RedirectURI != mcpLoopbackRequested || result.State != "state-happy" {
		t.Fatalf("approve result=%+v err=%v", result, err)
	}
	if _, err = env.exchangeAt(result.Code, mcpLoopbackOtherPort); !errors.Is(err, identity.ErrMCPInvalidGrant) {
		t.Fatalf("exchange with another port err=%v, want ErrMCPInvalidGrant", err)
	}
	tokens, err := env.exchangeAt(result.Code, mcpLoopbackRequested)
	if err != nil {
		t.Fatalf("exchange with the requested port: %v", err)
	}
	if _, err = env.svc.VerifyAccessToken(env.ctx, tokens.AccessToken); err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if grant := env.grantOf(t, user); grant.revoked {
		t.Fatalf("grant revoked by a refused exchange: %+v", grant)
	}
}

// TestMCPAuthorizeConcurrentDecisionsExactlyOneWinner races two decisions on one
// handle, twenty times, alternating approve+approve and approve+deny. Exactly one
// succeeds, the other is ErrMCPAuthorizationRequestNotFound, the consent row is
// gone, and a code exists only when the approval won.
func TestMCPAuthorizeConcurrentDecisionsExactlyOneWinner(t *testing.T) {
	env := newMCPEnv(t)
	for i := 0; i < 20; i++ {
		user, session := env.newAccount(t, mcpRaceLogin("mcp_auth_race", i))
		pending := env.begin(t, mcpTestRedirectURI, "")
		second := i%2 == 0 // even: approve+approve; odd: approve+deny
		results, errs := env.raceDecisions(user, session, pending, true, second)

		wins, losses := 0, 0
		var winner identity.MCPDecisionResult
		var winnerApproved bool
		for j, err := range errs {
			switch {
			case err == nil:
				wins++
				winner = results[j]
				winnerApproved = j == 0 || second
			case errors.Is(err, identity.ErrMCPAuthorizationRequestNotFound):
				losses++
			default:
				t.Fatalf("iteration %d: unexpected decision error: %v", i, err)
			}
		}
		if wins != 1 || losses != 1 {
			t.Fatalf("iteration %d: wins=%d losses=%d, want 1 and 1", i, wins, losses)
		}
		if n := countAuthorizationRequests(t, env, sha256Sum(pending.Handle)); n != 0 {
			t.Fatalf("iteration %d: consent row survived the decision", i)
		}
		c := env.counts(t, user)
		if winnerApproved && (c.codes != 1 || winner.Code == "") {
			t.Fatalf("iteration %d: approval won but codes=%d code=%q", i, c.codes, winner.Code)
		}
		if !winnerApproved && (c.codes != 0 || winner.Code != "") {
			t.Fatalf("iteration %d: denial won but codes=%d code=%q", i, c.codes, winner.Code)
		}
	}
}

// mcpRaceLogin builds a unique login per race iteration.
func mcpRaceLogin(prefix string, i int) string {
	return prefix + "_" + string(rune('a'+i/10)) + string(rune('0'+i%10))
}

// TestMCPAuthorizeExpiredAndDeniedRequestsAreNotFound: an expired consent is not
// found, mints no code, and is left for the sweep. A denied consent is consumed,
// so a second decision is not found.
func TestMCPAuthorizeExpiredAndDeniedRequestsAreNotFound(t *testing.T) {
	t.Run("expired", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_auth_expired")
		pending := env.begin(t, mcpTestRedirectURI, "")
		env.clock.advance(identity.MCPAuthorizationRequestLifetime + time.Second)
		if _, err := env.decide(user, session, pending, true); !errors.Is(err, identity.ErrMCPAuthorizationRequestNotFound) {
			t.Fatalf("decide on an expired consent err=%v, want ErrMCPAuthorizationRequestNotFound", err)
		}
		env.requireNoMCPSecrets(t, user)
		if n := countAuthorizationRequests(t, env, sha256Sum(pending.Handle)); n != 1 {
			t.Fatalf("expired consent rows=%d, want 1 left for the sweep", n)
		}
	})

	t.Run("denied then decided again", func(t *testing.T) {
		env := newMCPEnv(t)
		user, session := env.newAccount(t, "mcp_auth_denied")
		pending := env.begin(t, mcpTestRedirectURI, "s")
		result, err := env.decide(user, session, pending, false)
		if err != nil || result.Code != "" || result.RedirectURI != mcpTestRedirectURI || result.State != "s" {
			t.Fatalf("deny result=%+v err=%v", result, err)
		}
		if _, err = env.decide(user, session, pending, true); !errors.Is(err, identity.ErrMCPAuthorizationRequestNotFound) {
			t.Fatalf("second decide err=%v, want ErrMCPAuthorizationRequestNotFound", err)
		}
		env.requireNoMCPSecrets(t, user)
		if n := countAuthorizationRequests(t, env, sha256Sum(pending.Handle)); n != 0 {
			t.Fatalf("denied consent rows=%d, want 0", n)
		}
	})
}
