package identity

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func mcpRevokeOf(token string) MCPTokenRevocation {
	return MCPTokenRevocation{Token: token, ClientID: mcpTestClient}
}

func TestMCPRevokeAccessTokenRevokesGrantAsClient(t *testing.T) {
	fx := newMCPFixture(t)
	_, tokens := fx.exchangeOK()
	grantID := fx.f.onlyGrantID(t)
	if err := fx.svc.RevokeToken(context.Background(), mcpRevokeOf(tokens.AccessToken)); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	rev, ok := fx.f.state.revocations[grantID]
	if !ok || rev.Reason != MCPRevokeClient || rev.ActorUserID != "" {
		t.Fatalf("revocation = %+v ok=%v, want client with empty actor", rev, ok)
	}
	if !fx.f.state.grants[grantID].Revoked {
		t.Fatal("grant not revoked")
	}
	if _, err := fx.svc.VerifyAccessToken(context.Background(), tokens.AccessToken); !errors.Is(err, ErrMCPInvalidToken) {
		t.Fatalf("revoked access token err = %v, want ErrMCPInvalidToken", err)
	}
}

func TestMCPRevokeRefreshTokenRevokesGrant(t *testing.T) {
	fx := newMCPFixture(t)
	_, tokens := fx.exchangeOK()
	grantID := fx.f.onlyGrantID(t)
	if err := fx.svc.RevokeToken(context.Background(), mcpRevokeOf(tokens.RefreshToken)); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	rev := fx.f.state.revocations[grantID]
	if rev.Reason != MCPRevokeClient || rev.ActorUserID != "" {
		t.Fatalf("revocation = %+v, want client with empty actor", rev)
	}
}

// TestMCPRevokeConsumedRefreshTokenIsClientNotReplay pins that a rotated-away
// refresh token still revokes the grant, with the client reason and never the
// replay reason: the client is revoking, not replaying.
func TestMCPRevokeConsumedRefreshTokenIsClientNotReplay(t *testing.T) {
	fx := newMCPFixture(t)
	_, first := fx.exchangeOK()
	grantID := fx.f.onlyGrantID(t)
	if _, err := fx.svc.Refresh(context.Background(), mcpRefreshOf(first)); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !fx.f.state.refresh[string(mcpDigest(first.RefreshToken))].Consumed {
		t.Fatal("setup: refresh token not consumed")
	}
	if err := fx.svc.RevokeToken(context.Background(), mcpRevokeOf(first.RefreshToken)); err != nil {
		t.Fatalf("RevokeToken of consumed refresh: %v", err)
	}
	rev, ok := fx.f.state.revocations[grantID]
	if !ok || rev.Reason != MCPRevokeClient {
		t.Fatalf("revocation = %+v ok=%v, want client", rev, ok)
	}
	if !fx.f.state.grants[grantID].Revoked {
		t.Fatal("grant not revoked")
	}
}

func TestMCPRevokeForeignClientIsNoOp(t *testing.T) {
	fx := newMCPFixture(t)
	_, tokens := fx.exchangeOK()
	grantID := fx.f.onlyGrantID(t)
	err := fx.svc.RevokeToken(context.Background(), MCPTokenRevocation{Token: tokens.AccessToken, ClientID: "other-client"})
	if err != nil {
		t.Fatalf("foreign client err = %v, want silent success", err)
	}
	if fx.f.state.grants[grantID].Revoked {
		t.Fatal("another client revoked the grant")
	}
	if _, ok := fx.f.state.revocations[grantID]; ok {
		t.Fatal("another client wrote a revocation")
	}
}

func TestMCPRevokeMalformedOrUnknownIsSilent(t *testing.T) {
	tests := []struct {
		name string
		in   MCPTokenRevocation
		noDB bool
	}{
		{name: "empty token", in: MCPTokenRevocation{Token: "", ClientID: mcpTestClient}, noDB: true},
		{name: "short token", in: MCPTokenRevocation{Token: "short", ClientID: mcpTestClient}, noDB: true},
		{name: "empty client", in: MCPTokenRevocation{Token: strings.Repeat("A", 43)}, noDB: true},
		{name: "unknown token", in: MCPTokenRevocation{Token: strings.Repeat("A", 43), ClientID: mcpTestClient}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			_, _ = fx.exchangeOK()
			grantID := fx.f.onlyGrantID(t)
			fx.f.calls = nil
			if err := fx.svc.RevokeToken(context.Background(), tt.in); err != nil {
				t.Fatalf("err = %v, want silent success", err)
			}
			if tt.noDB && len(fx.f.calls) != 0 {
				t.Fatalf("malformed revocation touched the repository: %v", fx.f.calls)
			}
			if fx.f.state.grants[grantID].Revoked {
				t.Fatal("no-op revocation revoked the grant")
			}
		})
	}
}

func TestMCPRevokeInfrastructureErrorRollsBack(t *testing.T) {
	for _, step := range []string{"FindTokenGrantOwner", "RevokeGrant"} {
		t.Run(step, func(t *testing.T) {
			fx := newMCPFixture(t)
			_, tokens := fx.exchangeOK()
			grantID := fx.f.onlyGrantID(t)
			fx.f.failOn = step
			err := fx.svc.RevokeToken(context.Background(), mcpRevokeOf(tokens.AccessToken))
			if !errors.Is(err, errMCPInjected) {
				t.Fatalf("err = %v, want the injected failure", err)
			}
			if fx.f.state.grants[grantID].Revoked {
				t.Fatal("failed revocation committed")
			}
			if _, ok := fx.f.state.revocations[grantID]; ok {
				t.Fatal("failed revocation wrote a cause")
			}
		})
	}
}

// refreshScopedFixture exchanges a code for both scopes and returns its tokens.
func refreshScopedFixture(t *testing.T) (*mcpFixture, MCPTokens) {
	t.Helper()
	fx := newMCPFixture(t)
	in := mcpValidAuthorization()
	in.Scopes = []string{MCPScopeWrite, MCPScopeRead}
	code := fx.issueCode(in)
	tokens, err := fx.svc.ExchangeCode(context.Background(), mcpValidExchange(code))
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	return fx, tokens
}

func TestMCPRefreshScopeAbsentIsUnchanged(t *testing.T) {
	fx, tokens := refreshScopedFixture(t)
	in := mcpRefreshOf(tokens)
	in.Scopes = nil
	if _, err := fx.svc.Refresh(context.Background(), in); err != nil {
		t.Fatalf("absent scope: %v", err)
	}
}

func TestMCPRefreshScopeSameSetOtherOrderAccepted(t *testing.T) {
	fx, tokens := refreshScopedFixture(t)
	in := mcpRefreshOf(tokens)
	in.Scopes = []string{MCPScopeWrite, MCPScopeRead, MCPScopeRead}
	next, err := fx.svc.Refresh(context.Background(), in)
	if err != nil {
		t.Fatalf("same set in other order: %v", err)
	}
	if !slices.Equal(next.Scopes, []string{MCPScopeRead, MCPScopeWrite}) {
		t.Fatalf("scopes = %v, want the grant's", next.Scopes)
	}
}

func TestMCPRefreshScopeSubsetRefusedWithoutConsuming(t *testing.T) {
	fx, tokens := refreshScopedFixture(t)
	grantID := fx.f.onlyGrantID(t)
	in := mcpRefreshOf(tokens)
	in.Scopes = []string{MCPScopeRead}
	_, err := fx.svc.Refresh(context.Background(), in)
	if !errors.Is(err, ErrMCPInvalidScope) {
		t.Fatalf("subset err = %v, want ErrMCPInvalidScope", err)
	}
	if fx.f.state.refresh[string(mcpDigest(tokens.RefreshToken))].Consumed {
		t.Fatal("refused scope consumed the refresh token")
	}
	if _, revoked := fx.f.state.revocations[grantID]; revoked {
		t.Fatal("refused scope revoked the grant")
	}
}

func TestMCPRefreshScopeUnknownRefusedBeforeRepository(t *testing.T) {
	fx, tokens := refreshScopedFixture(t)
	in := mcpRefreshOf(tokens)
	in.Scopes = []string{MCPScopeRead, "tendo:admin"}
	fx.f.calls = nil
	_, err := fx.svc.Refresh(context.Background(), in)
	if !errors.Is(err, ErrMCPInvalidScope) {
		t.Fatalf("unknown scope err = %v, want ErrMCPInvalidScope", err)
	}
	if len(fx.f.calls) != 0 {
		t.Fatalf("unknown scope touched the repository: %v", fx.f.calls)
	}
}
