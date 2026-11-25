package mcptool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	userA     = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	userB     = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"
	houseA    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b71"
	houseB    = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b72"
	itemA     = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81"
	itemB     = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b82"
	innerNext = "aTI6c2VydmljZS1uZXh0"
)

// fakeItems records the arguments of each call and answers fixed values.
type fakeItems struct {
	calls     int
	userID    string
	household string
	itemID    string
	query     item.ListQuery
	limit     int
	cursor    string
	order     string
	err       error
	next      *string
}

func (f *fakeItems) Get(_ context.Context, userID, householdID, itemID string) (item.Item, error) {
	f.calls++
	f.userID, f.household, f.itemID = userID, householdID, itemID
	if f.err != nil {
		return item.Item{}, f.err
	}
	return sampleItem(), nil
}

func (f *fakeItems) List(_ context.Context, userID, householdID string, q item.ListQuery) (item.Page, error) {
	f.calls++
	f.userID, f.household, f.query = userID, householdID, q
	if f.err != nil {
		return item.Page{}, f.err
	}
	return item.Page{Items: []item.Item{sampleItem()}, NextCursor: f.next}, nil
}

func (f *fakeItems) ListCompletions(_ context.Context, userID, householdID, itemID string, limit int, cursor, order string) (item.CompletionPage, error) {
	f.calls++
	f.userID, f.household, f.itemID, f.limit, f.cursor, f.order = userID, householdID, itemID, limit, cursor, order
	if f.err != nil {
		return item.CompletionPage{}, f.err
	}
	on, _ := schedule.NewDate(2026, time.May, 4)
	return item.CompletionPage{Items: []item.Completion{{ID: "c1", ItemID: itemID, CompletedByUserID: userID, CompletedOn: on}}, NextCursor: f.next}, nil
}

func sampleItem() item.Item {
	on, _ := schedule.NewDate(2026, time.June, 1)
	at := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	return item.Item{
		ID: itemA, HouseholdID: houseA, SubjectID: "s1", Title: "Change filter", AttentionOn: &on,
		Recurrence:    &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 3, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed},
		WorkflowState: item.StateOpen, Attention: schedule.Upcoming, CreatedAt: at, UpdatedAt: at, Version: 4,
	}
}

// session serves tools over an in-memory transport. A test-only middleware
// attaches the verified token the endpoint would attach, for the given user and
// scopes; an empty user attaches none.
type session struct {
	t    *testing.T
	peer *mcp.ClientSession
}

func newSession(t *testing.T, user string, scopes []string, tools ...identityhttp.MCPTool) *session {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "1"}, nil)
	for _, tool := range tools {
		tool.Install(server)
	}
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if call, ok := req.(*mcp.CallToolRequest); ok && user != "" {
				call.Extra = &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{
					Scopes: scopes, UserID: user, Extra: map[string]any{"grant_id": "g1"},
				}}
			}
			return next(ctx, method, req)
		}
	})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	peer, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "1"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	return &session{t: t, peer: peer}
}

func readSession(t *testing.T, user string, service Reader) *session {
	return newSession(t, user, []string{identity.MCPScopeRead}, ReadTools(service)...)
}

// call returns the structured result, or the tool error text.
func (s *session) call(name string, args map[string]any) (map[string]any, string) {
	s.t.Helper()
	res, err := s.peer.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		s.t.Fatalf("%s: protocol error %v", name, err)
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
	raw, _ := json.Marshal(res.StructuredContent)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out, ""
}

func TestItemReadToolsDeclareReadScopeAndReadOnlyHint(t *testing.T) {
	want := []string{ListItemsName, GetItemName, ListCompletionsName}
	tools := ReadTools(&fakeItems{})
	if len(tools) != len(want) {
		t.Fatalf("got %d tools, want %v", len(tools), want)
	}
	for i, tool := range tools {
		if tool.Tool.Name != want[i] || tool.Scope != identity.MCPScopeRead ||
			tool.Tool.Annotations == nil || !tool.Tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %d = %q scope %q; want %q, tendo:read, read-only", i, tool.Tool.Name, tool.Scope, want[i])
		}
	}
}

func TestItemReadToolsRefuseMissingPrincipalBeforeTheService(t *testing.T) {
	fake := &fakeItems{}
	s := readSession(t, "", fake)
	args := map[string]any{"householdId": houseA, "itemId": itemA}
	for _, name := range []string{GetItemName, ListCompletionsName} {
		if _, msg := s.call(name, args); msg != identityhttp.ErrMCPToolNoPrincipal.Error() {
			t.Fatalf("%s: %q", name, msg)
		}
	}
	if _, msg := s.call(ListItemsName, map[string]any{"householdId": houseA}); msg != identityhttp.ErrMCPToolNoPrincipal.Error() {
		t.Fatalf("list_items: %q", msg)
	}
	if fake.calls != 0 {
		t.Fatalf("service called %d times without a principal", fake.calls)
	}
}

func TestItemReadToolsRefuseAWriteOnlyToken(t *testing.T) {
	fake := &fakeItems{}
	s := newSession(t, userA, []string{identity.MCPScopeWrite}, ReadTools(fake)...)
	if _, msg := s.call(GetItemName, map[string]any{"householdId": houseA, "itemId": itemA}); msg != identityhttp.ErrMCPToolNoPrincipal.Error() || fake.calls != 0 {
		t.Fatalf("write-only token: msg=%q calls=%d", msg, fake.calls)
	}
}

func TestItemReadSchemasRefuseMalformedArgumentsBeforeTheService(t *testing.T) {
	fake := &fakeItems{}
	s := readSession(t, userA, fake)
	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"missing household", GetItemName, map[string]any{"itemId": itemA}},
		{"missing item", GetItemName, map[string]any{"householdId": houseA}},
		{"non-uuid household", GetItemName, map[string]any{"householdId": "../x", "itemId": itemA}},
		{"unknown argument", GetItemName, map[string]any{"householdId": houseA, "itemId": itemA, "userId": userB}},
		{"bad workflow", ListItemsName, map[string]any{"householdId": houseA, "workflowState": "done"}},
		{"bad sort", ListItemsName, map[string]any{"householdId": houseA, "sort": "title"}},
		{"limit too high", ListItemsName, map[string]any{"householdId": houseA, "limit": 101}},
		{"limit zero", ListItemsName, map[string]any{"householdId": houseA, "limit": 0}},
		{"long search", ListItemsName, map[string]any{"householdId": houseA, "q": strings.Repeat("a", item.MaxSearchRunes+1)}},
		{"bad responsible", ListItemsName, map[string]any{"householdId": houseA, "responsibleUserId": "nobody"}},
		{"bad order", ListCompletionsName, map[string]any{"householdId": houseA, "itemId": itemA, "order": "random"}},
		{"long cursor", ListCompletionsName, map[string]any{"householdId": houseA, "itemId": itemA, "cursor": strings.Repeat("A", identityhttp.MaxMCPCursorLength+1)}},
	}
	for _, tc := range cases {
		if out, msg := s.call(tc.tool, tc.args); msg == "" {
			t.Fatalf("%s: accepted, out=%v", tc.name, out)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("service called %d times for malformed arguments", fake.calls)
	}
	if _, msg := s.call(ListItemsName, map[string]any{"householdId": houseA, "responsibleUserId": "unassigned"}); msg != "" {
		t.Fatalf("unassigned responsible filter refused: %q", msg)
	}
}

func TestGetItemPassesTheCallerAndReturnsTheTypedItem(t *testing.T) {
	fake := &fakeItems{}
	out, msg := readSession(t, userA, fake).call(GetItemName, map[string]any{"householdId": houseA, "itemId": itemA})
	if msg != "" {
		t.Fatal(msg)
	}
	if fake.userID != userA || fake.household != houseA || fake.itemID != itemA {
		t.Fatalf("service got user=%q household=%q item=%q", fake.userID, fake.household, fake.itemID)
	}
	rec, _ := out["recurrence"].(map[string]any)
	if out["id"] != itemA || out["title"] != "Change filter" || out["attentionOn"] != "2026-06-01" ||
		out["attention"] != "upcoming" || out["version"] != float64(4) || rec["intervalUnit"] != "month" ||
		out["createdAt"] != "2026-05-04T10:00:00Z" || out["lastCompletedOn"] != nil {
		t.Fatalf("output=%v", out)
	}
	if _, leaked := out["householdId"]; leaked {
		t.Fatalf("output carries householdId: %v", out)
	}
}

func TestItemReadToolsMapServiceErrorsToFixedTexts(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{item.ErrNotFound, identityhttp.ErrMCPToolNotFound.Error()},
		{&item.InvalidQueryError{Parameter: "q"}, "the argument q is not valid"},
		{&item.InvalidQueryError{Parameter: "cursor"}, identityhttp.ErrMCPToolBadCursor.Error()},
		{&item.InvalidQueryError{Parameter: "attentionDate"}, identityhttp.ErrMCPToolUnavailable.Error()},
		{item.ErrUnavailable, identityhttp.ErrMCPToolUnavailable.Error()},
		{errors.New("pq: connection to 10.0.0.9 password=secret"), identityhttp.ErrMCPToolUnavailable.Error()},
	}
	for _, tc := range cases {
		s := readSession(t, userA, &fakeItems{err: tc.err})
		if _, msg := s.call(ListItemsName, map[string]any{"householdId": houseA}); msg != tc.want {
			t.Fatalf("%v: got %q want %q", tc.err, msg, tc.want)
		}
	}
	s := readSession(t, userA, &fakeItems{err: item.ErrNotFound})
	if _, msg := s.call(GetItemName, map[string]any{"householdId": houseA, "itemId": itemA}); msg != identityhttp.ErrMCPToolNotFound.Error() {
		t.Fatalf("get_item not found: %q", msg)
	}
	if _, msg := s.call(ListCompletionsName, map[string]any{"householdId": houseA, "itemId": itemA}); msg != identityhttp.ErrMCPToolNotFound.Error() {
		t.Fatalf("list_completions not found: %q", msg)
	}
}

func TestListItemsCursorContinuesOnlyTheSameCall(t *testing.T) {
	next := innerNext
	fake := &fakeItems{next: &next}
	s := readSession(t, userA, fake)
	args := map[string]any{"householdId": houseA, "q": "filter", "workflowState": "open", "recurring": true, "sort": "attention_on"}
	out, msg := s.call(ListItemsName, args)
	if msg != "" {
		t.Fatal(msg)
	}
	cursor, _ := out["nextCursor"].(string)
	if cursor == "" || cursor == innerNext {
		t.Fatalf("nextCursor %q must be a bound, non-empty cursor", cursor)
	}
	if fake.query.Q != "filter" || fake.query.WorkflowState != "open" || fake.query.Recurring == nil || !*fake.query.Recurring || fake.query.Sort != item.SortAttention {
		t.Fatalf("service query %+v", fake.query)
	}

	page2 := clone(args, "cursor", cursor)
	page2["limit"] = 10
	if _, msg := s.call(ListItemsName, page2); msg != "" || fake.query.Cursor != innerNext || fake.query.Limit != 10 {
		t.Fatalf("same call, other page size: msg=%q cursor=%q", msg, fake.query.Cursor)
	}

	before := fake.calls
	replays := map[string]map[string]any{
		"other household": clone(clone(args, "cursor", cursor), "householdId", houseB),
		"other search":    clone(clone(args, "cursor", cursor), "q", "filters"),
		"other filter":    clone(clone(args, "cursor", cursor), "workflowState", "paused"),
		"other recurring": clone(clone(args, "cursor", cursor), "recurring", false),
		"other sort":      clone(clone(args, "cursor", cursor), "sort", "created"),
		"archived":        clone(clone(args, "cursor", cursor), "archived", true),
		"raw inner":       clone(args, "cursor", innerNext),
	}
	for name, replay := range replays {
		if _, msg := s.call(ListItemsName, replay); msg != identityhttp.ErrMCPToolBadCursor.Error() {
			t.Fatalf("%s: %q", name, msg)
		}
	}
	other := readSession(t, userB, fake)
	if _, msg := other.call(ListItemsName, clone(args, "cursor", cursor)); msg != identityhttp.ErrMCPToolBadCursor.Error() {
		t.Fatalf("other user: %q", msg)
	}
	other2 := newSession(t, userA, []string{identity.MCPScopeRead}, ListCompletions(fake))
	if _, msg := other2.call(ListCompletionsName, map[string]any{"householdId": houseA, "itemId": itemA, "cursor": cursor}); msg != identityhttp.ErrMCPToolBadCursor.Error() {
		t.Fatalf("other tool: %q", msg)
	}
	if fake.calls != before {
		t.Fatalf("a refused cursor reached the service (%d calls)", fake.calls-before)
	}
}

func TestListCompletionsCursorIsBoundToItemAndOrder(t *testing.T) {
	next := innerNext
	fake := &fakeItems{next: &next}
	s := readSession(t, userA, fake)
	args := map[string]any{"householdId": houseA, "itemId": itemA, "order": "newest"}
	out, msg := s.call(ListCompletionsName, args)
	if msg != "" {
		t.Fatal(msg)
	}
	rows, _ := out["completions"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["completedOn"] != "2026-05-04" {
		t.Fatalf("completions=%v", out)
	}
	cursor := out["nextCursor"].(string)
	if _, msg := s.call(ListCompletionsName, clone(args, "cursor", cursor)); msg != "" || fake.cursor != innerNext || fake.order != "newest" {
		t.Fatalf("same call: msg=%q cursor=%q order=%q", msg, fake.cursor, fake.order)
	}
	before := fake.calls
	for name, replay := range map[string]map[string]any{
		"other item":  clone(clone(args, "cursor", cursor), "itemId", itemB),
		"other order": clone(clone(args, "cursor", cursor), "order", "oldest"),
	} {
		if _, msg := s.call(ListCompletionsName, replay); msg != identityhttp.ErrMCPToolBadCursor.Error() {
			t.Fatalf("%s: %q", name, msg)
		}
	}
	if fake.calls != before {
		t.Fatal("a refused cursor reached the service")
	}
}

func TestItemListsReturnEmptyCursorOnTheLastPage(t *testing.T) {
	s := readSession(t, userA, &fakeItems{})
	out, msg := s.call(ListItemsName, map[string]any{"householdId": houseA})
	if msg != "" || out["nextCursor"] != "" {
		t.Fatalf("last page: msg=%q out=%v", msg, out)
	}
}

func clone(m map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out[key] = value
	return out
}
