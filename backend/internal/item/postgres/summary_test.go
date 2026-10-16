package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

func mustSummaryService(t *testing.T, s *item.Service, ctx context.Context, user, h string, q item.SummaryQuery) item.Summary {
	t.Helper()
	v, err := s.Summary(ctx, user, h, q)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func mustSummary(t *testing.T, r *Repository, ctx context.Context, h string, opts item.SummaryOptions) item.SummaryCounts {
	t.Helper()
	counts, err := r.Summary(ctx, h, opts)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestSummarizeItemsPostgresAndListWalk(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var h, other, s, otherS, user, member string
	for _, q := range []struct {
		sql string
		dst *string
	}{
		{`INSERT INTO households(name,timezone) VALUES('summary target','America/New_York') RETURNING id::text`, &h},
		{`INSERT INTO households(name,timezone) VALUES('summary other','UTC') RETURNING id::text`, &other},
		{`INSERT INTO user_accounts(login) VALUES('summary actor') RETURNING id::text`, &user},
		{`INSERT INTO user_accounts(login) VALUES('summary member') RETURNING id::text`, &member},
	} {
		if err := admin.QueryRow(ctx, q.sql).Scan(q.dst); err != nil {
			t.Fatal(err)
		}
	}
	if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','summary subject') RETURNING id::text`, h).Scan(&s); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','other subject') RETURNING id::text`, other).Scan(&otherS); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner'),($1::uuid,$3::uuid,'owner'),($4::uuid,$2::uuid,'member')`, user, h, other, member); err != nil {
		t.Fatal(err)
	}
	today := mustTestDate(2026, time.March, 1)
	rows := []struct {
		subject, responsible, state, date string
		rec, arch, done                   bool
	}{
		{s, "", "open", "", false, false, false}, {s, member, "open", "2026-02-28", true, false, false}, {s, "", "open", "2026-03-01", false, false, false}, {s, "", "open", "2026-03-02", true, false, false},
		{s, "", "in_progress", "2026-03-03", false, false, false}, {s, "", "waiting", "2026-02-28", false, false, false}, {s, "", "paused", "2026-03-02", false, false, false},
		{s, "", "open", "2026-03-03", false, false, true}, {s, "", "paused", "", false, false, true}, {s, "", "open", "", false, true, false}, {s, "", "open", "", false, true, true},
	}
	for i, r := range rows {
		_, err := admin.Exec(ctx, `INSERT INTO items(household_id,subject_id,responsible_user_id,title,workflow_state,attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,archived,done) VALUES($1::uuid,$2::uuid,NULLIF($3::text,'')::uuid,$4,$5,NULLIF($6,'')::date,CASE WHEN $7 THEN 1 END,CASE WHEN $7 THEN 'day' END,CASE WHEN $7 THEN 'fixed' END,$8,$9)`, h, r.subject, r.responsible, fmt.Sprintf("matrix-%02d", i), r.state, r.date, r.rec, r.arch, r.done)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := admin.Exec(ctx, `INSERT INTO items(household_id,subject_id,title) VALUES($1::uuid,$2::uuid,'other-only')`, other, otherS)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(app)
	if _, err := repo.Summary(ctx, h, item.SummaryOptions{Filters: item.CollectionFilters{SubjectID: "invalid"}, Today: today}); !errors.Is(err, errPersistence) || errors.Is(err, item.ErrNotFound) {
		t.Fatalf("invalid validated subject filter error=%v", err)
	}
	if _, err := repo.Summary(ctx, h, item.SummaryOptions{Filters: item.CollectionFilters{ResponsibleUserID: item.Some("invalid")}, Today: today}); !errors.Is(err, errPersistence) || errors.Is(err, item.ErrNotFound) {
		t.Fatalf("invalid validated responsible filter error=%v", err)
	}
	clock := time.Date(2026, time.March, 2, 4, 30, 0, 0, time.UTC)
	houses := func(_ context.Context, uid, hid string) (string, error) {
		if uid != user || (hid != h && hid != other) {
			return "", item.ErrNotFound
		}
		if hid == h {
			return "America/New_York", nil
		}
		return "UTC", nil
	}
	svc := item.NewService(repo, houses, nil, nil, func() time.Time { return clock })
	query := func(q item.SummaryQuery) item.Summary {
		t.Helper()
		return mustSummaryService(t, svc, ctx, user, h, q)
	}
	want := item.Summary{EvaluatedOn: today, SummaryCounts: item.SummaryCounts{Total: 11, NeedsAttention: 3, InProgress: 1, Waiting: 1, Upcoming: 1, Paused: 1, Done: 2, Archived: 2}}
	if got := query(item.SummaryQuery{}); got != want {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
	for _, tc := range []struct {
		name  string
		q     item.SummaryQuery
		total int64
	}{{"subject", item.SummaryQuery{SubjectID: s}, 11}, {"responsible", item.SummaryQuery{ResponsibleUserID: member}, 1}, {"unassigned", item.SummaryQuery{ResponsibleUserID: "unassigned"}, 10}, {"foreign subject", item.SummaryQuery{SubjectID: otherS}, 0}, {"foreign member", item.SummaryQuery{ResponsibleUserID: user}, 0}, {"workflow", item.SummaryQuery{WorkflowState: "paused"}, 2}, {"attention", item.SummaryQuery{Attention: "upcoming"}, 4}, {"recurring", item.SummaryQuery{Recurring: ptrBool(true)}, 2}, {"and", item.SummaryQuery{SubjectID: s, WorkflowState: "open", Attention: "needs_attention", Recurring: ptrBool(false)}, 4}} {
		if got := query(tc.q); got.Total != tc.total {
			t.Errorf("%s got=%+v want total=%d", tc.name, got, tc.total)
		}
	}
	if got := mustSummary(t, repo, ctx, h, item.SummaryOptions{Today: today}); got.Total != 11 {
		t.Fatalf("target household count=%d", got.Total)
	}
	if got := mustSummary(t, repo, ctx, other, item.SummaryOptions{Today: today}); got.Total != 1 {
		t.Fatalf("other household count=%d", got.Total)
	}
	if got, err := svc.Summary(ctx, user, other, item.SummaryQuery{}); err != nil || got.Total != 1 {
		t.Fatalf("actor in both households summary=%+v err=%v", got, err)
	}
	if got, err := svc.Summary(ctx, user, h, item.SummaryQuery{SubjectID: otherS}); err != nil || got.Total != 0 {
		t.Fatalf("foreign subject=%+v err=%v", got, err)
	}
	// Walk the four service-level archived/done partitions through keyset cursors.
	var walked item.SummaryCounts
	seen := map[string]bool{}
	pages := 0
	for _, lifecycle := range []struct{ arch, done bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
		cursor := ""
		for {
			page, e := svc.List(ctx, user, h, item.ListQuery{Archived: lifecycle.arch, Done: lifecycle.done, Limit: 7, Cursor: cursor})
			if e != nil {
				t.Fatal(e)
			}
			pages++
			if pages > 100 {
				t.Fatal("service list walk exceeded bounded page count")
			}
			for _, row := range page.Items {
				if seen[row.ID] {
					t.Fatalf("duplicate item %s", row.ID)
				}
				seen[row.ID] = true
				switch {
				case row.Archived:
					walked.Archived++
				case row.Done:
					walked.Done++
				case row.WorkflowState == item.StatePaused:
					walked.Paused++
				case row.WorkflowState == item.StateInProgress:
					walked.InProgress++
				case row.WorkflowState == item.StateWaiting:
					walked.Waiting++
				case row.Attention == schedule.NeedsAttention:
					walked.NeedsAttention++
				case row.Attention == schedule.Upcoming:
					walked.Upcoming++
				}
				walked.Total++
			}
			if page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
	}
	if got := query(item.SummaryQuery{}).SummaryCounts; got != walked {
		t.Errorf("list-walk=%+v summary=%+v", walked, got)
	}
	if walked.Total != 11 || walked.NeedsAttention == 0 || walked.InProgress == 0 || walked.Waiting == 0 || walked.Upcoming == 0 || walked.Paused == 0 || walked.Done == 0 || walked.Archived == 0 {
		t.Fatalf("fixture must populate all partitions: %+v", walked)
	}
	var before int64
	if err = admin.QueryRow(ctx, `SELECT sum(version) FROM items WHERE household_id=$1::uuid`, h).Scan(&before); err != nil {
		t.Fatal(err)
	}
	_ = query(item.SummaryQuery{})
	var after int64
	if err = admin.QueryRow(ctx, `SELECT sum(version) FROM items WHERE household_id=$1::uuid`, h).Scan(&after); err != nil || before != after {
		t.Fatalf("summary mutated versions before=%d after=%d err=%v", before, after, err)
	}
}

func TestSummarizeItemsListWalkEquivalenceLarge(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var h, other, subjectID, otherSubject, foreignSubject, member string
	for _, entry := range []struct {
		sql string
		dst *string
	}{
		{`INSERT INTO households(name,timezone) VALUES('large walk target','UTC') RETURNING id::text`, &h},
		{`INSERT INTO households(name,timezone) VALUES('large walk other','UTC') RETURNING id::text`, &other},
		{`INSERT INTO user_accounts(login) VALUES('large walk member') RETURNING id::text`, &member},
	} {
		if err := admin.QueryRow(ctx, entry.sql).Scan(entry.dst); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range []struct {
		house string
		dst   *string
		name  string
	}{{h, &subjectID, "target subject"}, {h, &otherSubject, "second target subject"}, {other, &foreignSubject, "other subject"}} {
		if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person',$2) RETURNING id::text`, entry.house, entry.name).Scan(entry.dst); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, member, h); err != nil {
		t.Fatal(err)
	}
	today := mustTestDate(2026, time.March, 2)
	_, err := admin.Exec(ctx, `INSERT INTO items(household_id,subject_id,responsible_user_id,title,workflow_state,attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,archived,done)
SELECT $1::uuid, CASE WHEN n%2=0 THEN $2::uuid ELSE $3::uuid END, CASE WHEN n%3=0 THEN $4::uuid END, 'large-'||n,
CASE n%4 WHEN 0 THEN 'open' WHEN 1 THEN 'in_progress' WHEN 2 THEN 'waiting' ELSE 'paused' END,
CASE n%5 WHEN 0 THEN NULL WHEN 1 THEN DATE '2026-03-01' WHEN 2 THEN DATE '2026-03-02' WHEN 3 THEN DATE '2026-03-03' ELSE CASE WHEN n%4=0 THEN DATE '2026-03-03' ELSE DATE '2026-03-01' END END,
CASE WHEN n%2=0 THEN 1 END, CASE WHEN n%2=0 THEN 'day' END, CASE WHEN n%2=0 THEN 'fixed' END, n%17=0, n%19=0 FROM generate_series(1,280)n`, h, subjectID, otherSubject, member)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, `INSERT INTO items(household_id,subject_id,title) SELECT $1::uuid,$2::uuid,'foreign-'||n FROM generate_series(1,50)n`, other, foreignSubject)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(app)
	queries := []struct {
		name string
		q    item.SummaryQuery
	}{
		{"unfiltered", item.SummaryQuery{}}, {"subject", item.SummaryQuery{SubjectID: subjectID}},
		{"responsible", item.SummaryQuery{ResponsibleUserID: member}}, {"unassigned", item.SummaryQuery{ResponsibleUserID: "unassigned"}},
		{"workflow", item.SummaryQuery{WorkflowState: "waiting"}}, {"needs", item.SummaryQuery{Attention: "needs_attention"}},
		{"upcoming", item.SummaryQuery{Attention: "upcoming"}}, {"nonrecurring", item.SummaryQuery{Recurring: ptrBool(false)}},
		{"subject+needs", item.SummaryQuery{SubjectID: subjectID, Attention: "needs_attention"}},
		{"unassigned+recurring", item.SummaryQuery{ResponsibleUserID: "unassigned", Recurring: ptrBool(true)}},
	}
	for _, query := range queries {
		t.Run(query.name, func(t *testing.T) {
			filters := item.CollectionFilters{SubjectID: query.q.SubjectID, Recurring: query.q.Recurring}
			if query.q.ResponsibleUserID == "unassigned" {
				filters.ResponsibleUserID = item.Null[string]()
			} else if query.q.ResponsibleUserID != "" {
				filters.ResponsibleUserID = item.Some(query.q.ResponsibleUserID)
			}
			if query.q.WorkflowState != "" {
				filters.WorkflowState = item.WorkflowState(query.q.WorkflowState)
			}
			if query.q.Attention != "" {
				filters.Attention = schedule.Attention(query.q.Attention)
			}
			want := mustSummary(t, repo, ctx, h, item.SummaryOptions{Filters: filters, Today: today})
			var walked item.SummaryCounts
			seen := map[string]bool{}
			for _, life := range []struct{ arch, done bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
				last := "00000000-0000-0000-0000-000000000000"
				for {
					page, e := repo.List(ctx, h, item.ListOptions{Filters: item.ListFilters{Archived: life.arch, Done: life.done, SubjectID: filters.SubjectID, ResponsibleUserID: filters.ResponsibleUserID, WorkflowState: filters.WorkflowState, Attention: filters.Attention, Recurring: filters.Recurring}, Today: today, AfterID: last, Limit: 7})
					if e != nil {
						t.Fatal(e)
					}
					for _, row := range page {
						if seen[row.ID] {
							t.Fatalf("duplicate item %s", row.ID)
						}
						seen[row.ID] = true
						last = row.ID
						switch {
						case row.Archived:
							walked.Archived++
						case row.Done:
							walked.Done++
						case row.WorkflowState == item.StatePaused:
							walked.Paused++
						case row.WorkflowState == item.StateInProgress:
							walked.InProgress++
						case row.WorkflowState == item.StateWaiting:
							walked.Waiting++
						case row.AttentionOn == nil || row.AttentionOn.Compare(today) <= 0:
							walked.NeedsAttention++
						default:
							walked.Upcoming++
						}
						walked.Total++
					}
					if len(page) < 7 {
						break
					}
				}
			}
			if walked != want {
				t.Fatalf("walked=%+v summary=%+v", walked, want)
			}
		})
	}
	var targetTotal int64
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid`, h).Scan(&targetTotal); err != nil {
		t.Fatal(err)
	}
	if got := mustSummary(t, repo, ctx, h, item.SummaryOptions{Today: today}).Total; got != targetTotal {
		t.Fatalf("total=%d independent=%d", got, targetTotal)
	}
}

func TestSummaryLifecycleShifts(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var h, user, sid string
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('summary lifecycle','UTC') RETURNING id::text`).Scan(&h); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('summary lifecycle actor') RETURNING id::text`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','lifecycle') RETURNING id::text`, h).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, user, h); err != nil {
		t.Fatal(err)
	}
	today := mustTestDate(2026, time.March, 2)
	yesterday := "2026-03-01"
	houses := func(_ context.Context, uid, hid string) (string, error) {
		if uid != user || hid != h {
			return "", item.ErrNotFound
		}
		return "UTC", nil
	}
	repo := NewRepository(app)
	subjects := func(_ context.Context, uid, hid, candidate string) (bool, error) {
		if uid != user || hid != h || candidate != sid {
			return false, item.ErrNotFound
		}
		return false, nil
	}
	svc := item.NewService(repo, houses, subjects, nil, func() time.Time { return time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC) })
	assert := func(w item.SummaryCounts) {
		t.Helper()
		if got := mustSummary(t, repo, ctx, h, item.SummaryOptions{Today: today}); got != w {
			t.Fatalf("summary=%+v want=%+v", got, w)
		}
	}
	var open item.SummaryCounts
	open.Total = 1
	open.NeedsAttention = 1
	created, err := svc.Create(ctx, user, h, "lifecycle-open", item.NewItem{SubjectID: sid, Title: "one-off", AttentionOn: &yesterday})
	if err != nil {
		t.Fatal(err)
	}
	assert(open)
	receipt, _, err := svc.Complete(ctx, user, h, created.ID, created.Version, "lifecycle-complete", item.CompletionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	done := item.SummaryCounts{Total: 1, Done: 1}
	assert(done)
	_, err = svc.UndoCompletion(ctx, user, h, created.ID, receipt.ID, created.Version+1)
	if err != nil {
		t.Fatal(err)
	}
	assert(open)
	undoneItem, err := repo.Get(ctx, h, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	daily := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}
	repeating, err := svc.Create(ctx, user, h, "lifecycle-repeat", item.NewItem{SubjectID: sid, Title: "daily", AttentionOn: &yesterday, Recurrence: &daily})
	if err != nil {
		t.Fatal(err)
	}
	open.Total = 2
	open.NeedsAttention = 2
	assert(open)
	_, _, err = svc.Complete(ctx, user, h, repeating.ID, repeating.Version, "lifecycle-repeat-complete", item.CompletionRequest{})
	if err != nil {
		t.Fatal(err)
	}
	// Fixed daily recurrence advances to tomorrow, so the item remains upcoming.
	upcoming := item.SummaryCounts{Total: 2, NeedsAttention: 1, Upcoming: 1}
	assert(upcoming)
	pausedState := "paused"
	pausedItem, err := svc.Update(ctx, user, h, created.ID, undoneItem.Version, item.Patch{WorkflowState: &pausedState})
	if err != nil {
		t.Fatal(err)
	}
	paused := item.SummaryCounts{Total: 2, Paused: 1, Upcoming: 1}
	assert(paused)
	openState := "open"
	resumed, err := svc.Update(ctx, user, h, created.ID, pausedItem.Version, item.Patch{WorkflowState: &openState})
	if err != nil {
		t.Fatal(err)
	}
	assert(upcoming)
	archived := true
	archivedItem, err := svc.Update(ctx, user, h, created.ID, resumed.Version, item.Patch{Archived: &archived})
	if err != nil {
		t.Fatal(err)
	}
	archSummary := item.SummaryCounts{Total: 2, Archived: 1, Upcoming: 1}
	assert(archSummary)
	archived = false
	if _, err = svc.Update(ctx, user, h, created.ID, archivedItem.Version, item.Patch{Archived: &archived}); err != nil {
		t.Fatal(err)
	}
	assert(upcoming)
}

func inspectPlan(plan map[string]any, aggregates, itemScans *int, hasSubplan *bool) {
	if typ, _ := plan["Node Type"].(string); strings.Contains(typ, "Aggregate") {
		*aggregates++
	}
	if relation, _ := plan["Relation Name"].(string); relation == "items" {
		*itemScans++
	}
	if parent, _ := plan["Parent Relationship"].(string); parent == "SubPlan" || parent == "InitPlan" {
		*hasSubplan = true
	}
	if _, ok := plan["Subplan Name"]; ok {
		*hasSubplan = true
	}
	if children, ok := plan["Plans"].([]any); ok {
		for _, child := range children {
			inspectPlan(child.(map[string]any), aggregates, itemScans, hasSubplan)
		}
	}
}

func collectItemScanRows(plan map[string]any, rows *[]float64) {
	if relation, _ := plan["Relation Name"].(string); relation == "items" {
		if count, ok := plan["Actual Rows"].(float64); ok {
			*rows = append(*rows, count)
		}
	}
	if children, ok := plan["Plans"].([]any); ok {
		for _, child := range children {
			collectItemScanRows(child.(map[string]any), rows)
		}
	}
}

func TestSummarizeItemsExplainAtScale(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	var h, other, s, os, user, sparse, assignee string
	for _, q := range []struct {
		sql  string
		dst  *string
		args func() []any
	}{{`INSERT INTO households(name,timezone) VALUES('summary explain target','UTC') RETURNING id::text`, &h, func() []any { return nil }}, {`INSERT INTO households(name,timezone) VALUES('summary explain other','UTC') RETURNING id::text`, &other, func() []any { return nil }}, {`INSERT INTO user_accounts(login) VALUES('summary explain actor') RETURNING id::text`, &user, func() []any { return nil }}, {`INSERT INTO user_accounts(login) VALUES('summary explain assignee') RETURNING id::text`, &assignee, func() []any { return nil }}, {`INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','target') RETURNING id::text`, &s, func() []any { return []any{h} }}, {`INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','sparse') RETURNING id::text`, &sparse, func() []any { return []any{h} }}, {`INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','other') RETURNING id::text`, &os, func() []any { return []any{other} }}} {
		if err := admin.QueryRow(ctx, q.sql, q.args()...).Scan(q.dst); err != nil {
			t.Fatal(err)
		}
	}
	_, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner')`, user, h)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, assignee, h)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, `INSERT INTO items(id,household_id,subject_id,title,attention_on,responsible_user_id,workflow_state,archived,done,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) SELECT ('0198a2f0-7c1e-7a53-9b0e-'||lpad((200000+n)::text,12,'0'))::uuid,CASE WHEN n%2=0 THEN $1::uuid ELSE $2::uuid END,CASE WHEN n%100=0 AND n%2=0 THEN $5::uuid WHEN n%2=0 THEN $3::uuid ELSE $4::uuid END,'summary-explain-'||n,CASE WHEN n%7=0 THEN NULL ELSE DATE '2026-01-01'+(n%10) END,CASE WHEN n%10=0 AND n%2=0 THEN NULLIF($6::text,'')::uuid ELSE NULL END,CASE n%4 WHEN 0 THEN 'open' WHEN 1 THEN 'in_progress' WHEN 2 THEN 'waiting' ELSE 'paused' END,n%11=0,n%13=0,CASE WHEN n%5=0 THEN 1 END,CASE WHEN n%5=0 THEN 'day' END,CASE WHEN n%5=0 THEN 'fixed' END FROM generate_series(1,20000)n`, h, other, s, os, sparse, assignee)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, `ANALYZE items`)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := app.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	cases := []struct {
		name    string
		filters item.CollectionFilters
	}{{"unfiltered", item.CollectionFilters{}}, {"attention", item.CollectionFilters{Attention: schedule.Upcoming}}, {"sparse subject", item.CollectionFilters{SubjectID: sparse}}, {"sparse assignee", item.CollectionFilters{ResponsibleUserID: item.Some(assignee)}}, {"combined", item.CollectionFilters{SubjectID: sparse, ResponsibleUserID: item.Some(assignee), Recurring: ptrBool(true)}}}
	for _, tc := range cases {
		tx, e := conn.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		capture := &capturingDB{Tx: tx}
		counts, e := NewRepository(capture).Summary(ctx, h, item.SummaryOptions{Filters: tc.filters, Today: mustTestDate(2026, time.January, 5)})
		if e != nil {
			t.Fatal(e)
		}
		var payload []byte
		if e = tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)\n"+capture.query, capture.args...).Scan(&payload); e != nil {
			t.Fatal(e)
		}
		var docs []map[string]any
		if e = json.Unmarshal(payload, &docs); e != nil {
			t.Fatal(e)
		}
		plan := docs[0]["Plan"].(map[string]any)
		types := []string{}
		walkPlan(plan, &types)
		if len(types) == 0 || !strings.HasSuffix(types[0], "Aggregate") {
			t.Errorf("%s plan nodes=%v", tc.name, types)
		}
		aggregates, itemScans := 0, 0
		hasSubplan := false
		inspectPlan(plan, &aggregates, &itemScans, &hasSubplan)
		if aggregates != 1 || itemScans != 1 || hasSubplan || containsPlanType(types, "Sort") || planTempWritten(plan) != 0 {
			t.Errorf("%s plan structure aggregates=%d itemScans=%d subplan=%v nodes=%v", tc.name, aggregates, itemScans, hasSubplan, types)
		}
		independentWhere := `household_id=$1::uuid`
		independentArgs := []any{h}
		switch tc.name {
		case "attention":
			independentWhere += ` AND attention_on > $2::date`
			independentArgs = append(independentArgs, "2026-01-05")
		case "sparse subject":
			independentWhere += ` AND subject_id=$2::uuid`
			independentArgs = append(independentArgs, sparse)
		case "sparse assignee":
			independentWhere += ` AND responsible_user_id=$2::uuid`
			independentArgs = append(independentArgs, assignee)
		case "combined":
			independentWhere += ` AND subject_id=$2::uuid AND responsible_user_id=$3::uuid AND recurrence_interval_value IS NOT NULL AND recurrence_interval_unit IS NOT NULL AND recurrence_mode IS NOT NULL`
			independentArgs = append(independentArgs, sparse, assignee)
		}
		var want item.SummaryCounts
		independentSQL := `SELECT count(*), count(*) FILTER (WHERE NOT archived AND NOT done AND workflow_state='open' AND (attention_on IS NULL OR attention_on <= $` + fmt.Sprint(len(independentArgs)+1) + `::date)), count(*) FILTER (WHERE NOT archived AND NOT done AND workflow_state='in_progress'), count(*) FILTER (WHERE NOT archived AND NOT done AND workflow_state='waiting'), count(*) FILTER (WHERE NOT archived AND NOT done AND workflow_state='open' AND attention_on > $` + fmt.Sprint(len(independentArgs)+1) + `::date), count(*) FILTER (WHERE NOT archived AND NOT done AND workflow_state='paused'), count(*) FILTER (WHERE done AND NOT archived), count(*) FILTER (WHERE archived) FROM items WHERE ` + independentWhere
		independentArgs = append(independentArgs, "2026-01-05")
		if e = admin.QueryRow(ctx, independentSQL, independentArgs...).Scan(&want.Total, &want.NeedsAttention, &want.InProgress, &want.Waiting, &want.Upcoming, &want.Paused, &want.Done, &want.Archived); e != nil {
			t.Fatal(e)
		}
		if counts != want {
			t.Errorf("%s summary=%+v independent=%+v", tc.name, counts, want)
		}
		rootType, _ := plan["Node Type"].(string)
		rootHit, _ := plan["Shared Hit Blocks"].(float64)
		rootRead, _ := plan["Shared Read Blocks"].(float64)
		var scanRows []float64
		collectItemScanRows(plan, &scanRows)
		executionMS, _ := docs[0]["Execution Time"].(float64)
		t.Logf("EXPLAIN %s root=%s scans=%v root_shared_hit=%.0f root_shared_read=%.0f item_scan_actual_rows=%v execution_ms=%.3f", tc.name, rootType, types, rootHit, rootRead, scanRows, executionMS)
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
}

func walkPlan(plan map[string]any, out *[]string) {
	typ, _ := plan["Node Type"].(string)
	*out = append(*out, typ)
	if children, ok := plan["Plans"].([]any); ok {
		for _, child := range children {
			walkPlan(child.(map[string]any), out)
		}
	}
}
func containsPlanType(nodes []string, want string) bool {
	for _, v := range nodes {
		if strings.Contains(v, want) {
			return true
		}
	}
	return false
}
