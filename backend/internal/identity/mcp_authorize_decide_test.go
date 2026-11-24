package identity

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// mcpBeginOK stores a default pending consent and returns its plaintext pair.
func mcpBeginOK(t *testing.T, fx *mcpFixture) MCPPendingAuthorization {
	t.Helper()
	pending, err := fx.svc.BeginAuthorization(context.Background(), mcpValidParams())
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	return pending
}

func mcpDecisionOf(p MCPPendingAuthorization, approve bool) MCPDecision {
	return MCPDecision{
		Principal: Principal{UserID: mcpTestUser, Login: "owner"}, SessionToken: mcpTestSession,
		Handle: p.Handle, BrowserBinding: p.BrowserBinding, Approve: approve,
	}
}

func TestMCPGetAuthorizationRequestView(t *testing.T) {
	fx := newMCPFixture(t)
	pending := mcpBeginOK(t, fx)
	fx.f.calls = nil
	view, err := fx.svc.GetAuthorizationRequest(context.Background(), pending.Handle, pending.BrowserBinding)
	if err != nil {
		t.Fatalf("GetAuthorizationRequest: %v", err)
	}
	if view.ClientName != "Test client" || view.RedirectURI != mcpTestRedirect ||
		!slices.Equal(view.Scopes, []string{MCPScopeRead}) || !view.ExpiresAt.Equal(pending.ExpiresAt) {
		t.Fatalf("view = %+v", view)
	}
	if fx.f.requestCount() != 1 {
		t.Fatal("reading the consent consumed it")
	}
	if want := []string{"WithMCPTransaction", "FindAuthorizationRequest"}; !slices.Equal(fx.f.calls, want) {
		t.Fatalf("calls = %v, want %v", fx.f.calls, want)
	}
}

func TestMCPGetAuthorizationRequestNotFound(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		handle  func(MCPPendingAuthorization) string
		binding func(MCPPendingAuthorization) string
		clock   time.Duration
		noDB    bool
	}{
		{name: "wrong binding", handle: func(p MCPPendingAuthorization) string { return p.Handle },
			binding: func(p MCPPendingAuthorization) string { return strings.Repeat("B", 43) }},
		{name: "unknown handle", handle: func(MCPPendingAuthorization) string { return strings.Repeat("C", 43) },
			binding: func(p MCPPendingAuthorization) string { return p.BrowserBinding }},
		{name: "expired one microsecond late", handle: func(p MCPPendingAuthorization) string { return p.Handle },
			binding: func(p MCPPendingAuthorization) string { return p.BrowserBinding }, clock: MCPAuthorizationRequestLifetime + time.Microsecond},
		{name: "expired exactly at expiry", handle: func(p MCPPendingAuthorization) string { return p.Handle },
			binding: func(p MCPPendingAuthorization) string { return p.BrowserBinding }, clock: MCPAuthorizationRequestLifetime},
		{name: "malformed handle", handle: func(MCPPendingAuthorization) string { return "short" },
			binding: func(p MCPPendingAuthorization) string { return p.BrowserBinding }, noDB: true},
		{name: "malformed binding", handle: func(p MCPPendingAuthorization) string { return p.Handle },
			binding: func(MCPPendingAuthorization) string { return strings.Repeat("+", 43) }, noDB: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			pending := mcpBeginOK(t, fx)
			fx.f.now = mcpTestStart.Add(tt.clock)
			fx.f.calls = nil
			_, err := fx.svc.GetAuthorizationRequest(ctx, tt.handle(pending), tt.binding(pending))
			if !errors.Is(err, ErrMCPAuthorizationRequestNotFound) {
				t.Fatalf("err = %v, want ErrMCPAuthorizationRequestNotFound", err)
			}
			if tt.noDB && len(fx.f.calls) != 0 {
				t.Fatalf("malformed input touched the repository: %v", fx.f.calls)
			}
		})
	}
}

func TestMCPDecideApproveIssuesRedeemableCode(t *testing.T) {
	fx := newMCPFixture(t)
	pending := mcpBeginOK(t, fx)
	fx.f.calls = nil
	result, err := fx.svc.DecideAuthorization(context.Background(), mcpDecisionOf(pending, true))
	if err != nil {
		t.Fatalf("DecideAuthorization: %v", err)
	}
	want := []string{"WithMCPTransaction", "LockLiveAccount", "ActiveSessionExists", "FindAuthorizationRequest",
		"FindClient", "ConsumeAuthorizationRequest", "ReadPolicy", "CreateAuthorizationCode"}
	if !slices.Equal(fx.f.calls, want) {
		t.Fatalf("calls = %v, want %v", fx.f.calls, want)
	}
	if result.RedirectURI != mcpTestRedirect || result.State != "state-1" || result.Code == "" {
		t.Fatalf("result = %+v", result)
	}
	if fx.f.requestCount() != 0 {
		t.Fatal("approval did not consume the pending consent")
	}
	if _, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(result.Code)); err != nil {
		t.Fatalf("ExchangeCode of the approved code: %v", err)
	}
}

// TestMCPDecideApproveUsesLoopbackPortOfRequest pins that the code carries the
// redirect the consent was made with, and that the token endpoint still refuses
// a different port.
func TestMCPDecideApproveUsesLoopbackPortOfRequest(t *testing.T) {
	fx := newMCPFixture(t)
	fx.f.clients[mcpTestClient] = MCPClient{ID: mcpTestClient, Name: "Loopback", RedirectURIs: []string{mcpTestLoopbackRedirect}}
	in := mcpValidParams()
	in.RedirectURI = "http://127.0.0.1:54123/callback"
	pending, err := fx.svc.BeginAuthorization(context.Background(), in)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	result, err := fx.svc.DecideAuthorization(context.Background(), mcpDecisionOf(pending, true))
	if err != nil {
		t.Fatalf("DecideAuthorization: %v", err)
	}
	if result.RedirectURI != "http://127.0.0.1:54123/callback" {
		t.Fatalf("redirect = %q, want the consent's port", result.RedirectURI)
	}
	other := mcpValidExchange(result.Code)
	other.RedirectURI = "http://127.0.0.1:9000/callback"
	if _, err := fx.svc.ExchangeCode(context.Background(), other); !errors.Is(err, ErrMCPInvalidGrant) {
		t.Fatalf("exchange at a different port: err = %v, want ErrMCPInvalidGrant", err)
	}
	exact := mcpValidExchange(result.Code)
	exact.RedirectURI = in.RedirectURI
	if _, err := fx.svc.ExchangeCode(context.Background(), exact); err != nil {
		t.Fatalf("exchange at the consent's redirect: %v", err)
	}
}

func TestMCPDecideDenyConsumesAndIssuesNoCode(t *testing.T) {
	fx := newMCPFixture(t)
	pending := mcpBeginOK(t, fx)
	fx.f.calls = nil
	result, err := fx.svc.DecideAuthorization(context.Background(), mcpDecisionOf(pending, false))
	if err != nil {
		t.Fatalf("DecideAuthorization deny: %v", err)
	}
	if result.Code != "" || result.RedirectURI != mcpTestRedirect || result.State != "state-1" {
		t.Fatalf("deny result = %+v", result)
	}
	if slices.Contains(fx.f.calls, "CreateAuthorizationCode") {
		t.Fatalf("deny created a code: %v", fx.f.calls)
	}
	if len(fx.f.state.codes) != 0 {
		t.Fatalf("deny stored %d codes", len(fx.f.state.codes))
	}
	if fx.f.requestCount() != 0 {
		t.Fatal("deny did not consume the pending consent")
	}
}

func TestMCPDecideSecondDecisionNotFound(t *testing.T) {
	for _, approve := range []bool{true, false} {
		fx := newMCPFixture(t)
		pending := mcpBeginOK(t, fx)
		if _, err := fx.svc.DecideAuthorization(context.Background(), mcpDecisionOf(pending, true)); err != nil {
			t.Fatalf("first decision: %v", err)
		}
		_, err := fx.svc.DecideAuthorization(context.Background(), mcpDecisionOf(pending, approve))
		if !errors.Is(err, ErrMCPAuthorizationRequestNotFound) {
			t.Fatalf("second decision (approve=%v) err = %v, want ErrMCPAuthorizationRequestNotFound", approve, err)
		}
	}
}

// TestMCPDecideRefusalsConsumeNothing pins that every refusal rolls back, so
// the consent survives for its rightful browser.
func TestMCPDecideRefusalsConsumeNothing(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(*mcpFixture)
		mutate func(*MCPDecision)
		want   error
	}{
		{name: "no session", setup: func(fx *mcpFixture) { fx.f.sessionActive = false }, want: ErrUnauthenticated},
		{name: "deleted account", setup: func(fx *mcpFixture) { fx.f.accounts[mcpTestUser] = false }, want: ErrUnauthenticated},
		{name: "wrong binding", mutate: func(d *MCPDecision) { d.BrowserBinding = strings.Repeat("B", 43) }, want: ErrMCPAuthorizationRequestNotFound},
		{name: "client registration dropped", setup: func(fx *mcpFixture) {
			c := fx.f.clients[mcpTestClient]
			c.RedirectURIs = nil
			fx.f.clients[mcpTestClient] = c
		}, want: ErrMCPAuthorizationRequestNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			pending := mcpBeginOK(t, fx)
			if tt.setup != nil {
				tt.setup(fx)
			}
			in := mcpDecisionOf(pending, true)
			if tt.mutate != nil {
				tt.mutate(&in)
			}
			_, err := fx.svc.DecideAuthorization(context.Background(), in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if fx.f.requestCount() != 1 {
				t.Fatalf("refusal consumed the consent: %d left", fx.f.requestCount())
			}
			if len(fx.f.state.codes) != 0 {
				t.Fatalf("refusal stored %d codes", len(fx.f.state.codes))
			}
		})
	}
}

func TestMCPDecideMalformedAndAnonymousInput(t *testing.T) {
	fx := newMCPFixture(t)
	pending := mcpBeginOK(t, fx)
	anon := mcpDecisionOf(pending, true)
	anon.Principal.UserID = ""
	if _, err := fx.svc.DecideAuthorization(context.Background(), anon); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("empty principal err = %v, want ErrUnauthenticated", err)
	}
	noSession := mcpDecisionOf(pending, true)
	noSession.SessionToken = ""
	if _, err := fx.svc.DecideAuthorization(context.Background(), noSession); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("empty session err = %v, want ErrUnauthenticated", err)
	}
	fx.f.calls = nil
	bad := mcpDecisionOf(pending, true)
	bad.Handle = "short"
	if _, err := fx.svc.DecideAuthorization(context.Background(), bad); !errors.Is(err, ErrMCPAuthorizationRequestNotFound) {
		t.Fatalf("malformed handle err = %v, want ErrMCPAuthorizationRequestNotFound", err)
	}
	if len(fx.f.calls) != 0 {
		t.Fatalf("malformed handle touched the repository: %v", fx.f.calls)
	}
	if fx.f.requestCount() != 1 {
		t.Fatal("malformed decision consumed the consent")
	}
}

func TestMCPDecideExpiredConsentNotFound(t *testing.T) {
	fx := newMCPFixture(t)
	pending := mcpBeginOK(t, fx)
	fx.f.now = mcpTestStart.Add(MCPAuthorizationRequestLifetime)
	_, err := fx.svc.DecideAuthorization(context.Background(), mcpDecisionOf(pending, true))
	if !errors.Is(err, ErrMCPAuthorizationRequestNotFound) {
		t.Fatalf("err = %v, want ErrMCPAuthorizationRequestNotFound", err)
	}
}
