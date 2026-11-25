package mcptool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	userA     = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	userB     = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	houseA    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
	houseB    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b72"
	subjectA  = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b91"
	innerNext = "czE6bmV4dA"
)

type fakeSubjects struct {
	calls     int
	userID    string
	household string
	subjectID string
	query     subject.ListQuery
	err       error
	next      *string
}

func (f *fakeSubjects) Get(_ context.Context, userID, householdID, subjectID string) (subject.Subject, error) {
	f.calls++
	f.userID, f.household, f.subjectID = userID, householdID, subjectID
	if f.err != nil {
		return subject.Subject{}, f.err
	}
	return sample(), nil
}

func (f *fakeSubjects) List(_ context.Context, userID, householdID string, q subject.ListQuery) (subject.Page, error) {
	f.calls++
	f.userID, f.household, f.query = userID, householdID, q
	if f.err != nil {
		return subject.Page{}, f.err
	}
	return subject.Page{Items: []subject.Subject{sample()}, NextCursor: f.next}, nil
}

func sample() subject.Subject {
	at := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	return subject.Subject{ID: subjectA, HouseholdID: houseA, Type: subject.TypeVehicle, Name: "Car", CreatedAt: at, UpdatedAt: at, Version: 2}
}

// call serves the subject read tools in memory for one verified user (empty:
// no token) and returns the structured result or the tool error text.
func call(t *testing.T, user string, service Reader, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	for _, tool := range ReadTools(service) {
		tool.Install(server)
	}
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if c, ok := req.(*mcp.CallToolRequest); ok && user != "" {
				c.Extra = &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{
					Scopes: []string{identity.MCPScopeRead}, UserID: user, Extra: map[string]any{"grant_id": "g1"},
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
	defer func() { _ = ss.Close() }()
	peer, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = peer.Close() }()
	res, err := peer.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error %v", name, err)
	}
	if res.IsError {
		return nil, res.Content[0].(*mcp.TextContent).Text
	}
	raw, _ := json.Marshal(res.StructuredContent)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out, ""
}

func TestSubjectReadToolsDeclareReadScopeAndReadOnlyHint(t *testing.T) {
	want := []string{ListSubjectsName, GetSubjectName}
	tools := ReadTools(&fakeSubjects{})
	if len(tools) != len(want) {
		t.Fatalf("got %d tools", len(tools))
	}
	for i, tool := range tools {
		if tool.Tool.Name != want[i] || tool.Scope != identity.MCPScopeRead || !tool.Tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %d = %q scope %q", i, tool.Tool.Name, tool.Scope)
		}
	}
}

func TestGetSubjectPassesTheCallerAndReturnsTheTypedSubject(t *testing.T) {
	fake := &fakeSubjects{}
	out, msg := call(t, userA, fake, GetSubjectName, map[string]any{"householdId": houseA, "subjectId": subjectA})
	if msg != "" {
		t.Fatal(msg)
	}
	if fake.userID != userA || fake.household != houseA || fake.subjectID != subjectA {
		t.Fatalf("service got %q %q %q", fake.userID, fake.household, fake.subjectID)
	}
	if out["id"] != subjectA || out["type"] != "vehicle" || out["name"] != "Car" || out["linkedUserId"] != nil || out["version"] != float64(2) {
		t.Fatalf("output=%v", out)
	}
}

func TestSubjectReadToolsRefuseBeforeTheService(t *testing.T) {
	fake := &fakeSubjects{}
	if _, msg := call(t, "", fake, GetSubjectName, map[string]any{"householdId": houseA, "subjectId": subjectA}); msg != identityhttp.ErrMCPToolNoPrincipal.Error() {
		t.Fatalf("no principal: %q", msg)
	}
	for name, args := range map[string]map[string]any{
		"missing subject": {"householdId": houseA},
		"bad household":   {"householdId": "x", "subjectId": subjectA},
		"unknown arg":     {"householdId": houseA, "subjectId": subjectA, "ids": []string{subjectA}},
	} {
		if _, msg := call(t, userA, fake, GetSubjectName, args); msg == "" {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, msg := call(t, userA, fake, ListSubjectsName, map[string]any{"householdId": houseA, "limit": 101}); msg == "" {
		t.Fatal("limit 101 accepted")
	}
	if fake.calls != 0 {
		t.Fatalf("service called %d times", fake.calls)
	}
}

func TestSubjectReadToolsMapServiceErrorsToFixedTexts(t *testing.T) {
	for err, want := range map[error]string{
		subject.ErrNotFound:                             identityhttp.ErrMCPToolNotFound.Error(),
		&subject.InvalidQueryError{Parameter: "cursor"}: identityhttp.ErrMCPToolBadCursor.Error(),
		subject.ErrUnavailable:                          identityhttp.ErrMCPToolUnavailable.Error(),
		errors.New("pq: password=secret host=10.0.0.9"): identityhttp.ErrMCPToolUnavailable.Error(),
	} {
		if _, msg := call(t, userA, &fakeSubjects{err: err}, ListSubjectsName, map[string]any{"householdId": houseA}); msg != want {
			t.Fatalf("%v: got %q want %q", err, msg, want)
		}
	}
	if _, msg := call(t, userA, &fakeSubjects{err: subject.ErrNotFound}, GetSubjectName, map[string]any{"householdId": houseA, "subjectId": subjectA}); msg != identityhttp.ErrMCPToolNotFound.Error() {
		t.Fatalf("get not found: %q", msg)
	}
}

func TestListSubjectsCursorIsBoundToHouseholdArchivedAndUser(t *testing.T) {
	next := innerNext
	fake := &fakeSubjects{next: &next}
	out, msg := call(t, userA, fake, ListSubjectsName, map[string]any{"householdId": houseA})
	if msg != "" {
		t.Fatal(msg)
	}
	cursor := out["nextCursor"].(string)
	if cursor == "" || cursor == innerNext {
		t.Fatalf("cursor %q", cursor)
	}
	if _, msg := call(t, userA, fake, ListSubjectsName, map[string]any{"householdId": houseA, "cursor": cursor}); msg != "" || fake.query.Cursor != innerNext {
		t.Fatalf("same call: %q %q", msg, fake.query.Cursor)
	}
	before := fake.calls
	for name, tc := range map[string]struct {
		user string
		args map[string]any
	}{
		"other household": {userA, map[string]any{"householdId": houseB, "cursor": cursor}},
		"archived":        {userA, map[string]any{"householdId": houseA, "archived": true, "cursor": cursor}},
		"other user":      {userB, map[string]any{"householdId": houseA, "cursor": cursor}},
		"raw inner":       {userA, map[string]any{"householdId": houseA, "cursor": innerNext}},
	} {
		if _, msg := call(t, tc.user, fake, ListSubjectsName, tc.args); msg != identityhttp.ErrMCPToolBadCursor.Error() {
			t.Fatalf("%s: %q", name, msg)
		}
	}
	if fake.calls != before {
		t.Fatal("a refused cursor reached the service")
	}
}
