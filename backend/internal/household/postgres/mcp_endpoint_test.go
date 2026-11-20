package postgres

// Real-PostgreSQL tests for the /mcp endpoint (ADR 0023, Issue #456 A4a). They
// wire the production services: the identity MCP repository, the household
// service behind list_households and the endpoint itself. Every token is minted
// through the real authorization flow.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdmcp "github.com/bigtcze/tendo/backend/internal/household/mcptool"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	identitypostgres "github.com/bigtcze/tendo/backend/internal/identity/postgres"
)

const (
	mcpEndpointIssuer   = "https://tendo.test"
	mcpEndpointResource = mcpEndpointIssuer + "/mcp"
	mcpEndpointRedirect = "https://client.test/callback"
	mcpEndpointPassword = "correct horse battery"
	mcpEndpointInit     = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"endpoint-test","version":"1"}}}`
)

// mcpEndpointNegotiated is the revision the SDK's initialize settles on.
const mcpEndpointNegotiated = "2025-11-25"

var mcpEndpointVerifier = "mcp_endpoint_verifier_" + strings.Repeat("y", 30)

// mcpEndpointChallenge is BASE64URL(SHA256(verifier)) per RFC 7636.
func mcpEndpointChallenge() string {
	sum := sha256.Sum256([]byte(mcpEndpointVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// mcpEndpointClock is a fixed clock shared by the MCP service and the sessions.
type mcpEndpointClock struct{ at time.Time }

func (c *mcpEndpointClock) now() time.Time { return c.at }

// mcpEndpointFixture wires the MCP service over the runtime pool, the household
// service behind list_households and the endpoint, on top of the account
// deletion fixture (which provides users, memberships and sessions).
type mcpEndpointFixture struct {
	*accountDeletionFixture
	mcp      *identityapp.MCPService
	endpoint *identityhttp.MCPEndpoint
	clock    *mcpEndpointClock
	clientID string
}

func newMCPEndpointFixture(t *testing.T) *mcpEndpointFixture {
	t.Helper()
	_, base := newAccountDeletionFixture(t)
	// Registered clients outlive user resets; clear them so the grantless
	// client cap cannot fill across tests.
	if _, err := base.admin.Exec(base.ctx, `TRUNCATE mcp_clients CASCADE`); err != nil {
		t.Fatal(err)
	}
	clock := &mcpEndpointClock{at: time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)}
	svc, err := identityapp.NewMCPService(identitypostgres.NewMCPRepository(base.app), identityapp.MCPOptions{
		Enabled: true, Resource: mcpEndpointResource,
	}, clock.now)
	if err != nil {
		t.Fatal(err)
	}
	name := "Endpoint test client"
	registered, err := svc.RegisterClient(base.ctx, identityapp.MCPClientRegistration{
		Name: &name, RedirectURIs: []string{mcpEndpointRedirect},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	households := household.NewService(NewRepository(base.app))
	endpoint, err := identityhttp.NewMCPEndpoint(svc, mcpEndpointIssuer, 5*time.Second,
		[]identityhttp.MCPTool{householdmcp.ListHouseholds(households)})
	if err != nil {
		t.Fatal(err)
	}
	return &mcpEndpointFixture{accountDeletionFixture: base, mcp: svc, endpoint: endpoint, clock: clock, clientID: registered.ID}
}

// mint signs the user in and runs the real consent, decision and code exchange,
// returning the issued token pair for the given scopes.
func (f *mcpEndpointFixture) mint(t *testing.T, user string, scopes ...string) identityapp.MCPTokens {
	t.Helper()
	return f.mintVia(t, f.mcp, mcpEndpointResource, user, scopes...)
}

// mintVia runs the real flow against the given service and resource, so a token
// can be minted for a different protected resource.
func (f *mcpEndpointFixture) mintVia(t *testing.T, svc *identityapp.MCPService, resource, user string, scopes ...string) identityapp.MCPTokens {
	t.Helper()
	signed, err := f.sessions.Login(f.ctx, f.loginOf(user), mcpEndpointPassword)
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	pending, err := svc.BeginAuthorization(f.ctx, identityapp.MCPAuthorizationParams{
		ClientID: f.clientID, RedirectURI: mcpEndpointRedirect, CodeChallenge: mcpEndpointChallenge(),
		CodeChallengeMethod: "S256", Resource: resource, Scopes: scopes,
	})
	if err != nil {
		t.Fatalf("BeginAuthorization: %v", err)
	}
	decided, err := svc.DecideAuthorization(f.ctx, identityapp.MCPDecision{
		Principal: signed.Principal, SessionToken: signed.Token,
		Handle: pending.Handle, BrowserBinding: pending.BrowserBinding, Approve: true,
	})
	if err != nil {
		t.Fatalf("DecideAuthorization: %v", err)
	}
	tokens, err := svc.ExchangeCode(f.ctx, identityapp.MCPCodeExchange{
		Code: decided.Code, CodeVerifier: mcpEndpointVerifier, ClientID: f.clientID,
		RedirectURI: mcpEndpointRedirect, Resource: resource,
	})
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	return tokens
}

// mcpEndpointReply is one answer from the endpoint.
type mcpEndpointReply struct {
	code   int
	raw    string
	body   map[string]any
	header http.Header
}

// post sends one JSON-RPC message with the bearer token. It is safe to call from
// goroutines: it asserts nothing.
func (f *mcpEndpointFixture) post(token, body string) mcpEndpointReply {
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Content-Type", "application/json")
	// Every post-initialize message carries the negotiated revision, as the
	// protocol requires; initialize itself is exempt from the header rule.
	r.Header.Set("MCP-Protocol-Version", mcpEndpointNegotiated)
	w := httptest.NewRecorder()
	f.endpoint.ServeHTTP(w, r)
	reply := mcpEndpointReply{code: w.Code, raw: w.Body.String(), header: w.Header()}
	_ = json.Unmarshal(w.Body.Bytes(), &reply.body)
	return reply
}

func mcpEndpointListCall(id int) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"list_households","arguments":{}}}`, id)
}

// mcpEndpointParseList returns the households of a list_households result. It
// reports an error rather than failing, so it is safe from goroutines.
func mcpEndpointParseList(reply mcpEndpointReply) ([]map[string]any, error) {
	if reply.code != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", reply.code, reply.raw)
	}
	result, _ := reply.body["result"].(map[string]any)
	if result["isError"] == true {
		return nil, fmt.Errorf("tool error result: %s", reply.raw)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	rows, _ := structured["households"].([]any)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("malformed row: %s", reply.raw)
		}
		out = append(out, m)
	}
	return out, nil
}

// TestMCPEndpointListsExactlyTheCallersHouseholdsAgainstPostgres runs the happy
// path end to end: initialize, tools/list (exactly list_households) and a
// tools/call that returns exactly the caller's households.
func TestMCPEndpointListsExactlyTheCallersHouseholdsAgainstPostgres(t *testing.T) {
	f := newMCPEndpointFixture(t)
	user := f.newUser()
	f.password(user)
	alpha := f.newHousehold("alpha home")
	beta := f.newHousehold("beta flat")
	f.join(user, alpha, household.RoleAdmin)
	f.join(user, beta, household.RoleMember)
	tokens := f.mint(t, user, identityapp.MCPScopeRead)

	if init := f.post(tokens.AccessToken, mcpEndpointInit); init.code != http.StatusOK {
		t.Fatalf("initialize: status=%d body=%s", init.code, init.raw)
	}
	list := f.post(tokens.AccessToken, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result, _ := list.body["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if list.code != http.StatusOK || len(tools) != 1 || tools[0].(map[string]any)["name"] != "list_households" {
		t.Fatalf("tools/list: status=%d body=%s; want exactly list_households", list.code, list.raw)
	}

	rows, err := mcpEndpointParseList(f.post(tokens.AccessToken, mcpEndpointListCall(3)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{ name, role string }{
		alpha: {"alpha home", household.RoleAdmin},
		beta:  {"beta flat", household.RoleMember},
	}
	if len(rows) != len(want) {
		t.Fatalf("tools/call returned %d households, want %d: %v", len(rows), len(want), rows)
	}
	for _, row := range rows {
		id, _ := row["id"].(string)
		exp, ok := want[id]
		if !ok || row["name"] != exp.name || row["role"] != exp.role {
			t.Fatalf("unexpected household %v; want the caller's alpha/beta rows only", row)
		}
	}
}
