package postgres

import (
	"strings"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestItemTextSearchPostgres(t *testing.T) {
	f := newAttentionFixture(t)
	ids := make([]string, 0, 12)
	add := func(sub, title, notes, state, attention string, archived, done, recurring bool, responsible *string) string {
		id := f.add(t, sub, attention, archived, done, recurring, state, responsible)
		f.setText(t, id, title, notes)
		ids = append(ids, id)
		return id
	}
	czech := add(f.subject, "Žluťoučký kůň", "Straße and literal %_\\, text", "open", "", false, false, false, nil)
	notesOnly := add(f.subject, "Title boundary", "notes contain exclusive phrase", "open", "", false, false, false, nil)
	nullNotes := f.add(t, f.subject, "", false, false, false, "open", nil)
	f.setText(t, nullNotes, "null notes target", "")
	ids = append(ids, nullNotes)
	add(f.subject, "cross-field alpha", "omega split", "open", "", false, false, false, nil)
	wild := add(f.subject, "a%b a_b a\\b", "literal comma,", "open", "", false, false, false, nil)
	decoy := add(f.subject, "axxb axb aXb", "not literal", "open", "", false, false, false, nil)
	sameHouseOtherSubject := add(f.otherSubject, "Straße elsewhere", "family 家族", "open", "", false, false, false, nil)
	member := f.assignee
	task := add(f.subject, "Straße task", "other", "waiting", "2026-03-03", false, false, true, &member)
	archived := add(f.subject, "Straße archived", "other", "open", "", true, false, false, nil)
	done := add(f.subject, "Straße done", "other", "open", "", false, true, false, nil)
	both := add(f.subject, "Straße archived done", "other", "open", "", true, true, false, nil)

	var foreignHouse, foreignSubject string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO households(name,timezone) VALUES('search foreign','UTC') RETURNING id::text`).Scan(&foreignHouse); err != nil {
		t.Fatal(err)
	}
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','foreign') RETURNING id::text`, foreignHouse).Scan(&foreignSubject); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO items(household_id,subject_id,title,notes) VALUES($1::uuid,$2::uuid,'Straße foreign','family')`, foreignHouse, foreignSubject); err != nil {
		t.Fatal(err)
	}
	foreignPage, err := f.svc.List(f.ctx, f.actor, f.house, item.ListQuery{Q: "Straße foreign", Limit: 100})
	if err != nil || len(foreignPage.Items) != 0 {
		t.Fatalf("foreign household search leaked: %+v err=%v", foreignPage, err)
	}
	foreignSummary, err := f.svc.Summary(f.ctx, f.actor, f.house, item.SummaryQuery{Q: "Straße foreign"})
	if err != nil || foreignSummary.Total != 0 {
		t.Fatalf("foreign household summary leaked: %+v err=%v", foreignSummary, err)
	}

	for _, tc := range []struct {
		q    string
		want []string
	}{
		{"zlutoucky", []string{czech}}, {"z\u030Clutoucky", []string{czech}}, {"ŽLUŤOUČKÝ", []string{czech}},
		{"STRASSE", []string{czech, sameHouseOtherSubject, task}}, {"notes contain", []string{notesOnly}}, {"null notes", []string{nullNotes}},
		{"alpha omega", nil}, {"alphaomega", nil}, {"%", []string{czech, wild}}, {"_", []string{czech, wild}}, {"\\", []string{czech, wild}},
		{"a%b", []string{wild}}, {"a_b", []string{wild}}, {"a\\b", []string{wild}}, {"家族", []string{sameHouseOtherSubject}}, {"\u0301", nil},
	} {
		page, e := f.svc.List(f.ctx, f.actor, f.house, item.ListQuery{Q: tc.q, Limit: 100})
		if e != nil {
			t.Fatalf("q=%q: %v", tc.q, e)
		}
		got := itemIDs(page.Items)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("q=%q got=%v want=%v decoy=%s", tc.q, got, tc.want, decoy)
		}
	}
	// Classification is cross-checked against default-list walks for every archived/done combination.
	for _, q := range []string{"Straße", "no-match-search"} {
		var want item.SummaryCounts
		for _, combo := range []struct{ archived, done bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
			query := item.ListQuery{Q: q, Archived: combo.archived, Done: combo.done, Limit: 100}
			for iteration := 0; ; iteration++ {
				if iteration >= 1000 {
					t.Fatal("summary comparison cursor walk exceeded 1000 pages")
				}
				page, e := f.svc.List(f.ctx, f.actor, f.house, query)
				if e != nil {
					t.Fatal(e)
				}
				for _, it := range page.Items {
					want.Total++
					if it.Archived {
						want.Archived++
					} else if it.Done {
						want.Done++
					} else {
						switch it.WorkflowState {
						case item.StatePaused:
							want.Paused++
						case item.StateInProgress:
							want.InProgress++
						case item.StateWaiting:
							want.Waiting++
						default:
							if it.AttentionOn == nil || it.AttentionOn.String() <= "2026-03-02" {
								want.NeedsAttention++
							} else {
								want.Upcoming++
							}
						}
					}
				}
				if page.NextCursor == nil {
					break
				}
				query.Cursor = *page.NextCursor
			}
		}
		got, e := f.svc.Summary(f.ctx, f.actor, f.house, item.SummaryQuery{Q: q})
		if e != nil {
			t.Fatal(e)
		}
		if got.SummaryCounts != want {
			t.Errorf("summary q=%q got=%+v want=%+v", q, got.SummaryCounts, want)
		}
		combinedWalk := item.ListQuery{Q: q, SubjectID: f.subject, WorkflowState: "waiting", Recurring: boolPtr(true), Limit: 100}
		var combinedWant item.SummaryCounts
		for iteration := 0; ; iteration++ {
			if iteration >= 1000 {
				t.Fatal("combined summary cursor walk exceeded 1000 pages")
			}
			page, e := f.svc.List(f.ctx, f.actor, f.house, combinedWalk)
			if e != nil {
				t.Fatal(e)
			}
			for _, it := range page.Items {
				combinedWant.Total++
				if it.Archived {
					combinedWant.Archived++
				} else if it.Done {
					combinedWant.Done++
				} else {
					switch it.WorkflowState {
					case item.StatePaused:
						combinedWant.Paused++
					case item.StateInProgress:
						combinedWant.InProgress++
					case item.StateWaiting:
						combinedWant.Waiting++
					default:
						if it.AttentionOn == nil || it.AttentionOn.String() <= "2026-03-02" {
							combinedWant.NeedsAttention++
						} else {
							combinedWant.Upcoming++
						}
					}
				}
			}
			if page.NextCursor == nil {
				break
			}
			combinedWalk.Cursor = *page.NextCursor
		}
		combinedGot, e := f.svc.Summary(f.ctx, f.actor, f.house, item.SummaryQuery{Q: q, SubjectID: f.subject, WorkflowState: "waiting", Recurring: boolPtr(true)})
		if e != nil || combinedGot.SummaryCounts != combinedWant {
			t.Errorf("combined summary q=%q got=%+v want=%+v err=%v", q, combinedGot.SummaryCounts, combinedWant, e)
		}
	}
	if archived == "" || done == "" || both == "" {
		t.Fatal("lifecycle fixtures must have IDs")
	}

	draft, err := f.svc.Create(f.ctx, f.actor, f.house, "search-runtime-create", item.NewItem{SubjectID: f.subject, Title: "first searchable phrase"})
	if err != nil {
		t.Fatal(err)
	}
	assertSearchIDs(t, f, "first searchable", []string{draft.ID})
	title := "second searchable phrase"
	updated, err := f.svc.Update(f.ctx, f.actor, f.house, draft.ID, draft.Version, item.Patch{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	assertSearchIDs(t, f, "first searchable", nil)
	assertSearchIDs(t, f, "second searchable", []string{draft.ID})
	note := "third searchable phrase"
	updated, err = f.svc.Update(f.ctx, f.actor, f.house, draft.ID, updated.Version, item.Patch{Notes: item.Some(note)})
	if err != nil {
		t.Fatal(err)
	}
	assertSearchIDs(t, f, "third searchable", []string{draft.ID})
	updated, err = f.svc.Update(f.ctx, f.actor, f.house, draft.ID, updated.Version, item.Patch{Notes: item.Null[string]()})
	if err != nil {
		t.Fatal(err)
	}
	assertSearchIDs(t, f, "third searchable", nil)
	created, err := f.svc.Create(f.ctx, f.actor, f.house, "search-complete", item.NewItem{SubjectID: f.subject, Title: "completion searchable", Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}})
	if err != nil {
		t.Fatal(err)
	}
	receipt, _, err := f.svc.Complete(f.ctx, f.actor, f.house, created.ID, created.Version, "search-complete-key", item.CompletionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	assertSearchIDs(t, f, "completion searchable", []string{created.ID})
	if _, err = f.svc.UndoCompletion(f.ctx, f.actor, f.house, created.ID, receipt.ID, created.Version+1); err != nil {
		t.Fatal(err)
	}
	assertSearchIDs(t, f, "completion searchable", []string{created.ID})
	first, err := f.svc.List(f.ctx, f.actor, f.house, item.ListQuery{Q: "Straße", Limit: 1})
	if err != nil || first.NextCursor == nil {
		t.Fatalf("cursor first page=%+v err=%v", first, err)
	}
	if _, err = f.svc.List(f.ctx, f.actor, f.house, item.ListQuery{Q: "family", Cursor: *first.NextCursor}); err == nil {
		t.Fatal("changed q cursor accepted")
	}
	for _, sortBy := range []item.ListSort{item.SortCreated, item.SortAttention} {
		for _, limit := range []int{1, 2, 7} {
			q := item.ListQuery{Q: "Straße", Sort: sortBy, Limit: limit}
			var walked []string
			for iteration := 0; ; iteration++ {
				if iteration >= 1000 {
					t.Fatal("search cursor walk exceeded 1000 pages")
				}
				p, e := f.svc.List(f.ctx, f.actor, f.house, q)
				if e != nil {
					t.Fatal(e)
				}
				walked = append(walked, itemIDs(p.Items)...)
				if p.NextCursor == nil {
					break
				}
				q.Cursor = *p.NextCursor
			}
			want := []string{czech, sameHouseOtherSubject, task}
			if strings.Join(walked, ",") != strings.Join(want, ",") {
				t.Errorf("sort=%s limit=%d search walk=%v want=%v", sortBy, limit, walked, want)
			}
		}
	}
}

func assertSearchIDs(t *testing.T, f *attentionFixture, q string, want []string) {
	t.Helper()
	page, err := f.svc.List(f.ctx, f.actor, f.house, item.ListQuery{Q: q, Limit: 100})
	if err != nil || strings.Join(itemIDs(page.Items), ",") != strings.Join(want, ",") {
		t.Fatalf("q=%q got=%v want=%v err=%v", q, itemIDs(page.Items), want, err)
	}
}

func TestSearchCursorFilteredWalks(t *testing.T) {
	f := newAttentionFixture(t)
	member := f.assignee
	ids := map[string]string{}
	add := func(name, sub, title, state, date string, rec, archived, done bool, responsible *string) {
		id := f.add(t, sub, date, archived, done, rec, state, responsible)
		f.setText(t, id, title, "")
		ids[name] = id
	}
	add("sub-match", f.subject, "search subject match", "open", "", false, false, false, nil)
	add("subject-excluded", f.otherSubject, "search subject excluded", "open", "", false, false, false, nil)
	add("assigned", f.subject, "search assigned", "waiting", "2026-03-03", true, false, false, &member)
	add("unassigned", f.subject, "search unassigned", "open", "", false, false, false, nil)
	add("workflow-match", f.subject, "search workflow", "waiting", "", false, false, false, nil)
	add("workflow-excluded", f.subject, "search other workflow", "paused", "", false, false, false, nil)
	add("needs", f.subject, "search needs", "open", "", false, false, false, nil)
	add("upcoming", f.subject, "search upcoming", "open", "2026-03-04", false, false, false, nil)
	add("recurring", f.subject, "search recurring", "open", "", true, false, false, nil)
	add("oneoff", f.subject, "search oneoff", "open", "", false, false, false, nil)
	add("nonmatch", f.subject, "other phrase", "waiting", "", true, false, false, &member)
	cases := []struct {
		name               string
		query              item.ListQuery
		created, attention []string
	}{
		{"subject", item.ListQuery{SubjectID: f.subject}, []string{ids["sub-match"], ids["assigned"], ids["unassigned"], ids["workflow-match"], ids["workflow-excluded"], ids["needs"], ids["upcoming"], ids["recurring"], ids["oneoff"]}, []string{ids["sub-match"], ids["unassigned"], ids["workflow-match"], ids["workflow-excluded"], ids["needs"], ids["recurring"], ids["oneoff"], ids["assigned"], ids["upcoming"]}},
		{"responsible", item.ListQuery{ResponsibleUserID: f.assignee}, []string{ids["assigned"]}, []string{ids["assigned"]}},
		{"unassigned", item.ListQuery{ResponsibleUserID: "unassigned"}, []string{ids["sub-match"], ids["subject-excluded"], ids["unassigned"], ids["workflow-match"], ids["workflow-excluded"], ids["needs"], ids["upcoming"], ids["recurring"], ids["oneoff"]}, []string{ids["sub-match"], ids["subject-excluded"], ids["unassigned"], ids["workflow-match"], ids["workflow-excluded"], ids["needs"], ids["recurring"], ids["oneoff"], ids["upcoming"]}},
		{"workflow waiting", item.ListQuery{WorkflowState: "waiting"}, []string{ids["assigned"], ids["workflow-match"]}, []string{ids["workflow-match"], ids["assigned"]}},
		{"needs attention", item.ListQuery{Attention: "needs_attention"}, []string{ids["sub-match"], ids["subject-excluded"], ids["unassigned"], ids["workflow-match"], ids["workflow-excluded"], ids["needs"], ids["recurring"], ids["oneoff"]}, []string{ids["sub-match"], ids["subject-excluded"], ids["unassigned"], ids["workflow-match"], ids["workflow-excluded"], ids["needs"], ids["recurring"], ids["oneoff"]}},
		{"upcoming", item.ListQuery{Attention: "upcoming"}, []string{ids["assigned"], ids["upcoming"]}, []string{ids["assigned"], ids["upcoming"]}},
		{"recurring true", item.ListQuery{Recurring: boolPtr(true)}, []string{ids["assigned"], ids["recurring"]}, []string{ids["recurring"], ids["assigned"]}},
		{"recurring false", item.ListQuery{Recurring: boolPtr(false)}, []string{ids["sub-match"], ids["subject-excluded"], ids["unassigned"], ids["workflow-match"], ids["workflow-excluded"], ids["needs"], ids["upcoming"], ids["oneoff"]}, []string{ids["sub-match"], ids["subject-excluded"], ids["unassigned"], ids["workflow-match"], ids["workflow-excluded"], ids["needs"], ids["oneoff"], ids["upcoming"]}},
		{"combined", item.ListQuery{SubjectID: f.subject, WorkflowState: "waiting", Recurring: boolPtr(true), ResponsibleUserID: f.assignee}, []string{ids["assigned"]}, []string{ids["assigned"]}},
	}
	for _, tc := range cases {
		tc.query.Q = "search"
		for _, sortBy := range []item.ListSort{item.SortCreated, item.SortAttention} {
			want := tc.created
			if sortBy == item.SortAttention {
				want = tc.attention
			}
			for _, limit := range []int{1, 2, 7} {
				q := tc.query
				q.Sort = sortBy
				q.Limit = limit
				var got []string
				for iteration := 0; ; iteration++ {
					if iteration >= 1000 {
						t.Fatal("structured cursor walk exceeded 1000 pages")
					}
					p, e := f.svc.List(f.ctx, f.actor, f.house, q)
					if e != nil {
						t.Fatal(e)
					}
					got = append(got, itemIDs(p.Items)...)
					if p.NextCursor == nil {
						break
					}
					q.Cursor = *p.NextCursor
				}
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Errorf("filter=%s sort=%s limit=%d got=%v want=%v", tc.name, sortBy, limit, got, want)
				}
			}
		}
	}
}
func boolPtr(v bool) *bool { return &v }

func TestItemSearchRuntimePrivileges(t *testing.T) {
	f := newAttentionFixture(t)
	var id string
	if err := f.admin.QueryRow(f.ctx, `SELECT uuidv7()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	_, err := f.app.Exec(f.ctx, `INSERT INTO items(id,household_id,subject_id,title,search_text) VALUES($1::uuid,$2::uuid,$3::uuid,'trusted title','forged search')`, id, f.house, f.subject)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	if err = f.admin.QueryRow(f.ctx, `SELECT search_text FROM items WHERE id=$1::uuid`, id).Scan(&text); err != nil || text != "trusted title\n" {
		t.Fatalf("forged text not overwritten: %q err=%v", text, err)
	}
	_, err = f.app.Exec(f.ctx, `UPDATE items SET search_text='forged' WHERE id=$1::uuid`, id)
	if err == nil {
		t.Fatal("direct search_text update allowed")
	}
	if pg, ok := err.(*pgconn.PgError); !ok || pg.Code != "42501" {
		t.Fatalf("direct update error=%T %v", err, err)
	}
	for _, statement := range []struct {
		sql  string
		args []any
	}{{`DELETE FROM items WHERE id=$1::uuid`, []any{id}}, {`TRUNCATE items`, nil}} {
		_, err = f.app.Exec(f.ctx, statement.sql, statement.args...)
		if err == nil {
			t.Fatalf("runtime role unexpectedly executed %s", statement.sql)
		}
		if pg, ok := err.(*pgconn.PgError); !ok || pg.Code != "42501" {
			t.Fatalf("%s error=%T %v", statement.sql, err, err)
		}
	}
}
