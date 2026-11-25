package postgres

// Real-PostgreSQL tests for the MCP write tools (ADR 0023, #457 slice B3). The
// fixture serves the production tool set over the production services, with
// tokens minted through the real authorization flow.

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/household"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/bigtcze/tendo/backend/internal/schedule/scheduletest"
)

// mcpNotFound is the not-found tool error result as callTool marshals it.
const mcpNotFound = `{"content":[{"text":"the household or record was not found","type":"text"}],"isError":true}`

// writeToken mints a grant holding both scopes for user.
func (f *mcpEndpointFixture) writeToken(t *testing.T, user string) string {
	t.Helper()
	return f.mint(t, user, identityapp.MCPScopeRead, identityapp.MCPScopeWrite).AccessToken
}

func (f *mcpEndpointFixture) scalar(t *testing.T, query string, args ...any) string {
	t.Helper()
	var out string
	if err := f.admin.QueryRow(f.ctx, query, args...).Scan(&out); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

// itemSchedule renders the stored schedule of an item: attention date, anchor,
// done and version.
func (f *mcpEndpointFixture) itemSchedule(t *testing.T, itemID string) string {
	t.Helper()
	return f.scalar(t, `SELECT coalesce(attention_on::text,'-')||'|'||coalesce(attention_anchor_day::text,'-')||'|'||done::text||'|'||version::text
		FROM items WHERE id=$1::uuid`, itemID)
}

func num(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

func nested(out map[string]any, key string) map[string]any {
	m, _ := out[key].(map[string]any)
	return m
}

// TestMCPWriteToolsRunTheCanonicalServicesAgainstPostgres creates a subject
// and an item, edits it, marks it Done and undoes that, all through MCP, and
// checks the stored rows and the attribution to the token's user.
func TestMCPWriteToolsRunTheCanonicalServicesAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	token := f.writeToken(t, w.userA)

	subjectArgs := map[string]any{"householdId": w.houseA, "name": "Garden", "type": "home", "idempotencyKey": "subject-1"}
	subject := f.mustCall(t, token, "create_subject", subjectArgs)
	if subject["name"] != "Garden" || subject["type"] != "home" {
		t.Fatalf("create_subject=%v", subject)
	}
	if again := f.mustCall(t, token, "create_subject", subjectArgs); again["id"] != subject["id"] {
		t.Fatalf("create_subject replay=%v want id %v", again, subject["id"])
	}
	if got := f.scalar(t, `SELECT created_by_user_id::text||'|'||(creation_key LIKE 'mcp1.%')::text||'|'||(creation_key<>'subject-1')::text FROM subjects WHERE id=$1::uuid`, subject["id"]); got != w.userA+"|true|true" {
		t.Fatalf("subject attribution and derived key: %s", got)
	}

	itemArgs := map[string]any{
		"householdId": w.houseA, "subjectId": subject["id"], "title": "Prune roses", "idempotencyKey": "item-1",
		"notes": "north bed", "attentionOn": "2026-05-10", "responsibleUserId": w.userA,
		"recurrence": map[string]any{"intervalValue": 1, "intervalUnit": "month", "mode": "fixed"},
	}
	created := f.mustCall(t, token, "create_item", itemArgs)
	itemID, _ := created["id"].(string)
	if created["title"] != "Prune roses" || created["attentionOn"] != "2026-05-10" || num(created["version"]) != 1 {
		t.Fatalf("create_item=%v", created)
	}
	if again := f.mustCall(t, token, "create_item", itemArgs); again["id"] != itemID {
		t.Fatalf("create_item replay=%v", again)
	}
	if got := f.scalar(t, `SELECT created_by_user_id::text FROM items WHERE id=$1::uuid`, itemID); got != w.userA {
		t.Fatalf("item created_by=%s", got)
	}

	updated := f.mustCall(t, token, "update_item", map[string]any{"householdId": w.houseA, "itemId": itemID, "version": 1, "title": "Prune all roses", "notes": nil})
	if updated["title"] != "Prune all roses" || updated["notes"] != nil || updated["responsibleUserId"] != w.userA || num(updated["version"]) != 2 {
		t.Fatalf("update_item=%v", updated)
	}
	stale := f.callTool(token, "update_item", map[string]any{"householdId": w.houseA, "itemId": itemID, "version": 1, "title": "x"})
	if !strings.Contains(stale.error, "the record has changed") {
		t.Fatalf("stale version: %+v", stale)
	}

	f.today.at = time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	done := f.mustCall(t, token, "complete_item", map[string]any{"householdId": w.houseA, "itemId": itemID, "version": 2, "idempotencyKey": "done-1"})
	completion := nested(done, "completion")
	if done["replayed"] != false || num(done["itemVersion"]) != 3 || completion["nextAttentionOn"] != "2026-06-10" || completion["completedByUserId"] != w.userA {
		t.Fatalf("complete_item=%v", done)
	}
	replay := f.mustCall(t, token, "complete_item", map[string]any{"householdId": w.houseA, "itemId": itemID, "version": 2, "idempotencyKey": "done-1"})
	if replay["replayed"] != true || num(replay["itemVersion"]) != 3 || nested(replay, "completion")["id"] != completion["id"] {
		t.Fatalf("complete_item replay=%v", replay)
	}
	if got := f.scalar(t, `SELECT completed_by_user_id::text FROM item_completions WHERE id=$1::uuid`, completion["id"]); got != w.userA {
		t.Fatalf("completed_by=%s", got)
	}
	undone := f.mustCall(t, token, "undo_completion", map[string]any{"householdId": w.houseA, "itemId": itemID, "completionId": completion["id"], "version": 3})
	if num(nested(undone, "item")["version"]) != 4 || nested(undone, "item")["attentionOn"] != "2026-05-10" || nested(undone, "completion")["undoneByUserId"] != w.userA {
		t.Fatalf("undo_completion=%v", undone)
	}
	if got := f.itemSchedule(t, itemID); got != "2026-05-10|10|false|4" {
		t.Fatalf("schedule after undo=%s", got)
	}
}

// TestMCPUpdateItemKeepsAbsentApartFromNullAgainstPostgres: an absent field is
// kept and an explicit null clears it, for every nullable field.
func TestMCPUpdateItemKeepsAbsentApartFromNullAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	token := f.writeToken(t, w.userA)
	item := f.mustCall(t, token, "create_item", map[string]any{
		"householdId": w.houseA, "subjectId": w.subjectA, "title": "Filters", "idempotencyKey": "nullable",
		"notes": "kept", "attentionOn": "2026-06-01", "responsibleUserId": w.userA,
		"recurrence": map[string]any{"intervalValue": 2, "intervalUnit": "week", "mode": "after_completion"},
	})
	id := item["id"].(string)
	row := func() string {
		return f.scalar(t, `SELECT coalesce(responsible_user_id::text,'-')||'|'||coalesce(notes,'-')||'|'||coalesce(attention_on::text,'-')||'|'||
			coalesce(recurrence_interval_value::text,'-')||'|'||title FROM items WHERE id=$1::uuid`, id)
	}
	f.mustCall(t, token, "update_item", map[string]any{"householdId": w.houseA, "itemId": id, "version": 1, "title": "Filters 2"})
	if got, want := row(), w.userA+"|kept|2026-06-01|2|Filters 2"; got != want {
		t.Fatalf("absent fields: %s want %s", got, want)
	}
	version := int64(2)
	for _, field := range []string{"responsibleUserId", "notes", "attentionOn", "recurrence"} {
		f.mustCall(t, token, "update_item", map[string]any{"householdId": w.houseA, "itemId": id, "version": version, field: nil})
		version++
	}
	if got := row(); got != "-|-|-|-|Filters 2" {
		t.Fatalf("nulls: %s", got)
	}
	if empty := f.callTool(token, "update_item", map[string]any{"householdId": w.houseA, "itemId": id, "version": version}); !strings.Contains(empty.error, "changes nothing") {
		t.Fatalf("empty patch: %+v", empty)
	}
}

// TestMCPDoneFollowsTheRecurrenceGoldenTableAgainstPostgres drives every
// golden and mandatory coverage row through complete_item, then undoes every
// Done in reverse through undo_completion, which must restore the exact stored
// schedule and anchor of each step.
func TestMCPDoneFollowsTheRecurrenceGoldenTableAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	for _, tc := range append(scheduletest.Golden(), scheduletest.Coverage()...) {
		t.Run(tc.Name, func(t *testing.T) {
			token := f.writeToken(t, w.userA) // one grant per row keeps under the per-grant limit
			args := map[string]any{"householdId": w.houseA, "subjectId": w.subjectA, "title": "golden " + tc.Name,
				"idempotencyKey": "golden-" + strings.ReplaceAll(tc.Name, " ", "-"), "attentionOn": tc.Due}
			if tc.Policy != nil {
				args["recurrence"] = map[string]any{"intervalValue": tc.Policy.Interval.Value, "intervalUnit": string(tc.Policy.Interval.Unit), "mode": string(tc.Policy.Mode)}
			}
			id := f.mustCall(t, token, "create_item", args)["id"].(string)
			before := []string{}
			var receipts []string
			due := tc.Due
			for index, step := range tc.Steps {
				before = append(before, f.itemSchedule(t, id))
				f.today.at = scheduleNoon(scheduletest.Date(step.Completion))
				out := f.mustCall(t, token, "complete_item", map[string]any{"householdId": w.houseA, "itemId": id,
					"version": index + 1, "idempotencyKey": fmt.Sprintf("step-%d", index+1)})
				receipt := nested(out, "completion")
				next, _ := receipt["nextAttentionOn"].(string)
				if receipt["completedOn"] != step.Completion || out["replayed"] != false || num(out["itemVersion"]) != int64(index+2) {
					t.Fatalf("step %d: %v", index+1, out)
				}
				scheduletest.Check(t, tc.Name, index, due, step, next)
				// A recurring Done moves the date and stays open; a one-off Done
				// keeps its date and is done.
				stored := strings.Split(f.itemSchedule(t, id), "|")
				if (next != "" && (stored[0] != next || stored[2] != "false")) || (next == "" && (stored[2] != "true" || stored[0] != strings.Split(before[index], "|")[0])) {
					t.Fatalf("step %d: stored schedule %s; receipt next %q", index+1, stored, next)
				}
				receipts = append(receipts, receipt["id"].(string))
				due = next
			}
			for index := len(tc.Steps) - 1; index >= 0; index-- {
				version := int64(len(tc.Steps) + 1 + (len(tc.Steps) - 1 - index))
				f.mustCall(t, token, "undo_completion", map[string]any{"householdId": w.houseA, "itemId": id, "completionId": receipts[index], "version": version})
				got, want := f.itemSchedule(t, id), before[index]
				// Undo adds a version; compare the schedule part only.
				if got[:strings.LastIndex(got, "|")] != want[:strings.LastIndex(want, "|")] {
					t.Fatalf("undo of step %d: schedule %s want %s", index+1, got, want)
				}
			}
		})
	}
}

func scheduleNoon(d schedule.Date) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, time.UTC)
}

// TestMCPCompleteItemConcurrentRetriesMakeOneReceiptAgainstPostgres sends the
// same complete_item call with one key concurrently: one Done is recorded and
// every answer is that receipt.
func TestMCPCompleteItemConcurrentRetriesMakeOneReceiptAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	token := f.writeToken(t, w.userA)
	id := f.mustCall(t, token, "create_item", map[string]any{"householdId": w.houseA, "subjectId": w.subjectA, "title": "Race", "idempotencyKey": "race"})["id"].(string)
	const workers = 8
	replies := make([]toolReply, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range workers {
		wg.Go(func() {
			<-start
			replies[i] = f.callTool(token, "complete_item", map[string]any{"householdId": w.houseA, "itemId": id, "version": 1, "idempotencyKey": "same"})
		})
	}
	close(start)
	wg.Wait()
	fresh, ids := 0, map[any]bool{}
	for i, r := range replies {
		if r.error != "" {
			t.Fatalf("worker %d: %s", i, r.error)
		}
		if r.out["replayed"] == false {
			fresh++
		}
		ids[nested(r.out, "completion")["id"]] = true
	}
	if fresh != 1 || len(ids) != 1 {
		t.Fatalf("fresh=%d receipts=%v; want one Done", fresh, ids)
	}
	if got := f.scalar(t, `SELECT count(*)::text FROM item_completions WHERE item_id=$1::uuid`, id); got != "1" {
		t.Fatalf("stored receipts=%s", got)
	}
}

// TestMCPIdempotencyKeysAreIsolatedPerGrantAgainstPostgres: the same client
// key from two grants of one user is two actions; within a grant it replays.
func TestMCPIdempotencyKeysAreIsolatedPerGrantAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	first, second := f.writeToken(t, w.userA), f.writeToken(t, w.userA)
	args := map[string]any{"householdId": w.houseA, "subjectId": w.subjectA, "title": "Same key", "idempotencyKey": "shared"}
	a := f.mustCall(t, first, "create_item", args)["id"]
	b := f.mustCall(t, second, "create_item", args)["id"]
	if a == b || f.mustCall(t, first, "create_item", args)["id"] != a {
		t.Fatalf("grant isolation: first=%v second=%v", a, b)
	}
	args["title"] = "Other body"
	if reused := f.callTool(first, "create_item", args); !strings.Contains(reused.error, "already used for a different action") {
		t.Fatalf("key reuse: %+v", reused)
	}
	sa := f.mustCall(t, first, "create_subject", map[string]any{"householdId": w.houseA, "name": "S", "type": "custom", "idempotencyKey": "shared"})["id"]
	sb := f.mustCall(t, second, "create_subject", map[string]any{"householdId": w.houseA, "name": "S", "type": "custom", "idempotencyKey": "shared"})["id"]
	if sa == sb {
		t.Fatal("create_subject keys must be isolated per grant")
	}
}

// TestMCPWriteToolsRefuseEveryForeignReferenceIdenticallyAgainstPostgres: a
// foreign household, item, subject, member or completion, a completion of
// another item and unknown ids all give the not-found result, and nothing is
// written.
func TestMCPWriteToolsRefuseEveryForeignReferenceIdenticallyAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	token := f.writeToken(t, w.userA)
	otherItem := f.addItem(w.houseA, w.subjectA, "Other own item", "", false, false)
	otherCompletion := f.addCompletion(w.houseA, otherItem, w.userA, "other-key")
	snapshot := func() string {
		return f.scalar(t, `SELECT (SELECT count(*) FROM items)::text||'|'||(SELECT count(*) FROM subjects)::text||'|'||
			(SELECT count(*) FROM item_completions WHERE undone_at IS NULL)::text||'|'||(SELECT string_agg(version::text, ',' ORDER BY id) FROM items)`)
	}
	before := snapshot()
	keys := 0
	create := func(extra map[string]any) map[string]any {
		keys++
		args := map[string]any{"householdId": w.houseA, "subjectId": w.subjectA, "title": "t", "idempotencyKey": fmt.Sprintf("foreign-%d", keys)}
		for k, v := range extra {
			args[k] = v
		}
		return args
	}
	cases := map[string]struct {
		tool string
		args map[string]any
	}{
		"create_item foreign household":          {"create_item", create(map[string]any{"householdId": w.houseB, "subjectId": w.subjectB})},
		"create_item foreign subject":            {"create_item", create(map[string]any{"subjectId": w.subjectB})},
		"create_item unknown subject":            {"create_item", create(map[string]any{"subjectId": mcpToolsMissingID})},
		"create_item foreign responsible user":   {"create_item", create(map[string]any{"responsibleUserId": w.userB})},
		"create_subject foreign household":       {"create_subject", map[string]any{"householdId": w.houseB, "name": "x", "type": "pet", "idempotencyKey": "k"}},
		"update_item foreign item":               {"update_item", map[string]any{"householdId": w.houseA, "itemId": w.itemB, "version": 1, "title": "x"}},
		"update_item foreign household":          {"update_item", map[string]any{"householdId": w.houseB, "itemId": w.itemB, "version": 1, "title": "x"}},
		"update_item foreign subject":            {"update_item", map[string]any{"householdId": w.houseA, "itemId": w.itemA, "version": 1, "subjectId": w.subjectB}},
		"update_item foreign responsible user":   {"update_item", map[string]any{"householdId": w.houseA, "itemId": w.itemA, "version": 1, "responsibleUserId": w.userB}},
		"complete_item foreign item":             {"complete_item", map[string]any{"householdId": w.houseA, "itemId": w.itemB, "version": 1, "idempotencyKey": "k"}},
		"create_item unknown household":          {"create_item", create(map[string]any{"householdId": mcpToolsMissingID})},
		"create_subject unknown household":       {"create_subject", map[string]any{"householdId": mcpToolsMissingID, "name": "x", "type": "pet", "idempotencyKey": "k"}},
		"update_item unknown household":          {"update_item", map[string]any{"householdId": mcpToolsMissingID, "itemId": w.itemA, "version": 1, "title": "x"}},
		"update_item unknown item":               {"update_item", map[string]any{"householdId": w.houseA, "itemId": mcpToolsMissingID, "version": 1, "title": "x"}},
		"complete_item foreign household":        {"complete_item", map[string]any{"householdId": w.houseB, "itemId": w.itemB, "version": 1, "idempotencyKey": "k"}},
		"complete_item unknown household":        {"complete_item", map[string]any{"householdId": mcpToolsMissingID, "itemId": w.itemA, "version": 1, "idempotencyKey": "k"}},
		"undo unknown household":                 {"undo_completion", map[string]any{"householdId": mcpToolsMissingID, "itemId": w.itemA, "completionId": otherCompletion, "version": 1}},
		"undo unknown item":                      {"undo_completion", map[string]any{"householdId": w.houseA, "itemId": mcpToolsMissingID, "completionId": otherCompletion, "version": 1}},
		"complete_item unknown item":             {"complete_item", map[string]any{"householdId": w.houseA, "itemId": mcpToolsMissingID, "version": 1, "idempotencyKey": "k"}},
		"undo completion of another own item":    {"undo_completion", map[string]any{"householdId": w.houseA, "itemId": w.itemA, "completionId": otherCompletion, "version": 1}},
		"undo foreign completion, own household": {"undo_completion", map[string]any{"householdId": w.houseA, "itemId": w.itemA, "completionId": w.completionB, "version": 1}},
		"undo foreign household":                 {"undo_completion", map[string]any{"householdId": w.houseB, "itemId": w.itemB, "completionId": w.completionB, "version": 1}},
		"undo unknown completion":                {"undo_completion", map[string]any{"householdId": w.houseA, "itemId": w.itemA, "completionId": mcpToolsMissingID, "version": 1}},
	}
	for name, tc := range cases {
		if got := f.callTool(token, tc.tool, tc.args); got.error != mcpNotFound {
			t.Fatalf("%s: %+v; want the not-found result", name, got)
		}
	}
	if after := snapshot(); after != before {
		t.Fatalf("refused calls wrote: %s -> %s", before, after)
	}
}

// TestMCPWriteToolsFollowThePolicyAndRevocationAgainstPostgres: a read-only
// grant is challenged to step up while the policy allows tendo:write; once the
// policy allows only tendo:read a write grant gets the scope tool error; a
// revoked grant or a switched-off account is refused on the next write call.
// Nothing is written by any refused call.
func TestMCPWriteToolsFollowThePolicyAndRevocationAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	args := map[string]any{"householdId": w.houseA, "itemId": w.itemA, "version": 1, "idempotencyKey": "policy"}
	unchanged := func(why string) {
		t.Helper()
		if got := f.scalar(t, `SELECT version::text FROM items WHERE id=$1::uuid`, w.itemA); got != "1" {
			t.Fatalf("%s: item version %s; the refused call wrote", why, got)
		}
	}
	params := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"complete_item","arguments":{"householdId":"` + w.houseA + `","itemId":"` + w.itemA + `","version":1,"idempotencyKey":"policy"}}}`
	reply := f.post(w.tokenA, params)
	want := `Bearer error="insufficient_scope", scope="tendo:read tendo:write", resource_metadata="` + mcpEndpointIssuer + `/.well-known/oauth-protected-resource/mcp"`
	if reply.code != http.StatusForbidden || reply.header.Get("WWW-Authenticate") != want {
		t.Fatalf("read-only grant: status=%d challenge=%q", reply.code, reply.header.Get("WWW-Authenticate"))
	}
	unchanged("step-up")

	token := f.writeToken(t, w.userA)
	f.setUserScopes(t, w.userA, identityapp.MCPScopeRead)
	if got := f.callTool(token, "complete_item", args); got.error != mcpScopeRefusal {
		t.Fatalf("read-only policy: %+v; want the scope tool error", got)
	}
	unchanged("read-only policy")
	f.setUserScopes(t, w.userA, identityapp.MCPScopeRead, identityapp.MCPScopeWrite)

	if _, err := f.admin.Exec(f.ctx, `UPDATE mcp_account_policies SET user_enabled=false WHERE user_id=$1::uuid`, w.userA); err != nil {
		t.Fatal(err)
	}
	if r := f.post(token, params); r.code != http.StatusUnauthorized {
		t.Fatalf("account switched off: status=%d", r.code)
	}
	unchanged("account switched off")
	if _, err := f.admin.Exec(f.ctx, `UPDATE mcp_account_policies SET user_enabled=true WHERE user_id=$1::uuid`, w.userA); err != nil {
		t.Fatal(err)
	}

	grant := f.scalar(t, `SELECT g.id::text FROM mcp_grants g WHERE g.user_id=$1::uuid AND g.revoked_at IS NULL AND 'tendo:write'=ANY(g.scopes) ORDER BY g.created_at DESC LIMIT 1`, w.userA)
	if err := f.mcp.RevokeGrantForUser(f.ctx, w.userA, grant); err != nil {
		t.Fatalf("RevokeGrantForUser: %v", err)
	}
	if r := f.post(token, params); r.code != http.StatusUnauthorized {
		t.Fatalf("revoked grant: status=%d", r.code)
	}
	unchanged("revoked grant")
}

// TestMCPWriteToolsStopAtMembershipRemovalAgainstPostgres removes the caller
// from the household with the real removal service between two calls: every
// write tool then gives the not-found result and writes nothing.
func TestMCPWriteToolsStopAtMembershipRemovalAgainstPostgres(t *testing.T) {
	f := newMCPToolsFixture(t)
	w := newMCPToolsWorld(t, f)
	member := f.newUser()
	f.password(member)
	f.join(member, w.houseA, household.RoleMember)
	token := f.writeToken(t, member)
	own := f.mustCall(t, token, "create_item", map[string]any{"householdId": w.houseA, "subjectId": w.subjectA, "title": "Before removal", "idempotencyKey": "before"})
	done := f.mustCall(t, token, "complete_item", map[string]any{"householdId": w.houseA, "itemId": own["id"], "version": 1, "idempotencyKey": "before"})
	if err := f.service.RemoveMember(f.ctx, w.userA, w.houseA, member, 1); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	snapshot := func() string {
		return f.scalar(t, `SELECT (SELECT count(*) FROM items)::text||'|'||(SELECT count(*) FROM subjects)::text||'|'||
			(SELECT count(*) FROM item_completions WHERE undone_at IS NULL)::text||'|'||(SELECT string_agg(version::text, ',' ORDER BY id) FROM items)`)
	}
	before := snapshot()
	calls := map[string]struct {
		tool string
		args map[string]any
	}{
		"create_item":        {"create_item", map[string]any{"householdId": w.houseA, "subjectId": w.subjectA, "title": "After", "idempotencyKey": "after"}},
		"create_item replay": {"create_item", map[string]any{"householdId": w.houseA, "subjectId": w.subjectA, "title": "Before removal", "idempotencyKey": "before"}},
		"update_item":        {"update_item", map[string]any{"householdId": w.houseA, "itemId": own["id"], "version": 2, "title": "x"}},
		"complete_item":      {"complete_item", map[string]any{"householdId": w.houseA, "itemId": w.itemA, "version": 1, "idempotencyKey": "after"}},
		"undo_completion":    {"undo_completion", map[string]any{"householdId": w.houseA, "itemId": own["id"], "completionId": nested(done, "completion")["id"], "version": 2}},
		"create_subject":     {"create_subject", map[string]any{"householdId": w.houseA, "name": "x", "type": "pet", "idempotencyKey": "after"}},
	}
	for name, c := range calls {
		if got := f.callTool(token, c.tool, c.args); got.error != mcpNotFound {
			t.Fatalf("%s after removal: %+v; want the not-found result", name, got)
		}
	}
	if after := snapshot(); after != before {
		t.Fatalf("refused calls wrote: %s -> %s", before, after)
	}
}
