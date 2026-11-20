package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

// registerFake is a registration store with a fixed cap outcome. It implements
// MCPRepository only to satisfy the constructor; registration never reads it.
type registerFake struct {
	inserted bool
	calls    int
	last     MCPClient
	cap      int
}

func (f *registerFake) WithMCPTransaction(context.Context, func(MCPTransaction) error) error {
	return errors.New("registration must not open an issuance transaction")
}

func (f *registerFake) FindActiveAccessToken(context.Context, []byte, time.Time) (MCPGrantRecord, error) {
	return MCPGrantRecord{}, ErrNotFound
}

func (f *registerFake) CreateClientWithinCap(_ context.Context, client MCPClient, _ time.Time, cap int) (bool, error) {
	f.calls++
	f.last = client
	f.cap = cap
	return f.inserted, nil
}

func newRegisterService(t *testing.T, store *registerFake, enabled bool) *MCPService {
	t.Helper()
	svc, err := NewMCPService(store, MCPOptions{Enabled: enabled, Resource: mcpTestResource}, func() time.Time {
		return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// TestRealClientRegistrationsRegisterThroughService replays representative real
// client bodies through RegisterClient on a fake store with room. Each must be
// registered (201 at the HTTP layer, which is out of this slice).
func TestRealClientRegistrationsRegisterThroughService(t *testing.T) {
	bodies := map[string]string{
		"claude.ai":   `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"scope":"claudeai","token_endpoint_auth_method":"none","grant_types":["authorization_code","refresh_token"],"response_types":["code"]}`,
		"claude code": `{"client_name":"Claude Code","redirect_uris":["http://localhost:54123/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`,
		"cursor":      `{"client_name":"Cursor","redirect_uris":["cursor://anysphere.cursor-mcp/oauth/callback","https://www.cursor.com/agents/mcp/oauth/callback","http://localhost:8787/callback"],"application_type":"web","scope":"mcp offline_access","grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"client_secret_post"}`,
		"chatgpt":     `{"client_name":"ChatGPT","redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"token_endpoint_auth_method":"client_secret_post"}`,
		"no name":     `{"redirect_uris":["http://127.0.0.1:9000/cb"]}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			store := &registerFake{inserted: true}
			svc := newRegisterService(t, store, true)
			reg, err := DecodeClientRegistration([]byte(body))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			client, err := svc.RegisterClient(context.Background(), reg)
			if err != nil {
				t.Fatalf("RegisterClient: %v", err)
			}
			if client.ID == "" || store.calls != 1 || store.cap != MCPRegistrationCap {
				t.Fatalf("client=%+v calls=%d cap=%d", client, store.calls, store.cap)
			}
		})
	}
}

// bareMCPRepository implements only MCPRepository. It has neither the
// registration store nor the sweep store, so the service must fail closed.
type bareMCPRepository struct{ called bool }

func (b *bareMCPRepository) WithMCPTransaction(context.Context, func(MCPTransaction) error) error {
	b.called = true
	return errors.New("no transaction expected")
}

func (b *bareMCPRepository) FindActiveAccessToken(context.Context, []byte, time.Time) (MCPGrantRecord, error) {
	b.called = true
	return MCPGrantRecord{}, ErrNotFound
}

// TestServiceWithoutStoresFailsClosed proves a repository lacking the client
// store and the sweep store refuses to register and refuses to sweep, rather
// than doing nothing and reporting success.
func TestServiceWithoutStoresFailsClosed(t *testing.T) {
	repo := &bareMCPRepository{}
	svc, err := NewMCPService(repo, MCPOptions{Enabled: true, Resource: mcpTestResource}, func() time.Time {
		return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	if svc.clientStore != nil || svc.sweeper != nil {
		t.Fatal("bare repository must not satisfy the client or sweep store")
	}

	reg, err := DecodeClientRegistration([]byte(`{"redirect_uris":["http://localhost:54123/callback"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RegisterClient(context.Background(), reg); err == nil {
		t.Fatal("RegisterClient succeeded without a client store")
	}

	report, err := svc.Sweep(context.Background(), time.Now())
	if !errors.Is(err, ErrMCPSweepFailed) {
		t.Fatalf("Sweep without a sweep store = %v, want ErrMCPSweepFailed", err)
	}
	if len(report.Deleted) != 0 || len(report.Locked) != 0 {
		t.Fatalf("sweep reported work without a store: %+v", report)
	}
	if repo.called {
		t.Fatal("bare repository was used for registration or sweep")
	}
}

// TestRegisterClientRefusals covers the refusal paths of the service: a disabled
// service, a full grantless-client cap, and an invalid registration never reach
// the store.
func TestRegisterClientRefusals(t *testing.T) {
	valid, err := DecodeClientRegistration([]byte(`{"redirect_uris":["http://localhost:54123/callback"]}`))
	if err != nil {
		t.Fatal(err)
	}
	// The public host is the resource's host (tendo.test): a self-redirect is refused.
	invalid, err := DecodeClientRegistration([]byte(`{"redirect_uris":["https://tendo.test/self"]}`))
	if err != nil {
		t.Fatal(err)
	}

	store := &registerFake{inserted: true}
	if _, err := newRegisterService(t, store, false).RegisterClient(context.Background(), valid); !errors.Is(err, ErrMCPDisabled) {
		t.Fatalf("disabled = %v, want ErrMCPDisabled", err)
	}
	if store.calls != 0 {
		t.Fatalf("disabled registration reached the store %d time(s)", store.calls)
	}

	full := &registerFake{inserted: false}
	if _, err := newRegisterService(t, full, true).RegisterClient(context.Background(), valid); !errors.Is(err, ErrMCPRegistrationFull) {
		t.Fatalf("full cap = %v, want ErrMCPRegistrationFull", err)
	}

	refused := &registerFake{inserted: true}
	if _, err := newRegisterService(t, refused, true).RegisterClient(context.Background(), invalid); !errors.Is(err, ErrMCPInvalidRedirectURI) {
		t.Fatalf("self redirect = %v, want ErrMCPInvalidRedirectURI", err)
	}
	if refused.calls != 0 {
		t.Fatalf("invalid registration reached the store %d time(s)", refused.calls)
	}
}
