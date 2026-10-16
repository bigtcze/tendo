package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
)

func TestItemSearchExplain(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var target, foreign, targetSubject, foreignSubject string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('search explain target','UTC') RETURNING id::text`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('search explain foreign','UTC') RETURNING id::text`).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','target') RETURNING id::text`, target).Scan(&targetSubject); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','foreign') RETURNING id::text`, foreign).Scan(&foreignSubject); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO items(id,household_id,subject_id,title,notes,archived,done)
SELECT ('0198a2f0-7c1e-7a53-9b0e-'||lpad(($3::bigint + n)::text,12,'0'))::uuid,
       CASE WHEN n <= 10000 THEN $1::uuid ELSE $2::uuid END,
       CASE WHEN n <= 10000 THEN $4::uuid ELSE $5::uuid END,
       CASE WHEN n % 3 = 0 THEN 'sharedword ' ELSE '' END ||
         CASE WHEN n <= 5 THEN 'raretoken ' ELSE '' END ||
         CASE WHEN n <= 4 THEN 'a%b ' ELSE CASE WHEN n <= 8 THEN 'axxb ' ELSE '' END END ||
         CASE WHEN n % 2 = 0 THEN 'Žluťoučký kůň ' ELSE '普通文字 ' END || 'item-' || n::text,
       CASE WHEN n % 3 = 0 THEN 'common notes ' ELSE 'ordinary notes ' END || CASE WHEN n % 2 = 1 THEN 'žluťoučký' ELSE '家族' END,
       false,false
FROM generate_series(1,20000) AS n`
	if _, err := admin.Exec(ctx, insert, target, foreign, 400000, targetSubject, foreignSubject); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `ANALYZE items`); err != nil {
		t.Fatal(err)
	}

	queries := []struct {
		label, q string
		want     int
	}{
		{"absent", "", 10000},
		{"rare", "raretoken", 5},
		{"common", "sharedword", 3333},
		{"miss", "notpresent", 0},
		{"two-char", "家族", 5000},
		{"literal-a%b", "a%b", 4},
	}
	t.Log("query | q case | top node types | trgm index | execution ms")
	for _, query := range queries {
		for _, sortCase := range []struct {
			name string
			sort item.ListSort
		}{
			{"ListItems", item.SortCreated},
			{"ListItemsByAttention", item.SortAttention},
		} {
			tx, err := app.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tx.Rollback(ctx) })
			capture := &capturingDB{Tx: tx}
			rows, err := NewRepository(capture).List(ctx, target, item.ListOptions{Today: mustTestDate(2026, time.March, 2), Sort: sortCase.sort, Limit: 101, AfterID: "00000000-0000-0000-0000-000000000000", Filters: item.ListFilters{Q: query.q}})
			if err != nil {
				t.Fatalf("%s/%s list: %v", sortCase.name, query.label, err)
			}
			count := len(rows)
			for _, row := range rows {
				if row.HouseholdID != target {
					t.Errorf("%s/%s returned household=%s want=%s", sortCase.name, query.label, row.HouseholdID, target)
				}
			}
			args := capture.args
			want := query.want
			if want > 101 {
				want = 101
			}
			if count != want {
				t.Errorf("%s/%s count=%d want=%d", sortCase.name, query.label, count, want)
			}
			if query.label == "rare" || query.label == "literal-a%b" || query.label == "miss" {
				var independent int
				if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid AND archived=false AND done=false AND search_text LIKE public.tendo_item_search_pattern($2::text) ESCAPE E'\\'`, target, query.q).Scan(&independent); err != nil {
					t.Fatal(err)
				}
				if independent != query.want || count != min(independent, 101) {
					t.Errorf("%s independent=%d result=%d want=%d", query.label, independent, count, query.want)
				}
			}
			var payload []byte
			if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+capture.query, args...).Scan(&payload); err != nil {
				t.Fatalf("%s/%s explain: %v", sortCase.name, query.label, err)
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			doc, plan := decodeSearchPlan(t, payload)
			nodes := []string{}
			collectSearchPlanNodes(plan, &nodes)
			used := strings.Contains(strings.Join(nodes, " "), "items_search_text_trgm_idx")
			t.Logf("%s | %s | %s | %t | %.3f", sortCase.name, query.label, strings.Join(nodes[:min(3, len(nodes))], ","), used, doc["Execution Time"].(float64))
		}

		tx, err := app.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		summaryCapture := &capturingDB{Tx: tx}
		counts, err := NewRepository(summaryCapture).Summary(ctx, target, item.SummaryOptions{Today: mustTestDate(2026, time.March, 2), Filters: item.CollectionFilters{Q: query.q}})
		if err != nil {
			t.Fatalf("summary/%s result: %v", query.label, err)
		}
		if counts.Total != int64(query.want) {
			t.Errorf("summary/%s count=%d want=%d", query.label, counts.Total, query.want)
		}
		var payload []byte
		if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) `+summaryCapture.query, summaryCapture.args...).Scan(&payload); err != nil {
			t.Fatalf("summary/%s explain: %v", query.label, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		doc, plan := decodeSearchPlan(t, payload)
		nodes := []string{}
		collectSearchPlanNodes(plan, &nodes)
		aggregates, subplans := 0, false
		inspectSearchPlan(plan, &aggregates, &subplans)
		if aggregates != 1 || subplans {
			t.Errorf("summary/%s aggregates=%d subplans=%v nodes=%v", query.label, aggregates, subplans, nodes)
		}
		used := strings.Contains(strings.Join(nodes, " "), "items_search_text_trgm_idx")
		t.Logf("SummarizeItems | %s | %s | %t | %.3f", query.label, strings.Join(nodes[:min(3, len(nodes))], ","), used, doc["Execution Time"].(float64))
	}
}

func decodeSearchPlan(t *testing.T, payload []byte) (map[string]any, map[string]any) {
	t.Helper()
	var docs []map[string]any
	if err := json.Unmarshal(payload, &docs); err != nil {
		t.Fatal(err)
	}
	return docs[0], docs[0]["Plan"].(map[string]any)
}

func collectSearchPlanNodes(plan map[string]any, nodes *[]string) {
	label, _ := plan["Node Type"].(string)
	if index, ok := plan["Index Name"].(string); ok {
		label += "[" + index + "]"
	}
	*nodes = append(*nodes, label)
	if children, ok := plan["Plans"].([]any); ok {
		for _, child := range children {
			collectSearchPlanNodes(child.(map[string]any), nodes)
		}
	}
}

func inspectSearchPlan(plan map[string]any, aggregates *int, subplans *bool) {
	if strings.Contains(fmt.Sprint(plan["Node Type"]), "Aggregate") {
		*aggregates++
	}
	if plan["Parent Relationship"] == "SubPlan" || plan["Parent Relationship"] == "InitPlan" {
		*subplans = true
	}
	if children, ok := plan["Plans"].([]any); ok {
		for _, child := range children {
			inspectSearchPlan(child.(map[string]any), aggregates, subplans)
		}
	}
}
