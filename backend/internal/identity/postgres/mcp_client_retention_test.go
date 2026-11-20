package postgres

import (
	"errors"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/identity"
)

// idFor returns a distinct, valid client_id for a fixture index.
func idFor(i int) string { return "mcp_cap_client_" + string(rune('a'+i)) + "00000" }

// TestRegistrationPersistsAndReturnsClient proves a registration through the
// service is stored exactly as returned.
func TestRegistrationPersistsAndReturnsClient(t *testing.T) {
	env := newMCPEnv(t)
	svc, err := identity.NewMCPService(NewMCPRepository(env.app), identity.MCPOptions{
		Enabled: true, Resource: mcpTestResource,
	}, env.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := identity.DecodeClientRegistration([]byte(`{"client_name":" Claude Code ","redirect_uris":["http://localhost:54123/callback"]}`))
	if err != nil {
		t.Fatal(err)
	}
	client, err := svc.RegisterClient(env.ctx, reg)
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if client.Name != "Claude Code" || len(client.RedirectURIs) != 1 || client.RedirectURIs[0] != "http://localhost:54123/callback" {
		t.Fatalf("returned client %+v", client)
	}
	var name string
	var uris []string
	if err := env.admin.QueryRow(env.ctx, `SELECT client_name, redirect_uris FROM mcp_clients WHERE client_id=$1`, client.ID).Scan(&name, &uris); err != nil {
		t.Fatalf("stored client missing: %v", err)
	}
	if name != "Claude Code" || len(uris) != 1 || uris[0] != "http://localhost:54123/callback" {
		t.Fatalf("stored client name=%q uris=%v", name, uris)
	}
}

// TestRegistrationCapCountsOnlyGrantlessClients fills the cap with grantless
// clients, shows a refused registration writes nothing, and shows that a client
// holding a grant does not count against the cap.
func TestRegistrationCapCountsOnlyGrantlessClients(t *testing.T) {
	env := newMCPEnv(t)
	repo := NewMCPRepository(env.app)
	now := env.clock.now()
	// The bound fixture client is grantless too, so it fills one slot of the cap.
	const cap = 4
	for i := 0; i < cap-1; i++ {
		inserted, err := repo.CreateClientWithinCap(env.ctx, identity.MCPClient{
			ID: idFor(i), Name: "Grantless", RedirectURIs: []string{"https://client.test/" + idFor(i)},
		}, now, cap)
		if err != nil || !inserted {
			t.Fatalf("grantless client %d: inserted=%v err=%v", i, inserted, err)
		}
	}
	inserted, err := repo.CreateClientWithinCap(env.ctx, identity.MCPClient{
		ID: "mcp_cap_refused_0000", Name: "Refused", RedirectURIs: []string{"https://client.test/x"},
	}, now, cap)
	if err != nil || inserted {
		t.Fatalf("client over cap: inserted=%v err=%v; want refused without error", inserted, err)
	}
	var refused int
	if err := env.admin.QueryRow(env.ctx, `SELECT count(*) FROM mcp_clients WHERE client_id='mcp_cap_refused_0000'`).Scan(&refused); err != nil || refused != 0 {
		t.Fatalf("refused client persisted: count=%d err=%v", refused, err)
	}

	// Granting one client takes it out of the count, so one slot opens.
	user, _ := env.newAccount(t, "mcp_cap_grant_user")
	if _, err := env.admin.Exec(env.ctx, `INSERT INTO mcp_grants(user_id,client_id,scopes,resource,created_at,expires_at)
		VALUES($1::uuid,$2,ARRAY['tendo:read'],$3,now(),now()+interval '1 day')`, user, idFor(0), mcpTestResource); err != nil {
		t.Fatal(err)
	}
	inserted, err = repo.CreateClientWithinCap(env.ctx, identity.MCPClient{
		ID: "mcp_cap_after_grant_0", Name: "After grant", RedirectURIs: []string{"https://client.test/after"},
	}, now, cap)
	if err != nil || !inserted {
		t.Fatalf("client after a grant freed a slot: inserted=%v err=%v", inserted, err)
	}
}

// TestRegistrationRefusedWhenDisabled proves a disabled service refuses without
// writing a client row.
func TestRegistrationRefusedWhenDisabled(t *testing.T) {
	env := newMCPEnv(t)
	disabled, err := identity.NewMCPService(NewMCPRepository(env.app), identity.MCPOptions{Enabled: false}, env.clock.now)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := identity.DecodeClientRegistration([]byte(`{"redirect_uris":["http://localhost:54123/callback"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := disabled.RegisterClient(env.ctx, reg); !errors.Is(err, identity.ErrMCPDisabled) {
		t.Fatalf("RegisterClient disabled = %v, want ErrMCPDisabled", err)
	}
	var count int
	if err := env.admin.QueryRow(env.ctx, `SELECT count(*) FROM mcp_clients WHERE client_name='MCP client'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("disabled registration wrote %d rows (err=%v)", count, err)
	}
}
