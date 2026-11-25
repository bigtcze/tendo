package postgres

// Real-PostgreSQL tests for the MCP read tools (ADR 0023, #457 slice B2). The
// endpoint serves the production tool set over the production services: the
// household, subject and item services on the runtime pool, authorized through
// membership exactly as cmd/tendo wires them. Every token is minted through the
// real authorization flow.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdmcp "github.com/bigtcze/tendo/backend/internal/household/mcptool"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	itemapp "github.com/bigtcze/tendo/backend/internal/item"
	itemmcp "github.com/bigtcze/tendo/backend/internal/item/mcptool"
	itempostgres "github.com/bigtcze/tendo/backend/internal/item/postgres"
	subjectapp "github.com/bigtcze/tendo/backend/internal/subject"
	subjectmcp "github.com/bigtcze/tendo/backend/internal/subject/mcptool"
	subjectpostgres "github.com/bigtcze/tendo/backend/internal/subject/postgres"
)

const mcpToolsMissingID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4bff"

// newMCPToolsFixture is the endpoint fixture serving every production tool.
func newMCPToolsFixture(t *testing.T) *mcpEndpointFixture {
	t.Helper()
	f := newMCPEndpointFixture(t)
	households := household.NewService(NewRepository(f.app))
	membership := func(ctx context.Context, userID, householdID string) error {
		_, err := f.memberships.GetMembership(ctx, userID, householdID)
		return err
	}
	subjects := subjectapp.NewService(subjectpostgres.NewRepository(f.app),
		mapNotFound(func(ctx context.Context, u, h string) error { _, err := households.Get(ctx, u, h); return err }, subjectapp.ErrNotFound, subjectapp.ErrUnavailable),
		mapNotFound(membership, subjectapp.ErrNotFound, subjectapp.ErrUnavailable))
	items := itemapp.NewService(itempostgres.NewRepository(f.app),
		func(ctx context.Context, userID, householdID string) (string, error) {
			h, err := households.Get(ctx, userID, householdID)
			switch {
			case err == nil:
				return h.Timezone, nil
			case errors.Is(err, household.ErrNotFound):
				return "", itemapp.ErrNotFound
			default:
				return "", itemapp.ErrUnavailable
			}
		},
		func(ctx context.Context, userID, householdID, subjectID string) (bool, error) {
			s, err := subjects.Get(ctx, userID, householdID, subjectID)
			switch {
			case err == nil:
				return s.Archived, nil
			case errors.Is(err, subjectapp.ErrNotFound):
				return false, itemapp.ErrNotFound
			default:
				return false, itemapp.ErrUnavailable
			}
		},
		mapNotFound(membership, itemapp.ErrNotFound, itemapp.ErrUnavailable),
		f.clock.now)
	tools := []identityhttp.MCPTool{householdmcp.ListHouseholds(households)}
	tools = append(tools, itemmcp.ReadTools(items)...)
	tools = append(tools, subjectmcp.ReadTools(subjects)...)
	endpoint, err := identityhttp.NewMCPEndpoint(f.mcp, mcpEndpointIssuer, 5*time.Second, tools)
	if err != nil {
		t.Fatal(err)
	}
	f.endpoint = endpoint
	return f
}

// mapNotFound adapts a membership lookup to a module's errors, exactly as the
// cmd/tendo adapters do: household.ErrNotFound becomes notFound, anything else
// unavailable.
func mapNotFound(lookup func(ctx context.Context, userID, householdID string) error, notFound, unavailable error) func(ctx context.Context, userID, householdID string) error {
	return func(ctx context.Context, userID, householdID string) error {
		err := lookup(ctx, userID, householdID)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, household.ErrNotFound):
			return notFound
		default:
			return unavailable
		}
	}
}

// toolReply is one tools/call answer: the structured result, or the complete
// error result as raw JSON (so two refusals can be compared byte for byte).
type toolReply struct {
	out   map[string]any
	error string
}

// callTool sends one tools/call. It asserts nothing, so it is safe from
// goroutines; a transport failure is reported as an error result.
func (f *mcpEndpointFixture) callTool(token, name string, args map[string]any) toolReply {
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	reply := f.post(token, fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":%s}`, params))
	result, _ := reply.body["result"].(map[string]any)
	if reply.code != 200 || result == nil {
		return toolReply{error: fmt.Sprintf("transport status %d: %s", reply.code, reply.raw)}
	}
	if result["isError"] == true {
		raw, _ := json.Marshal(result)
		return toolReply{error: string(raw)}
	}
	out, _ := result["structuredContent"].(map[string]any)
	return toolReply{out: out}
}

func (f *mcpEndpointFixture) mustCall(t *testing.T, token, name string, args map[string]any) map[string]any {
	t.Helper()
	r := f.callTool(token, name, args)
	if r.error != "" {
		t.Fatalf("%s %v: %s", name, args, r.error)
	}
	return r.out
}

// rowIDs returns the "id" of every row under key.
func rowIDs(out map[string]any, key string) []string {
	rows, _ := out[key].([]any)
	found := make([]string, 0, len(rows))
	for _, row := range rows {
		m, _ := row.(map[string]any)
		found = append(found, fmt.Sprint(m["id"]))
	}
	return found
}

// mcpToolsWorld is two households with one member each, a subject, two items
// with distinctive titles and a completion each.
type mcpToolsWorld struct {
	userA, userB             string
	tokenA, tokenB           string
	houseA, houseB           string
	subjectA, subjectB       string
	itemA, itemB             string
	completionA, completionB string
}

func newMCPToolsWorld(t *testing.T, f *mcpEndpointFixture) mcpToolsWorld {
	t.Helper()
	w := mcpToolsWorld{userA: f.newUser(), userB: f.newUser()}
	f.password(w.userA)
	f.password(w.userB)
	w.houseA, w.houseB = f.newHousehold("alpha home"), f.newHousehold("beta flat")
	f.join(w.userA, w.houseA, household.RoleAdmin)
	f.join(w.userB, w.houseB, household.RoleAdmin)
	w.subjectA = f.addSubject(w.houseA, "Alpha car", "")
	w.subjectB = f.addSubject(w.houseB, "Beta car", "")
	w.itemA = f.addItem(w.houseA, w.subjectA, "Zebra oil alpha", w.userA, false, false)
	w.itemB = f.addItem(w.houseB, w.subjectB, "Zebra oil beta", w.userB, false, false)
	if _, err := f.admin.Exec(f.ctx, `UPDATE items SET notes = 'Quokka gasket' WHERE id = $1::uuid`, w.itemB); err != nil {
		t.Fatalf("foreign notes: %v", err)
	}
	w.completionA = f.addCompletion(w.houseA, w.itemA, w.userA, "key-a")
	w.completionB = f.addCompletion(w.houseB, w.itemB, w.userB, "key-b")
	w.tokenA = f.mint(t, w.userA, identityapp.MCPScopeRead).AccessToken
	w.tokenB = f.mint(t, w.userB, identityapp.MCPScopeRead).AccessToken
	return w
}

// mcpToolsAddCompletion records one more completion receipt of an item. Each
// receipt needs its own item version before it.
func mcpToolsAddCompletion(t *testing.T, f *mcpEndpointFixture, householdID, itemID, userID string, versionBefore int64) {
	t.Helper()
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO item_completions(id, household_id, item_id, completed_on,
		completed_by_user_id, prior_workflow_state, item_version_before, idempotency_key, request_fingerprint)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, '2026-01-05', $3::uuid, 'open', $4, $5, decode(repeat('cd', 32), 'hex'))`,
		householdID, itemID, userID, versionBefore, fmt.Sprintf("extra-%d", versionBefore)); err != nil {
		t.Fatalf("add completion: %v", err)
	}
}

// TestMCPReadToolsServeOnlyTheCallersHouseholdAgainstPostgres is the positive
// baseline: each tool returns the caller's own records.
func TestMCPReadToolsServeOnlyTheCallersHouseholdAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)

	if got := rowIDs(f.mustCall(t, w.tokenA, "list_items", map[string]any{"householdId": w.houseA}), "items"); len(got) != 1 || got[0] != w.itemA {
		t.Fatalf("list_items=%v want [%s]", got, w.itemA)
	}
	item := f.mustCall(t, w.tokenA, "get_item", map[string]any{"householdId": w.houseA, "itemId": w.itemA})
	if item["title"] != "Zebra oil alpha" || item["subjectId"] != w.subjectA || item["responsibleUserId"] != w.userA || item["attention"] != "needs_attention" {
		t.Fatalf("get_item=%v", item)
	}
	if got := rowIDs(f.mustCall(t, w.tokenA, "list_completions", map[string]any{"householdId": w.houseA, "itemId": w.itemA}), "completions"); len(got) != 1 || got[0] != w.completionA {
		t.Fatalf("list_completions=%v want [%s]", got, w.completionA)
	}
	if got := rowIDs(f.mustCall(t, w.tokenA, "list_subjects", map[string]any{"householdId": w.houseA}), "subjects"); len(got) != 1 || got[0] != w.subjectA {
		t.Fatalf("list_subjects=%v want [%s]", got, w.subjectA)
	}
	if s := f.mustCall(t, w.tokenA, "get_subject", map[string]any{"householdId": w.houseA, "subjectId": w.subjectA}); s["name"] != "Alpha car" {
		t.Fatalf("get_subject=%v", s)
	}
	if got := rowIDs(f.mustCall(t, w.tokenA, "list_items", map[string]any{"householdId": w.houseA, "q": "zebra OIL"}), "items"); len(got) != 1 || got[0] != w.itemA {
		t.Fatalf("search=%v want [%s]", got, w.itemA)
	}
}

// TestMCPReadToolsRefuseEveryForeignReferenceIdenticallyAgainstPostgres covers
// the #457 cross-household list for the read tools: a foreign household, a
// foreign item or subject under the caller's own household id, history of a
// foreign item, unknown and malformed ids. Every refusal is the same result.
func TestMCPReadToolsRefuseEveryForeignReferenceIdenticallyAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)

	cases := map[string]struct {
		tool string
		args map[string]any
	}{
		"list_items foreign household":         {"list_items", map[string]any{"householdId": w.houseB}},
		"list_items unknown household":         {"list_items", map[string]any{"householdId": mcpToolsMissingID}},
		"list_items uppercase foreign":         {"list_items", map[string]any{"householdId": strings.ToUpper(w.houseB)}},
		"search in foreign household":          {"list_items", map[string]any{"householdId": w.houseB, "q": "Zebra"}},
		"get_item foreign household":           {"get_item", map[string]any{"householdId": w.houseB, "itemId": w.itemB}},
		"get_item foreign item, own household": {"get_item", map[string]any{"householdId": w.houseA, "itemId": w.itemB}},
		"get_item own item, foreign household": {"get_item", map[string]any{"householdId": w.houseB, "itemId": w.itemA}},
		"get_item unknown item":                {"get_item", map[string]any{"householdId": w.houseA, "itemId": mcpToolsMissingID}},
		"get_item completion id as item":       {"get_item", map[string]any{"householdId": w.houseA, "itemId": w.completionA}},
		"history foreign item, own household":  {"list_completions", map[string]any{"householdId": w.houseA, "itemId": w.itemB}},
		"history foreign household":            {"list_completions", map[string]any{"householdId": w.houseB, "itemId": w.itemB}},
		"history unknown item":                 {"list_completions", map[string]any{"householdId": w.houseA, "itemId": mcpToolsMissingID}},
		"list_subjects foreign household":      {"list_subjects", map[string]any{"householdId": w.houseB}},
		"get_subject foreign, own household":   {"get_subject", map[string]any{"householdId": w.houseA, "subjectId": w.subjectB}},
		"get_subject foreign household":        {"get_subject", map[string]any{"householdId": w.houseB, "subjectId": w.subjectB}},
		"get_subject item id as subject":       {"get_subject", map[string]any{"householdId": w.houseA, "subjectId": w.itemA}},
		"get_subject unknown":                  {"get_subject", map[string]any{"householdId": w.houseA, "subjectId": mcpToolsMissingID}},
	}
	var want string
	for name, tc := range cases {
		got := f.callTool(w.tokenA, tc.tool, tc.args)
		if got.error == "" {
			t.Fatalf("%s: answered %v", name, got.out)
		}
		if want == "" {
			want = got.error
		}
		if got.error != want {
			t.Fatalf("%s: refusal %s differs from %s", name, got.error, want)
		}
	}
	if !strings.Contains(want, identityhttp.ErrMCPToolNotFound.Error()) || strings.Contains(want, w.houseB) || strings.Contains(want, "beta") {
		t.Fatalf("refusal %s must be the fixed not-found text with no foreign detail", want)
	}
}

// TestMCPListItemsForeignFiltersMatchNothingAgainstPostgres pins that foreign
// subjects and members as filters, and search, never reach another household.
func TestMCPListItemsForeignFiltersMatchNothingAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	for name, args := range map[string]map[string]any{
		"foreign subject":     {"householdId": w.houseA, "subjectId": w.subjectB},
		"foreign responsible": {"householdId": w.houseA, "responsibleUserId": w.userB},
		"foreign title":       {"householdId": w.houseA, "q": "beta"},
		"foreign title exact": {"householdId": w.houseA, "q": "Zebra oil beta"},
		"foreign notes":       {"householdId": w.houseA, "q": "Quokka"},
	} {
		out := f.mustCall(t, w.tokenA, "list_items", args)
		if got := rowIDs(out, "items"); len(got) != 0 || out["nextCursor"] != "" {
			t.Fatalf("%s: items=%v next=%v; want none", name, got, out["nextCursor"])
		}
	}
	if got := rowIDs(f.mustCall(t, w.tokenA, "list_items", map[string]any{"householdId": w.houseA, "q": "Zebra"}), "items"); len(got) != 1 || got[0] != w.itemA {
		t.Fatalf("shared search term: %v want only %s", got, w.itemA)
	}
	if got := rowIDs(f.mustCall(t, w.tokenB, "list_items", map[string]any{"householdId": w.houseB, "q": "quokka"}), "items"); len(got) != 1 || got[0] != w.itemB {
		t.Fatalf("notes search in the own household: %v want %s", got, w.itemB)
	}
}

// TestMCPReadToolsRefuseAWriteOnlyGrantAgainstPostgres calls every read tool
// with a real grant holding only tendo:write: tendo:write does not imply
// tendo:read, so the endpoint's front check answers each call with 403
// insufficient_scope before any tool runs.
func TestMCPReadToolsRefuseAWriteOnlyGrantAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	token := f.mint(t, w.userA, identityapp.MCPScopeWrite).AccessToken
	for tool, args := range map[string]map[string]any{
		"list_items":       {"householdId": w.houseA},
		"get_item":         {"householdId": w.houseA, "itemId": w.itemA},
		"list_completions": {"householdId": w.houseA, "itemId": w.itemA},
		"list_subjects":    {"householdId": w.houseA},
		"get_subject":      {"householdId": w.houseA, "subjectId": w.subjectA},
	} {
		params, _ := json.Marshal(map[string]any{"name": tool, "arguments": args})
		reply := f.post(token, fmt.Sprintf(`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":%s}`, params))
		if reply.code != http.StatusForbidden || reply.body["error"] != "insufficient_scope" {
			t.Fatalf("%s with tendo:write only: status=%d body=%s; want 403 insufficient_scope", tool, reply.code, reply.raw)
		}
	}
}

// TestMCPReadCursorsReplayedInAnotherContextAreRefusedAgainstPostgres pages a
// list, then replays the cursor where it does not belong. The caller is a
// member of both households, so the refusal is the cursor binding, not
// membership.
func TestMCPReadCursorsReplayedInAnotherContextAreRefusedAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	f.join(w.userA, w.houseB, household.RoleMember)
	f.join(w.userB, w.houseA, household.RoleMember)
	tokenA := f.mint(t, w.userA, identityapp.MCPScopeRead).AccessToken
	tokenB := f.mint(t, w.userB, identityapp.MCPScopeRead).AccessToken
	for i := range 2 {
		f.addItem(w.houseA, w.subjectA, fmt.Sprintf("Zebra extra %d", i), "", false, false)
		f.addItem(w.houseB, w.subjectB, fmt.Sprintf("Zebra extra %d", i), "", false, false)
		f.addSubject(w.houseA, fmt.Sprintf("Extra %d", i), "")
	}
	for i := range 2 {
		mcpToolsAddCompletion(t, f, w.houseA, w.itemA, w.userA, int64(i+2))
	}

	first := f.mustCall(t, tokenA, "list_items", map[string]any{"householdId": w.houseA, "q": "Zebra", "limit": 1})
	itemsCursor, _ := first["nextCursor"].(string)
	plain := f.mustCall(t, tokenA, "list_items", map[string]any{"householdId": w.houseA, "limit": 1})
	plainCursor, _ := plain["nextCursor"].(string)
	subjects := f.mustCall(t, tokenA, "list_subjects", map[string]any{"householdId": w.houseA, "limit": 1})
	subjectsCursor, _ := subjects["nextCursor"].(string)
	history := f.mustCall(t, tokenA, "list_completions", map[string]any{"householdId": w.houseA, "itemId": w.itemA, "limit": 1})
	historyCursor, _ := history["nextCursor"].(string)
	if itemsCursor == "" || plainCursor == "" || subjectsCursor == "" || historyCursor == "" {
		t.Fatalf("expected a second page everywhere: %q %q %q %q", itemsCursor, plainCursor, subjectsCursor, historyCursor)
	}
	if got := rowIDs(f.mustCall(t, tokenA, "list_items", map[string]any{"householdId": w.houseA, "q": "Zebra", "limit": 1, "cursor": itemsCursor}), "items"); len(got) != 1 {
		t.Fatalf("the same call must continue: %v", got)
	}

	replays := map[string]struct {
		token, tool string
		args        map[string]any
	}{
		"search cursor, other household":   {tokenA, "list_items", map[string]any{"householdId": w.houseB, "q": "Zebra", "limit": 1, "cursor": itemsCursor}},
		"search cursor, other search":      {tokenA, "list_items", map[string]any{"householdId": w.houseA, "q": "zebra", "limit": 1, "cursor": itemsCursor}},
		"search cursor, other user":        {tokenB, "list_items", map[string]any{"householdId": w.houseA, "q": "Zebra", "limit": 1, "cursor": itemsCursor}},
		"plain cursor, other household":    {tokenA, "list_items", map[string]any{"householdId": w.houseB, "limit": 1, "cursor": plainCursor}},
		"plain cursor, archived":           {tokenA, "list_items", map[string]any{"householdId": w.houseA, "archived": true, "cursor": plainCursor}},
		"subjects cursor, other household": {tokenA, "list_subjects", map[string]any{"householdId": w.houseB, "cursor": subjectsCursor}},
		"subjects cursor, as items":        {tokenA, "list_items", map[string]any{"householdId": w.houseA, "cursor": subjectsCursor}},
		"history cursor, other item":       {tokenA, "list_completions", map[string]any{"householdId": w.houseB, "itemId": w.itemB, "cursor": historyCursor}},
		"history cursor, other order":      {tokenA, "list_completions", map[string]any{"householdId": w.houseA, "itemId": w.itemA, "order": "newest", "cursor": historyCursor}},
		"history cursor, other user":       {tokenB, "list_completions", map[string]any{"householdId": w.houseA, "itemId": w.itemA, "cursor": historyCursor}},
	}
	for name, r := range replays {
		got := f.callTool(r.token, r.tool, r.args)
		if !strings.Contains(got.error, identityhttp.ErrMCPToolBadCursor.Error()) {
			t.Fatalf("%s: got %v %s; want the cursor refusal", name, got.out, got.error)
		}
	}
}

// TestMCPReadToolsStopAtMembershipRemovalAgainstPostgres removes the caller
// from the household between two calls with the real removal service. The
// next call, including a continuation cursor, is the same not-found refusal.
func TestMCPReadToolsStopAtMembershipRemovalAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	member := f.newUser()
	f.password(member)
	f.join(member, w.houseA, household.RoleMember)
	f.addItem(w.houseA, w.subjectA, "Second item", "", false, false)
	token := f.mint(t, member, identityapp.MCPScopeRead).AccessToken

	first := f.mustCall(t, token, "list_items", map[string]any{"householdId": w.houseA, "limit": 1})
	cursor, _ := first["nextCursor"].(string)
	if cursor == "" {
		t.Fatal("expected a second page")
	}
	f.mustCall(t, token, "get_item", map[string]any{"householdId": w.houseA, "itemId": w.itemA})

	if err := f.service.RemoveMember(f.ctx, w.userA, w.houseA, member, 1); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	calls := map[string]struct {
		tool string
		args map[string]any
	}{
		"list_items continuation": {"list_items", map[string]any{"householdId": w.houseA, "limit": 1, "cursor": cursor}},
		"list_items":              {"list_items", map[string]any{"householdId": w.houseA}},
		"get_item":                {"get_item", map[string]any{"householdId": w.houseA, "itemId": w.itemA}},
		"list_completions":        {"list_completions", map[string]any{"householdId": w.houseA, "itemId": w.itemA}},
		"list_subjects":           {"list_subjects", map[string]any{"householdId": w.houseA}},
		"get_subject":             {"get_subject", map[string]any{"householdId": w.houseA, "subjectId": w.subjectA}},
	}
	foreign := f.callTool(token, "get_item", map[string]any{"householdId": w.houseB, "itemId": w.itemB}).error
	for name, c := range calls {
		if got := f.callTool(token, c.tool, c.args); got.error != foreign {
			t.Fatalf("%s after removal: %v %s; want the foreign-household refusal %s", name, got.out, got.error, foreign)
		}
	}
	if rows, err := mcpEndpointParseList(f.post(token, mcpEndpointListCall(4))); err != nil || len(rows) != 0 {
		t.Fatalf("list_households after removal: %v %v", rows, err)
	}
}

// TestMCPReadToolsConcurrentCallersStayIsolatedAgainstPostgres interleaves
// list, search and detail calls from two users of disjoint households.
func TestMCPReadToolsConcurrentCallersStayIsolatedAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	type caller struct{ token, house, item, foreignItem string }
	callers := []caller{{w.tokenA, w.houseA, w.itemA, w.itemB}, {w.tokenB, w.houseB, w.itemB, w.itemA}}
	refusal := f.callTool(w.tokenA, "get_item", map[string]any{"householdId": w.houseA, "itemId": mcpToolsMissingID}).error
	if !strings.Contains(refusal, identityhttp.ErrMCPToolNotFound.Error()) {
		t.Fatalf("reference refusal %s", refusal)
	}
	const perCaller = 25
	var wg sync.WaitGroup
	errs := make(chan error, perCaller*len(callers)*2)
	for n := range perCaller {
		for _, c := range callers {
			wg.Add(1)
			go func(c caller, n int) {
				defer wg.Done()
				args := map[string]any{"householdId": c.house}
				if n%2 == 0 {
					args["q"] = "Zebra"
				}
				r := f.callTool(c.token, "list_items", args)
				if got := rowIDs(r.out, "items"); r.error != "" || len(got) != 1 || got[0] != c.item {
					errs <- fmt.Errorf("list_items for %s: %v %s", c.house, got, r.error)
				}
				if r := f.callTool(c.token, "get_item", map[string]any{"householdId": c.house, "itemId": c.foreignItem}); r.error != refusal {
					errs <- fmt.Errorf("foreign item %s: %v %s; want the not-found refusal", c.foreignItem, r.out, r.error)
				}
			}(c, n)
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
