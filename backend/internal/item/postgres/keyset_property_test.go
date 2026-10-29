package postgres

import (
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/item"
)

func TestFilteredAttentionKeysetWalkPostgres(t *testing.T) {
	f := newAttentionFixture(t)
	const count = 1007
	_, err := f.admin.Exec(f.ctx, `INSERT INTO items(id,household_id,subject_id,title,attention_on,workflow_state,recurrence_interval_value,recurrence_interval_unit,recurrence_mode)
		SELECT ('0198a3f3-0000-7000-8000-'||lpad(n::text,12,'0'))::uuid,$1::uuid,
		       CASE WHEN n%2=0 THEN $2::uuid ELSE $3::uuid END,'attention-keyset-'||n::text,
		       CASE WHEN n%9=0 THEN NULL ELSE DATE '2026-01-01'+(n%5) END,
		       CASE WHEN n%3=0 THEN 'open' ELSE 'paused' END,
		       CASE WHEN n%6=0 THEN 1 END,CASE WHEN n%6=0 THEN 'day' END,CASE WHEN n%6=0 THEN 'fixed' END
		FROM generate_series(1,$4)n`, f.house, f.subject, f.otherSubject, count)
	if err != nil {
		t.Fatal(err)
	}
	wantRows, err := f.admin.Query(f.ctx, `SELECT id::text FROM items WHERE household_id=$1::uuid AND subject_id=$2::uuid AND workflow_state='open'
		AND recurrence_mode IS NOT NULL ORDER BY attention_on ASC NULLS FIRST,id ASC`, f.house, f.subject)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for wantRows.Next() {
		var id string
		if err := wantRows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	wantRows.Close()
	if err := wantRows.Err(); err != nil {
		t.Fatal(err)
	}
	// Matching rows are multiples of six (even subject, open, recurring): floor(1007/6) = 167.
	if len(want) != 167 {
		t.Fatalf("filter fixture matched %d rows, want 167", len(want))
	}
	for _, limit := range []int{100, 7} {
		q := item.ListQuery{SubjectID: f.subject, WorkflowState: "open", Recurring: boolPtr(true), Sort: item.SortAttention, Limit: limit}
		var got []string
		for pages := 0; pages < 200; pages++ {
			page, err := f.svc.List(f.ctx, f.actor, f.house, q)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, itemIDs(page.Items)...)
			if page.NextCursor == nil {
				break
			}
			q.Cursor = *page.NextCursor
			if pages == 199 {
				t.Fatal("filtered attention cursor walk did not terminate")
			}
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("limit=%d got=%d rows, want=%d", limit, len(got), len(want))
		}
	}
}
