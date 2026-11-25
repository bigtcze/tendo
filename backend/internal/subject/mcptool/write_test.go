package mcptool

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const clientKey = "k-2026-0001"

// fakeSubjectWriter records the arguments of each Create and answers fixed values.
type fakeSubjectWriter struct {
	calls     int
	userID    string
	household string
	key       string
	name      string
	kind      subject.Type
	linked    *string
	err       error
}

func (f *fakeSubjectWriter) Create(_ context.Context, userID, householdID, key, name string, t subject.Type, linked *string) (subject.Subject, error) {
	f.calls++
	f.userID, f.household, f.key, f.name, f.kind, f.linked = userID, householdID, key, name, t, linked
	if f.err != nil {
		return subject.Subject{}, f.err
	}
	at := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	return subject.Subject{ID: subjectA, HouseholdID: houseA, Type: t, Name: name, CreatedAt: at, UpdatedAt: at, Version: 1}, nil
}

// writeSession serves the subject write tools over an in-memory transport for
// one verified user and scope set, with the grant id given. An empty user attaches
// no token.
func writeSession(t *testing.T, user string, scopes []string, grant string, service Writer) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	for _, tool := range WriteTools(service) {
		tool.Install(server)
	}
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if c, ok := req.(*mcp.CallToolRequest); ok && user != "" {
				c.Extra = &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{
					Scopes: scopes, UserID: user, Extra: map[string]any{"grant_id": grant},
				}}
			}
			return next(ctx, method, req)
		}
	})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	peer, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	return peer
}

// callWrite returns the structured result, or the tool error text.
func callWrite(t *testing.T, peer *mcp.ClientSession, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := peer.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error %v", name, err)
	}
	if res.IsError {
		text := ""
		if len(res.Content) == 1 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				text = tc.Text
			}
		}
		return nil, text
	}
	out := map[string]any{}
	if m, ok := res.StructuredContent.(map[string]any); ok {
		out = m
	}
	return out, ""
}

func createSubjectArgs() map[string]any {
	return map[string]any{"householdId": houseA, "name": "Car", "type": "vehicle", "idempotencyKey": clientKey}
}

func TestCreateSubjectDeclaresWriteScopeAndAnnotations(t *testing.T) {
	tools := WriteTools(&fakeSubjectWriter{})
	if len(tools) != 1 {
		t.Fatalf("got %d tools", len(tools))
	}
	tool := tools[0]
	a := tool.Tool.Annotations
	if tool.Tool.Name != CreateSubjectName || tool.Scope != identity.MCPScopeWrite || a == nil ||
		a.ReadOnlyHint || a.IdempotentHint || a.DestructiveHint == nil || *a.DestructiveHint {
		t.Fatalf("tool=%q scope=%q annotations=%+v", tool.Tool.Name, tool.Scope, a)
	}
}

func TestCreateSubjectRefusesMissingPrincipalAndReadOnlyTokenBeforeTheService(t *testing.T) {
	fake := &fakeSubjectWriter{}
	if _, msg := callWrite(t, writeSession(t, "", nil, "g1", fake), CreateSubjectName, createSubjectArgs()); msg != identityhttp.ErrMCPToolNoPrincipal.Error() {
		t.Fatalf("no principal: %q", msg)
	}
	read := writeSession(t, userA, []string{identity.MCPScopeRead}, "g1", fake)
	if _, msg := callWrite(t, read, CreateSubjectName, createSubjectArgs()); msg != identityhttp.ErrMCPToolNoPrincipal.Error() {
		t.Fatalf("read-only token: %q", msg)
	}
	if fake.calls != 0 {
		t.Fatalf("service called %d times", fake.calls)
	}
}

func TestCreateSubjectSchemaRefusesBeforeTheService(t *testing.T) {
	fake := &fakeSubjectWriter{}
	peer := writeSession(t, userA, []string{identity.MCPScopeWrite}, "g1", fake)
	long := make([]rune, maxNameRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	cases := map[string]map[string]any{
		"missing key":   {"householdId": houseA, "name": "Car", "type": "vehicle"},
		"missing name":  {"householdId": houseA, "type": "vehicle", "idempotencyKey": clientKey},
		"bad household": {"householdId": "../x", "name": "Car", "type": "vehicle", "idempotencyKey": clientKey},
		"bad type":      {"householdId": houseA, "name": "Car", "type": "spaceship", "idempotencyKey": clientKey},
		"name too long": {"householdId": houseA, "name": string(long), "type": "vehicle", "idempotencyKey": clientKey},
		"linked member": {"householdId": houseA, "name": "Car", "type": "vehicle", "idempotencyKey": clientKey, "linkedUserId": userB},
		"unknown extra": {"householdId": houseA, "name": "Car", "type": "vehicle", "idempotencyKey": clientKey, "owner": userB},
		"bad key chars": {"householdId": houseA, "name": "Car", "type": "vehicle", "idempotencyKey": "bad key"},
		"key too long":  {"householdId": houseA, "name": "Car", "type": "vehicle", "idempotencyKey": longKey(129)},
	}
	for name, args := range cases {
		if _, msg := callWrite(t, peer, CreateSubjectName, args); msg == "" {
			t.Fatalf("%s: accepted", name)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("service called %d times for refused arguments", fake.calls)
	}
}

func TestCreateSubjectPassesArgumentsAndDerivedKey(t *testing.T) {
	fake := &fakeSubjectWriter{}
	out, msg := callWrite(t, writeSession(t, userA, []string{identity.MCPScopeWrite}, "g1", fake), CreateSubjectName, createSubjectArgs())
	if msg != "" {
		t.Fatal(msg)
	}
	if fake.userID != userA || fake.household != houseA || fake.name != "Car" || fake.kind != subject.TypeVehicle {
		t.Fatalf("service got user=%q household=%q name=%q type=%q", fake.userID, fake.household, fake.name, fake.kind)
	}
	if fake.linked != nil {
		t.Fatalf("linkedUserID must be nil, got %q", *fake.linked)
	}
	if fake.key == "" || fake.key == clientKey {
		t.Fatalf("service received the raw client key or no key: %q", fake.key)
	}
	if out["id"] != subjectA || out["name"] != "Car" || out["type"] != "vehicle" {
		t.Fatalf("output=%v", out)
	}
}

func TestCreateSubjectDerivedKeyFollowsTheGrant(t *testing.T) {
	first := &fakeSubjectWriter{}
	if _, msg := callWrite(t, writeSession(t, userA, []string{identity.MCPScopeWrite}, "g1", first), CreateSubjectName, createSubjectArgs()); msg != "" {
		t.Fatal(msg)
	}
	again := &fakeSubjectWriter{}
	if _, msg := callWrite(t, writeSession(t, userA, []string{identity.MCPScopeWrite}, "g1", again), CreateSubjectName, createSubjectArgs()); msg != "" {
		t.Fatal(msg)
	}
	other := &fakeSubjectWriter{}
	if _, msg := callWrite(t, writeSession(t, userA, []string{identity.MCPScopeWrite}, "g2", other), CreateSubjectName, createSubjectArgs()); msg != "" {
		t.Fatal(msg)
	}
	if first.key != again.key {
		t.Fatal("same grant and key must derive the same service key")
	}
	if first.key == other.key {
		t.Fatal("another grant must derive a different service key")
	}
}

func TestCreateSubjectMapsErrorsToFixedTexts(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"not found", subject.ErrNotFound, identityhttp.ErrMCPToolNotFound.Error()},
		{"key reused", subject.ErrIdempotencyKeyReused, identityhttp.ErrMCPToolKeyReused.Error()},
		{"name refused", &subject.ValidationError{Field: "name", Code: "invalid_length"}, "the argument name is not valid"},
		{"type refused", &subject.ValidationError{Field: "type", Code: "invalid_type"}, "the argument type is not valid"},
		{"other validation", &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}, identityhttp.ErrMCPToolUnavailable.Error()},
		{"member person exists", subject.ErrMemberPersonExists, identityhttp.ErrMCPToolUnavailable.Error()},
		{"driver text", errors.New("pq: password=secret host=10.0.0.9"), identityhttp.ErrMCPToolUnavailable.Error()},
		{"unavailable", subject.ErrUnavailable, identityhttp.ErrMCPToolUnavailable.Error()},
	}
	for _, tc := range cases {
		fake := &fakeSubjectWriter{err: tc.err}
		_, msg := callWrite(t, writeSession(t, userA, []string{identity.MCPScopeWrite}, "g1", fake), CreateSubjectName, createSubjectArgs())
		if msg != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, msg, tc.want)
		}
	}
}

func longKey(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'k'
	}
	return string(b)
}
