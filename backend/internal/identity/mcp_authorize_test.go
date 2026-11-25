package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

const mcpTestLoopbackRedirect = "http://127.0.0.1:8787/callback"

// mcpValidParams is a request that Begin accepts for the default client.
func mcpValidParams() MCPAuthorizationParams {
	return MCPAuthorizationParams{
		ClientID: mcpTestClient, RedirectURI: mcpTestRedirect, CodeChallenge: mcpTestChallenge,
		CodeChallengeMethod: "S256", Resource: mcpTestResource, Scopes: []string{MCPScopeRead}, State: "state-1",
	}
}

// requestCount reports how many pending consents the committed state holds.
func (f *mcpFake) requestCount() int { return len(f.state.requests) }

func TestMCPDisabledAuthorizeRefusesWithoutTouchingRepository(t *testing.T) {
	f := newMCPFake(mcpTestStart)
	svc, err := NewMCPService(f, MCPOptions{Enabled: false}, f.clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	handle := strings.Repeat("A", 43)
	tests := []struct {
		name string
		call func() error
	}{
		{"BeginAuthorization", func() error {
			_, e := svc.BeginAuthorization(ctx, mcpValidParams())
			return e
		}},
		{"GetAuthorizationRequest", func() error {
			_, e := svc.GetAuthorizationRequest(ctx, mcpTestUser, handle, handle)
			return e
		}},
		{"DecideAuthorization", func() error {
			_, e := svc.DecideAuthorization(ctx, MCPDecision{
				Principal: Principal{UserID: mcpTestUser}, SessionToken: mcpTestSession,
				Handle: handle, BrowserBinding: handle, Approve: true,
			})
			return e
		}},
		{"RevokeToken", func() error {
			return svc.RevokeToken(ctx, MCPTokenRevocation{Token: handle, ClientID: mcpTestClient})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.calls = nil
			if err := tt.call(); !errors.Is(err, ErrMCPDisabled) {
				t.Fatalf("err = %v, want ErrMCPDisabled", err)
			}
			if len(f.calls) != 0 {
				t.Fatalf("repository touched: %v", f.calls)
			}
		})
	}
}

// TestMCPBeginAuthorizationRefusals covers every refusal class. Each refusal
// stores nothing and is decided before any repository call unless the class
// needs the client row.
func TestMCPBeginAuthorizationRefusals(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*MCPAuthorizationParams)
		want   error
		noDB   bool
	}{
		{name: "empty client", mutate: func(in *MCPAuthorizationParams) { in.ClientID = "" }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "empty redirect", mutate: func(in *MCPAuthorizationParams) { in.RedirectURI = "" }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "plain method", mutate: func(in *MCPAuthorizationParams) { in.CodeChallengeMethod = "plain" }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "absent method", mutate: func(in *MCPAuthorizationParams) { in.CodeChallengeMethod = "" }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "challenge 42 chars", mutate: func(in *MCPAuthorizationParams) { in.CodeChallenge = mcpTestChallenge[:42] }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "challenge padded", mutate: func(in *MCPAuthorizationParams) { in.CodeChallenge = mcpTestChallenge[:42] + "=" }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "state control character", mutate: func(in *MCPAuthorizationParams) { in.State = "a\x01b" }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "state tab", mutate: func(in *MCPAuthorizationParams) { in.State = "a\tb" }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "state non-ascii", mutate: func(in *MCPAuthorizationParams) { in.State = "caf\u00e9" }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "state 1025 bytes", mutate: func(in *MCPAuthorizationParams) { in.State = strings.Repeat("a", 1025) }, want: ErrMCPInvalidRequest, noDB: true},
		{name: "resource mismatch", mutate: func(in *MCPAuthorizationParams) { in.Resource = "https://evil.test/mcp" }, want: ErrMCPInvalidTarget, noDB: true},
		{name: "only unknown scopes", mutate: func(in *MCPAuthorizationParams) { in.Scopes = []string{"tendo:admin"} }, want: ErrMCPInvalidScope, noDB: true},
		{name: "unregistered redirect", mutate: func(in *MCPAuthorizationParams) { in.RedirectURI = "https://other.test/cb" }, want: ErrMCPInvalidRedirectURI},
		{name: "redirect fails policy", mutate: func(in *MCPAuthorizationParams) { in.RedirectURI = "http://example.test/cb" }, want: ErrMCPInvalidRedirectURI, noDB: true},
		{name: "redirect trailing slash", mutate: func(in *MCPAuthorizationParams) { in.RedirectURI = mcpTestRedirect + "/" }, want: ErrMCPInvalidRedirectURI},
		{name: "unknown client", mutate: func(in *MCPAuthorizationParams) { in.ClientID = "nobody" }, want: ErrMCPInvalidClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newMCPFixture(t)
			in := mcpValidParams()
			tt.mutate(&in)
			fx.f.calls = nil
			_, err := fx.svc.BeginAuthorization(context.Background(), in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if fx.f.requestCount() != 0 {
				t.Fatalf("refused request stored %d pending consents", fx.f.requestCount())
			}
			if tt.noDB && len(fx.f.calls) != 0 {
				t.Fatalf("refusal touched the repository: %v", fx.f.calls)
			}
		})
	}
}

func TestMCPBeginAuthorizationAcceptsAbsentFields(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidParams()
	in.Resource = ""
	in.Scopes = nil
	in.State = ""
	if _, err := fx.svc.BeginAuthorization(context.Background(), in); err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	for _, rec := range fx.f.state.requests {
		if rec.Resource != mcpTestResource {
			t.Fatalf("resource = %q, want the configured resource", rec.Resource)
		}
		if !slices.Equal(rec.Scopes, []string{MCPScopeRead, MCPScopeWrite}) {
			t.Fatalf("absent scope stored %v, want both", rec.Scopes)
		}
		if rec.State != "" {
			t.Fatalf("absent state stored %q", rec.State)
		}
	}
}

func TestMCPBeginAuthorizationDropsUnknownScopes(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidParams()
	in.Scopes = []string{MCPScopeWrite, "tendo:admin", MCPScopeRead, MCPScopeWrite}
	if _, err := fx.svc.BeginAuthorization(context.Background(), in); err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	for _, rec := range fx.f.state.requests {
		if !slices.Equal(rec.Scopes, []string{MCPScopeRead, MCPScopeWrite}) {
			t.Fatalf("stored scopes %v, want unknown dropped, deduplicated and sorted", rec.Scopes)
		}
	}
}

func TestMCPBeginAuthorizationResourceAbsentOrConfiguredOnly(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidParams()
	in.Resource = mcpTestResource
	if _, err := fx.svc.BeginAuthorization(context.Background(), in); err != nil {
		t.Fatalf("configured resource refused: %v", err)
	}
}

func TestMCPBeginAuthorizationLoopbackPortVariantAccepted(t *testing.T) {
	fx := newMCPFixture(t)
	fx.f.clients[mcpTestClient] = MCPClient{ID: mcpTestClient, Name: "Loopback", RedirectURIs: []string{"http://127.0.0.1:8787/callback"}}
	in := mcpValidParams()
	in.RedirectURI = "http://127.0.0.1:54123/callback"
	pending, err := fx.svc.BeginAuthorization(context.Background(), in)
	if err != nil {
		t.Fatalf("loopback port variant refused: %v", err)
	}
	rec := fx.f.state.requests[string(mcpDigest(pending.Handle))]
	if rec.RedirectURI != "http://127.0.0.1:54123/callback" {
		t.Fatalf("stored redirect %q, want the requested string verbatim", rec.RedirectURI)
	}
}

func TestMCPBeginAuthorizationStoresDigestsOnly(t *testing.T) {
	fx := newMCPFixture(t)
	in := mcpValidParams()
	in.Scopes = []string{MCPScopeWrite, MCPScopeRead}
	fx.f.now = mcpTestStart
	pending, err := fx.svc.BeginAuthorization(context.Background(), in)
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	if !mcpSecretPattern.MatchString(pending.Handle) || !mcpSecretPattern.MatchString(pending.BrowserBinding) {
		t.Fatal("handle or binding is not a 43-character secret")
	}
	if pending.Handle == pending.BrowserBinding {
		t.Fatal("handle and binding are identical")
	}
	if want := mcpTestStart.Add(5 * time.Minute); !pending.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt = %v, want %v", pending.ExpiresAt, want)
	}
	if len(fx.f.state.requests) != 1 {
		t.Fatalf("stored %d pending consents, want 1", len(fx.f.state.requests))
	}
	rec, ok := fx.f.state.requests[string(mcpDigest(pending.Handle))]
	if !ok {
		t.Fatal("pending consent not keyed by sha256(handle)")
	}
	if _, plain := fx.f.state.requests[pending.Handle]; plain {
		t.Fatal("plaintext handle used as a storage key")
	}
	if !slices.Equal(rec.RequestHash, mcpDigest(pending.Handle)) || !slices.Equal(rec.BindingHash, mcpDigest(pending.BrowserBinding)) {
		t.Fatal("stored digests are not sha256 of the handle and binding")
	}
	// No plaintext handle or binding may appear anywhere in the record.
	dump := fmt.Sprintf("%+v", rec)
	if strings.Contains(dump, pending.Handle) || strings.Contains(dump, pending.BrowserBinding) {
		t.Fatal("stored record contains a plaintext handle or binding")
	}
	if rec.RedirectURI != mcpTestRedirect || rec.ClientID != mcpTestClient || rec.CodeChallenge != mcpTestChallenge ||
		rec.Resource != mcpTestResource || rec.State != "state-1" {
		t.Fatalf("unexpected stored record: %+v", rec)
	}
	if !slices.Equal(rec.Scopes, []string{MCPScopeRead, MCPScopeWrite}) {
		t.Fatalf("stored scopes %v", rec.Scopes)
	}
	if !rec.CreatedAt.Equal(mcpTestStart) || !rec.ExpiresAt.Equal(pending.ExpiresAt) {
		t.Fatalf("stored times = %v..%v", rec.CreatedAt, rec.ExpiresAt)
	}
	want := []string{"WithMCPTransaction", "FindClient", "CreateAuthorizationRequest"}
	if !slices.Equal(fx.f.calls, want) {
		t.Fatalf("calls = %v, want %v (no account lock: the request is unauthenticated)", fx.f.calls, want)
	}
}
