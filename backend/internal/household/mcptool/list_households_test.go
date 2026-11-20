package mcptool

import (
	"context"
	"errors"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testUser = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"

// fakeLister records the arguments it receives and returns a fixed result.
type fakeLister struct {
	rows   []household.ListedHousehold
	next   string
	err    error
	called bool
	gotUID string
	gotCur string
	gotLim int
}

func (f *fakeLister) ListForMember(_ context.Context, userID, cursor string, limit int) ([]household.ListedHousehold, string, error) {
	f.called = true
	f.gotUID, f.gotCur, f.gotLim = userID, cursor, limit
	return f.rows, f.next, f.err
}

func TestListHouseholdsDeclaresReadScopeAndReadOnlyHint(t *testing.T) {
	tool := ListHouseholds(&fakeLister{})
	if tool.Scope != identity.MCPScopeRead {
		t.Fatalf("scope=%q want tendo:read", tool.Scope)
	}
	if tool.Tool.Name != ListHouseholdsName || tool.Tool.Annotations == nil || !tool.Tool.Annotations.ReadOnlyHint {
		t.Fatalf("tool=%+v must be named list_households and read-only", tool.Tool)
	}
}

func TestListHouseholdsRegistersExactlyOneTool(t *testing.T) {
	tool := ListHouseholds(&fakeLister{})
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	tool.Install(server)
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	session, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	peer, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	var names []string
	for listed, err := range peer.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, listed.Name)
		if listed.Annotations == nil || !listed.Annotations.ReadOnlyHint {
			t.Fatalf("%s must be read-only", listed.Name)
		}
	}
	if len(names) != 1 || names[0] != ListHouseholdsName {
		t.Fatalf("tools/list=%v, want exactly [%s]", names, ListHouseholdsName)
	}
}

func TestListHouseholdsHandlerReturnsTypedPageForPrincipal(t *testing.T) {
	fake := &fakeLister{
		rows: []household.ListedHousehold{{ID: "h1", Name: "Home", ViewerRole: household.RoleAdmin}},
		next: "h1next",
	}
	handler := listHandler(fake)
	req := principalRequest(t, testUser, "g1")
	_, out, err := handler(context.Background(), req, ListHouseholdsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !fake.called || fake.gotUID != testUser || fake.gotLim != defaultLimit || fake.gotCur != "" {
		t.Fatalf("service call uid=%q cursor=%q limit=%d", fake.gotUID, fake.gotCur, fake.gotLim)
	}
	if len(out.Households) != 1 || out.Households[0].Name != "Home" || out.Households[0].Role != household.RoleAdmin || out.NextCursor != "h1next" {
		t.Fatalf("output=%+v", out)
	}
}

func TestListHouseholdsRefusesMissingPrincipalWithoutServiceCall(t *testing.T) {
	fake := &fakeLister{}
	_, _, err := listHandler(fake)(context.Background(), &mcp.CallToolRequest{}, ListHouseholdsInput{})
	if err == nil || fake.called {
		t.Fatalf("missing principal must fail closed before the service: err=%v called=%v", err, fake.called)
	}
}

func TestListHouseholdsRefusesOversizedCursor(t *testing.T) {
	fake := &fakeLister{}
	req := principalRequest(t, testUser, "g1")
	long := make([]byte, maxCursorLength+1)
	for i := range long {
		long[i] = 'a'
	}
	_, _, err := listHandler(fake)(context.Background(), req, ListHouseholdsInput{Cursor: string(long)})
	if err == nil || fake.called {
		t.Fatalf("oversized cursor must be refused before the service: err=%v called=%v", err, fake.called)
	}
}

func TestListHouseholdsFixedErrorMessages(t *testing.T) {
	req := principalRequest(t, testUser, "g1")
	cases := map[string]struct {
		err  error
		want string
	}{
		"invalid cursor": {&household.ValidationError{Field: "cursor", Code: "invalid_format"}, errInvalid},
		"not found":      {household.ErrNotFound, errNotFound},
		"driver text":    {errors.New("pq: connection to 10.0.0.9 password=secret"), errUnavailable},
	}
	for name, tc := range cases {
		fake := &fakeLister{err: tc.err}
		_, _, err := listHandler(fake)(context.Background(), req, ListHouseholdsInput{})
		if err == nil || err.Error() != tc.want {
			t.Fatalf("%s: err=%v want %q", name, err, tc.want)
		}
	}
}

// principalRequest builds a call request whose verified token carries the user
// and grant the MCP endpoint would set. The tool reads them only through
// identityhttp.MCPUserID.
func principalRequest(t *testing.T, userID, grant string) *mcp.CallToolRequest {
	t.Helper()
	return &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: ListHouseholdsName},
		Extra: &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{
			Scopes: []string{identity.MCPScopeRead},
			UserID: userID,
			Extra:  map[string]any{"grant_id": grant},
		}},
	}
}
