package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

var attentionFixtureSeq atomic.Uint64

func nextAttentionID() string {
	return fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", attentionFixtureSeq.Add(1))
}

type attentionFixture struct {
	ctx                                                                 context.Context
	admin                                                               *pgxpool.Pool
	app                                                                 *pgxpool.Pool
	repo                                                                *Repository
	svc                                                                 *item.Service
	house, otherHouse, actor, outsider, subject, otherSubject, assignee string
	seq                                                                 uint64
}

func newAttentionFixture(t *testing.T) *attentionFixture {
	t.Helper()
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := &attentionFixture{ctx: ctx, admin: admin, app: app, repo: NewRepository(app)}
	id := attentionFixtureSeq.Add(1)
	for n, dst := range []*string{&f.house, &f.otherHouse} {
		if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES($1,'UTC') RETURNING id::text`, fmt.Sprintf("attention-p3-%d-%d", id, n)).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	for _, u := range []struct {
		login string
		dst   *string
	}{{"actor", &f.actor}, {"outsider", &f.outsider}, {"assignee", &f.assignee}} {
		if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`, fmt.Sprintf("attention-p3-%d-%s", id, u.login)).Scan(u.dst); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'owner'),($3::uuid,$2::uuid,'member'),($1::uuid,$4::uuid,'member'),($5::uuid,$4::uuid,'member')`, f.actor, f.house, f.assignee, f.otherHouse, f.outsider); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		h    string
		dst  *string
		name string
	}{{f.house, &f.subject, "active"}, {f.house, &f.otherSubject, "other"}, {f.otherHouse, new(string), "foreign"}} {
		if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person',$2) RETURNING id::text`, v.h, fmt.Sprintf("attention-p3-%d-%s", id, v.name)).Scan(v.dst); err != nil {
			t.Fatal(err)
		}
	}
	houses := func(_ context.Context, u, h string) (string, error) {
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, u, h).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return "", item.ErrNotFound
		}
		return "UTC", nil
	}
	members := func(_ context.Context, u, h string) error {
		var n int
		err := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, u, h).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			return item.ErrNotFound
		}
		return nil
	}
	subjects := func(_ context.Context, _user, h, sid string) (bool, error) {
		var archived bool
		if err := admin.QueryRow(ctx, `SELECT archived FROM subjects WHERE household_id=$1::uuid AND id=$2::uuid`, h, sid).Scan(&archived); err != nil {
			return false, item.ErrNotFound
		}
		return archived, nil
	}
	f.svc = item.NewService(f.repo, houses, subjects, members, func() time.Time { return time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC) })
	return f
}
func (f *attentionFixture) add(t *testing.T, sub, attention string, archived, done, recurring bool, state string, responsible *string) string {
	t.Helper()
	id := nextAttentionID()
	_, err := f.admin.Exec(f.ctx, `INSERT INTO items(id,household_id,subject_id,responsible_user_id,title,attention_on,workflow_state,archived,done,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'p3-'||$1,NULLIF($5,'')::date,$6,$7,$8,CASE WHEN $9 THEN 1 END,CASE WHEN $9 THEN 'day' END,CASE WHEN $9 THEN 'fixed' END)`, id, f.house, sub, responsible, attention, state, archived, done, recurring)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *attentionFixture) setText(t *testing.T, id, title, notes string) {
	t.Helper()
	if _, err := f.admin.Exec(f.ctx, `UPDATE items SET title=$2,notes=NULLIF($3,'') WHERE id=$1::uuid`, id, title, notes); err != nil {
		t.Fatal(err)
	}
}
func (f *attentionFixture) list(t *testing.T, q item.ListQuery) item.Page {
	t.Helper()
	p, err := f.svc.List(f.ctx, f.actor, f.house, q)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func exactIDs(t *testing.T, got []item.Item, want []string) {
	t.Helper()
	ids := itemIDs(got)
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("IDs got=%v want=%v", ids, want)
	}
}
func attentionWalk(t *testing.T, f *attentionFixture, limit int) []string {
	t.Helper()
	var ids []string
	q := item.ListQuery{Sort: item.SortAttention, Limit: limit}
	for pages := 0; pages < 30; pages++ {
		p := f.list(t, q)
		if p.NextCursor != nil && len(p.Items) != q.Limit {
			t.Fatalf("short nonfinal page: %+v", p)
		}
		ids = append(ids, itemIDs(p.Items)...)
		if p.NextCursor == nil {
			return ids
		}
		q.Cursor = *p.NextCursor
	}
	t.Fatal("cursor walk exceeded page bound")
	return nil
}
func dateString(d string) *string {
	if d == "" {
		return nil
	}
	return &d
}

func TestAttentionSortNullAndDateTiesAcrossPages(t *testing.T) {
	f := newAttentionFixture(t)
	type keyed struct{ date, id string }
	var rows []keyed
	// Interleave insertion so ID order differs from (null-first, date, id) order; five nulls and five equal dates span page boundaries.
	for _, d := range []string{"2026-03-03", "", "2026-02-01", "2026-03-01", "", "2026-02-01", "2026-02-02", "", "2026-02-01", "", "2026-02-01", "", "2026-02-01"} {
		rows = append(rows, keyed{d, f.add(t, f.subject, d, false, false, false, "open", nil)})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.date == "") != (b.date == "") {
			return a.date == ""
		}
		if a.date != b.date {
			return a.date < b.date
		}
		return a.id < b.id
	})
	var want []string
	for _, r := range rows {
		want = append(want, r.id)
	}
	var byID []string
	byID = append(byID, want...)
	sort.Strings(byID)
	if strings.Join(byID, ",") == strings.Join(want, ",") {
		t.Fatal("fixture does not distinguish attention order from id order")
	}
	p := f.list(t, item.ListQuery{Sort: item.SortAttention, Limit: 2})
	if p.NextCursor == nil {
		t.Fatal("expected continuation")
	}
	got := attentionWalk(t, f, 2)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("walk got=%v want=%v", got, want)
	}
}
func TestAttentionSortAllowsLimitChanges(t *testing.T) {
	f := newAttentionFixture(t)
	var want []string
	for _, d := range []string{"", "", "", "2026-01-01", "2026-01-01", "2026-02-01", "2026-03-03"} {
		want = append(want, f.add(t, f.subject, d, false, false, false, "open", nil))
	}
	var got []string
	q := item.ListQuery{Sort: item.SortAttention, Limit: 2}
	limits := []int{2, 3, 1}
	for n := 0; n < 12; n++ {
		p := f.list(t, q)
		if p.NextCursor != nil && len(p.Items) != q.Limit {
			t.Fatalf("short page %d: %v", n, itemIDs(p.Items))
		}
		got = append(got, itemIDs(p.Items)...)
		if p.NextCursor == nil {
			break
		}
		q.Cursor = *p.NextCursor
		q.Limit = limits[n%len(limits)]
	}
	sort.Strings(want)
	exactIDStrings(t, got, want)
}
func TestAttentionSortAppliesAllFiltersBeforeLimit(t *testing.T) {
	f := newAttentionFixture(t)
	assigned := f.assignee
	var matched, nonmatched []string
	for n := 0; n < 35; n++ {
		sub, state, day, rec, res := f.otherSubject, "paused", "2026-03-03", false, (*string)(nil)
		if n%5 == 0 {
			sub, state, day, rec, res = f.subject, "open", "2026-03-01", true, &assigned
		}
		id := f.add(t, sub, day, false, false, rec, state, res)
		if n%5 == 0 {
			matched = append(matched, id)
		} else {
			nonmatched = append(nonmatched, id)
		}
	}
	cases := []struct {
		name string
		q    item.ListQuery
		want []string
	}{{"subject", item.ListQuery{SubjectID: f.subject}, matched}, {"responsible", item.ListQuery{ResponsibleUserID: assigned}, matched}, {"unassigned", item.ListQuery{ResponsibleUserID: "unassigned"}, nonmatched}, {"workflow", item.ListQuery{WorkflowState: "open"}, matched}, {"recurring true", item.ListQuery{Recurring: ptrBool(true)}, matched}, {"recurring false", item.ListQuery{Recurring: ptrBool(false)}, nonmatched}, {"attention needs", item.ListQuery{Attention: "needs_attention"}, matched}, {"attention upcoming", item.ListQuery{Attention: "upcoming"}, nonmatched}, {"AND", item.ListQuery{SubjectID: f.subject, ResponsibleUserID: assigned, WorkflowState: "open", Recurring: ptrBool(true), Attention: "needs_attention"}, matched}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			q := tc.q
			q.Sort = item.SortAttention
			q.Limit = 2
			var got []string
			for pages := 0; pages < 30; pages++ {
				p := f.list(t, q)
				if p.NextCursor != nil && len(p.Items) != q.Limit {
					t.Fatalf("short page=%+v", p)
				}
				got = append(got, itemIDs(p.Items)...)
				if p.NextCursor == nil {
					exactIDStrings(t, got, want)
					return
				}
				q.Cursor = *p.NextCursor
			}
			t.Fatal("filter walk exceeded page bound")
		})
	}
}

func TestAttentionSortArchivedDonePartitions(t *testing.T) {
	f := newAttentionFixture(t)
	sets := map[[2]bool][]string{}
	for a := 0; a < 2; a++ {
		for d := 0; d < 2; d++ {
			key := [2]bool{a == 1, d == 1}
			for _, day := range []string{"", "2026-02-01"} {
				sets[key] = append(sets[key], f.add(t, f.subject, day, key[0], key[1], false, "open", nil))
			}
		}
	}
	for k, w := range sets {
		q := item.ListQuery{Sort: item.SortAttention, Archived: k[0], Done: k[1], Limit: 2}
		p := f.list(t, q)
		got := itemIDs(p.Items)
		for p.NextCursor != nil {
			q.Cursor = *p.NextCursor
			p = f.list(t, q)
			got = append(got, itemIDs(p.Items)...)
		}
		sort.Strings(w)
		exactIDStrings(t, got, w)
	}
}
func TestAttentionSortNeedsAndUpcomingBoundaries(t *testing.T) {
	f := newAttentionFixture(t)
	null := f.add(t, f.subject, "", false, false, false, "open", nil)
	y := f.add(t, f.subject, "2026-03-01", false, false, false, "open", nil)
	today := f.add(t, f.subject, "2026-03-02", false, false, false, "open", nil)
	tomorrow := f.add(t, f.subject, "2026-03-03", false, false, false, "open", nil)
	p := f.list(t, item.ListQuery{Sort: item.SortAttention, Attention: "needs_attention", Limit: 10})
	exactIDs(t, p.Items, []string{null, y, today})
	p = f.list(t, item.ListQuery{Sort: item.SortAttention, Attention: "upcoming", Limit: 10})
	exactIDs(t, p.Items, []string{tomorrow})
}
func TestAttentionCursorUsesEncodedBoundaryAfterEdit(t *testing.T) {
	f := newAttentionFixture(t)
	a := f.add(t, f.subject, "2026-01-01", false, false, false, "open", nil)
	b := f.add(t, f.subject, "2026-02-01", false, false, false, "open", nil)
	c := f.add(t, f.subject, "2026-03-01", false, false, false, "open", nil)
	d := f.add(t, f.subject, "2026-04-01", false, false, false, "open", nil)
	first := f.list(t, item.ListQuery{Sort: item.SortAttention, Limit: 2})
	exactIDs(t, first.Items, []string{a, b})
	if _, err := f.admin.Exec(f.ctx, `UPDATE items SET attention_on='2027-01-01' WHERE id=$1::uuid`, b); err != nil {
		t.Fatal(err)
	}
	next := f.list(t, item.ListQuery{Sort: item.SortAttention, Limit: 5, Cursor: *first.NextCursor})
	exactIDs(t, next.Items, []string{c, d, b})
}
func TestAttentionCursorNullBoundarySurvivesEdit(t *testing.T) {
	f := newAttentionFixture(t)
	a := f.add(t, f.subject, "", false, false, false, "open", nil)
	b := f.add(t, f.subject, "", false, false, false, "open", nil)
	c := f.add(t, f.subject, "2026-01-01", false, false, false, "open", nil)
	d := f.add(t, f.subject, "2026-02-01", false, false, false, "open", nil)
	first := f.list(t, item.ListQuery{Sort: item.SortAttention, Limit: 2})
	exactIDs(t, first.Items, []string{a, b})
	if _, err := f.admin.Exec(f.ctx, `UPDATE items SET attention_on='2027-01-01' WHERE id=$1::uuid`, b); err != nil {
		t.Fatal(err)
	}
	next := f.list(t, item.ListQuery{Sort: item.SortAttention, Limit: 5, Cursor: *first.NextCursor})
	exactIDs(t, next.Items, []string{c, d, b})
}
func TestAttentionTraversalWithConcurrentNonKeyAndKeyEdits(t *testing.T) {
	f := newAttentionFixture(t)
	a := f.add(t, f.subject, "2026-01-01", false, false, false, "open", nil)
	b := f.add(t, f.subject, "2026-02-01", false, false, false, "open", nil)
	c := f.add(t, f.subject, "2026-03-01", false, false, false, "open", nil)
	d := f.add(t, f.subject, "2026-04-01", false, false, false, "open", nil)
	first := f.list(t, item.ListQuery{Sort: item.SortAttention, Limit: 2})
	exactIDs(t, first.Items, []string{a, b})
	if _, err := f.admin.Exec(f.ctx, `UPDATE items SET title='title edit' WHERE id=$1::uuid`, a); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `UPDATE items SET attention_on='2027-01-01' WHERE id=$1::uuid`, d); err != nil {
		t.Fatal(err)
	}
	next := f.list(t, item.ListQuery{Sort: item.SortAttention, Limit: 5, Cursor: *first.NextCursor})
	exactIDs(t, next.Items, []string{c, d})
}
func TestAttentionSortHouseholdIsolation(t *testing.T) {
	f := newAttentionFixture(t)
	ownNull := f.add(t, f.subject, "", false, false, false, "open", nil)
	ownDate := f.add(t, f.subject, "2026-02-01", false, false, false, "open", nil)
	foreignSub := *new(string)
	if err := f.admin.QueryRow(f.ctx, `SELECT id::text FROM subjects WHERE household_id=$1::uuid AND name LIKE 'attention-p3-%-foreign' ORDER BY created_at DESC LIMIT 1`, f.otherHouse).Scan(&foreignSub); err != nil {
		t.Fatal(err)
	}
	foreignNull := fmt.Sprintf("0198a2f0-7c1e-7a53-9b0e-%012d", attentionFixtureSeq.Add(1))
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO items(id,household_id,subject_id,title,attention_on) VALUES($1::uuid,$2::uuid,$3::uuid,'foreign-null',NULL)`, foreignNull, f.otherHouse, foreignSub); err != nil {
		t.Fatal(err)
	}
	p := f.list(t, item.ListQuery{Sort: item.SortAttention, Limit: 10})
	exactIDs(t, p.Items, []string{ownNull, ownDate})
	first := f.list(t, item.ListQuery{Sort: item.SortAttention, Limit: 1})
	var invalid *item.InvalidQueryError
	if _, err := f.svc.List(f.ctx, f.actor, f.otherHouse, item.ListQuery{Sort: item.SortAttention, Cursor: *first.NextCursor}); !errors.As(err, &invalid) || invalid.Parameter != "cursor" {
		t.Fatalf("cross-house cursor=%v", err)
	}
	if _, err := f.svc.List(f.ctx, f.outsider, f.house, item.ListQuery{Sort: item.SortAttention}); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("nonmember=%v", err)
	}
}
func TestAttentionSortEmptyAndExactPage(t *testing.T) {
	f := newAttentionFixture(t)
	p, err := f.svc.List(f.ctx, f.actor, f.house, item.ListQuery{Sort: item.SortAttention, SubjectID: f.otherSubject, Limit: 2})
	if err != nil || p.Items == nil || len(p.Items) != 0 || p.NextCursor != nil {
		t.Fatalf("empty=%+v err=%v", p, err)
	}
	a := f.add(t, f.subject, "", false, false, false, "open", nil)
	b := f.add(t, f.subject, "2026-02-01", false, false, false, "open", nil)
	p = f.list(t, item.ListQuery{Sort: item.SortAttention, SubjectID: f.subject, Limit: 2})
	exactIDs(t, p.Items, []string{a, b})
	if p.NextCursor != nil {
		t.Fatalf("exact page cursor=%v", *p.NextCursor)
	}
}
func exactIDStrings(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("IDs got=%v want=%v", got, want)
	}
}
