package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/platform/database"
)

func TestCompletionOrderExplainUsesBackwardTargetIndex(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var targetHousehold, foreignHousehold, targetSubject, foreignSubject, actor, targetItem string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('completion explain target','UTC') RETURNING id::text`).Scan(&targetHousehold))
	must(admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('completion explain foreign','UTC') RETURNING id::text`).Scan(&foreignHousehold))
	must(admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('completion explain actor') RETURNING id::text`).Scan(&actor))
	must(admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','target') RETURNING id::text`, targetHousehold).Scan(&targetSubject))
	must(admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','foreign') RETURNING id::text`, foreignHousehold).Scan(&foreignSubject))
	must(admin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title) VALUES($1::uuid,$2::uuid,'target item') RETURNING id::text`, targetHousehold, targetSubject).Scan(&targetItem))
	mustExec := func(query string, args ...any) {
		t.Helper()
		_, err := admin.Exec(ctx, query, args...)
		must(err)
	}
	// A long-lived daily item (2k receipts, ~5.5 years) in a household whose other 1000 items carry ~48k
	// receipts, plus a foreign household with another 40k. The target item holds ~2% of all receipts, as
	// one item among many would; at that share the planner must use the household/item index instead of
	// filtering a backward primary-key walk. Receipt IDs ascend with n and interleave the streams, as
	// UUIDv7 receipts recorded over time would.
	mustExec(`INSERT INTO items(id,household_id,subject_id,title) SELECT ('0198a2f2-0000-7000-8000-'||lpad((300000+n)::text,12,'0'))::uuid,$1::uuid,$2::uuid,'target unrelated-'||n::text FROM generate_series(1,1000) n`, targetHousehold, targetSubject)
	mustExec(`INSERT INTO items(id,household_id,subject_id,title) SELECT ('0198a2f2-0000-7000-8000-'||lpad((400000+n)::text,12,'0'))::uuid,$1::uuid,$2::uuid,'foreign unrelated-'||n::text FROM generate_series(1,1000) n`, foreignHousehold, foreignSubject)
	mustExec(`INSERT INTO item_completions(id,household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint)
		SELECT ('0198a2f1-0000-7000-8000-'||lpad(n::text,12,'0'))::uuid,
			CASE WHEN n%9>=5 THEN $2::uuid ELSE $1::uuid END,
			CASE WHEN n%45=0 THEN $3::uuid
				WHEN n%9<5 THEN ('0198a2f2-0000-7000-8000-'||lpad((300000+n%1000+1)::text,12,'0'))::uuid
				ELSE ('0198a2f2-0000-7000-8000-'||lpad((400000+n%1000+1)::text,12,'0'))::uuid END,
			DATE '2026-01-01',$4::uuid,'open',n,'explain-'||n::text,decode(repeat('aa',32),'hex')
		FROM generate_series(1,90000) n`, targetHousehold, foreignHousehold, targetItem, actor)
	mustExec(`ANALYZE items`)
	mustExec(`ANALYZE item_completions`)
	tx, err := app.Begin(ctx)
	must(err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	capture := &capturingDB{Tx: tx}
	rows, err := NewRepository(capture).ListCompletions(ctx, targetHousehold, targetItem, "ffffffff-ffff-ffff-ffff-ffffffffffff", 51, true)
	must(err)
	if len(rows) != 51 {
		t.Fatalf("newest repository page rows=%d want=51", len(rows))
	}
	for _, row := range rows {
		if row.HouseholdID != targetHousehold || row.ItemID != targetItem {
			t.Fatalf("newest query leaked receipt: %+v", row)
		}
	}
	var payload []byte
	must(tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+capture.query, capture.args...).Scan(&payload))
	must(tx.Commit(ctx))
	var docs []map[string]any
	must(json.Unmarshal(payload, &docs))
	plan := docs[0]["Plan"].(map[string]any)
	if !hasBackwardCompletionIndex(plan) {
		t.Fatalf("expected backward index scan on item_completions_household_item_id_idx, got %s", payload)
	}
	if planContainsNode(plan, "Sort") {
		t.Fatalf("unexpected Sort node: %s", payload)
	}
	t.Logf("newest completion query=%s args=%v execution_ms=%.3f", strings.Join(strings.Fields(capture.query), " "), capture.args, docs[0]["Execution Time"].(float64))
}

func hasBackwardCompletionIndex(plan map[string]any) bool {
	if name, ok := plan["Index Name"].(string); ok && name == "item_completions_household_item_id_idx" {
		return strings.Contains(plan["Node Type"].(string), "Index") && plan["Scan Direction"] == "Backward"
	}
	children, _ := plan["Plans"].([]any)
	for _, child := range children {
		if hasBackwardCompletionIndex(child.(map[string]any)) {
			return true
		}
	}
	return false
}

func planContainsNode(plan map[string]any, node string) bool {
	if plan["Node Type"] == node {
		return true
	}
	children, _ := plan["Plans"].([]any)
	for _, child := range children {
		if planContainsNode(child.(map[string]any), node) {
			return true
		}
	}
	return false
}
