package postgres

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/bigtcze/tendo/backend/internal/subject"
	subjectpg "github.com/bigtcze/tendo/backend/internal/subject/postgres"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationURLs(t *testing.T) (string, string) {
	t.Helper()
	appURL, adminURL := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_DATABASE_ADMIN_URL")
	if appURL == "" || adminURL == "" {
		t.Fatal("TEST_DATABASE_URL and TEST_DATABASE_ADMIN_URL are required for real PostgreSQL item tests")
	}
	return appURL, adminURL
}
func pool(t *testing.T, ctx context.Context, url string) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}
func TestMigrationUpgradeFromV6PreservesData(t *testing.T) {
	_, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
	const dbName = "tendo_upgrade_v6_items_test"
	_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = dbName
	upAdmin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upAdmin.Close()
	if _, err = admin.Exec(ctx, `GRANT CONNECT ON DATABASE `+dbName+` TO tendo`); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `DROP TABLE item_completions; ALTER TABLE items DROP CONSTRAINT items_household_id_id_key; DROP INDEX items_household_archived_done_id_idx; CREATE INDEX items_household_archived_id_idx ON items(household_id,archived,id); ALTER TABLE items DROP COLUMN done; DELETE FROM tendo_schema_migrations WHERE version IN (7,8)`); err != nil {
		t.Fatal(err)
	}
	var hid, sid, iid string
	if err = upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('Upgrade v6','UTC') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','upgrade') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title) VALUES($1::uuid,$2::uuid,'legacy') RETURNING id::text`, hid, sid).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	var done bool
	if err = upAdmin.QueryRow(ctx, `SELECT done FROM items WHERE id=$1::uuid`, iid).Scan(&done); err != nil || done {
		t.Fatalf("done=%v err=%v", done, err)
	}
}

func TestItemsAgainstPostgres(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	reset := func() {
		t.Helper()
		c, cc := context.WithTimeout(context.Background(), 10*time.Second)
		defer cc()
		if _, err := admin.Exec(c, `TRUNCATE user_accounts, household_memberships, households CASCADE; UPDATE installation_state SET setup_required=true`); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	var role string
	if err := app.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil || role != "tendo" {
		t.Fatalf("runtime role=%q err=%v", role, err)
	}
	household := func(name string) string {
		t.Helper()
		var id string
		if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES ($1,'UTC') RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	hA, hB := household("A"), household("B")
	subRepo := subjectpg.NewRepository(app)
	newSubject := func(hid, name string) subject.Subject {
		t.Helper()
		s, err := subRepo.Create(ctx, hid, name, subject.TypePerson)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	sA, sB := newSubject(hA, "Person A"), newSubject(hB, "Person B")
	var completionUser string
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES ('item_completion_actor') RETURNING id::text`).Scan(&completionUser); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, completionUser, hA); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(app)
	create := func(hid, sid, title string, notes *string, attention *schedule.Date) item.Item {
		t.Helper()
		got, err := repo.Create(ctx, hid, item.Draft{SubjectID: sid, Title: title, Notes: notes, AttentionOn: attention})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	count := func(hid string) int {
		t.Helper()
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid`, hid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	t.Run("create persists Unicode fields", func(t *testing.T) {
		notes := "  notes\n家族 🚗  "
		day, _ := schedule.NewDate(2030, time.January, 2)
		got := create(hA, sA.ID, "Veselí 家族 🚗", &notes, &day)
		if got.Title != "Veselí 家族 🚗" || got.Notes == nil || *got.Notes != notes || got.AttentionOn == nil || *got.AttentionOn != day || got.Version != 1 || got.WorkflowState != item.StateOpen || got.HouseholdID != hA {
			t.Fatalf("item=%+v", got)
		}
		var title, storedNotes, state string
		var date time.Time
		if err := admin.QueryRow(ctx, `SELECT title,notes,attention_on,workflow_state FROM items WHERE id=$1::uuid`, got.ID).Scan(&title, &storedNotes, &date, &state); err != nil {
			t.Fatal(err)
		}
		if title != got.Title || storedNotes != notes || date.Format("2006-01-02") != "2030-01-02" || state != "open" {
			t.Fatalf("stored=%q %q %v %q", title, storedNotes, date, state)
		}
	})
	t.Run("recurrence round trips, clears, and respects database constraints", func(t *testing.T) {
		for _, tc := range []struct {
			value int
			unit  schedule.Unit
			mode  schedule.Mode
		}{
			{1, schedule.UnitDay, schedule.ModeFixed}, {2, schedule.UnitWeek, schedule.ModeAfterCompletion}, {3, schedule.UnitMonth, schedule.ModeFixed}, {4, schedule.UnitYear, schedule.ModeAfterCompletion},
		} {
			policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: tc.value, Unit: tc.unit}, Mode: tc.mode}
			i := create(hA, sA.ID, "recurrence", nil, nil)
			updated, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Recurrence: item.Some(*policy)})
			if err != nil || updated.Recurrence == nil || *updated.Recurrence != *policy {
				t.Fatalf("round trip=%+v err=%v", updated, err)
			}
			got, err := repo.Get(ctx, hA, i.ID)
			if err != nil || got.Recurrence == nil || *got.Recurrence != *policy {
				t.Fatalf("get=%+v err=%v", got, err)
			}
			cleared, err := repo.Update(ctx, hA, i.ID, 2, item.Change{Recurrence: item.Null[schedule.Policy]()})
			if err != nil || cleared.Recurrence != nil {
				t.Fatalf("clear=%+v err=%v", cleared, err)
			}
		}
		for _, stmt := range []string{
			`INSERT INTO items(household_id,subject_id,title,recurrence_interval_value) VALUES ($1::uuid,$2::uuid,'partial',1)`,
			`INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'zero',0,'day','fixed')`,
			`INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'high',1000,'day','fixed')`,
		} {
			if _, err := admin.Exec(ctx, stmt, hA, sA.ID); err == nil {
				t.Fatalf("constraint accepted %s", stmt)
			}
		}
		i := create(hA, sA.ID, "runtime recurrence", nil, nil)
		_, err := app.Exec(ctx, `UPDATE items SET recurrence_interval_value=1,recurrence_interval_unit='year',recurrence_mode='fixed' WHERE id=$1::uuid`, i.ID)
		if err != nil {
			t.Fatalf("runtime recurrence update denied: %v", err)
		}
	})
	t.Run("pagination no duplicate and archive filter", func(t *testing.T) {
		h := household("Pages")
		s := newSubject(h, "subject")
		var ids []string
		for i := 0; i < 7; i++ {
			ids = append(ids, create(h, s.ID, "row "+string(rune('a'+i)), nil, nil).ID)
		}
		var got []string
		after := "00000000-0000-0000-0000-000000000000"
		for {
			rows, err := repo.List(ctx, h, false, false, after, 3)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range rows {
				got = append(got, v.ID)
			}
			if len(rows) < 3 {
				break
			}
			after = rows[len(rows)-1].ID
		}
		if strings.Join(got, ",") != strings.Join(ids, ",") {
			t.Fatalf("got=%v want=%v", got, ids)
		}
		yes := true
		if _, err := repo.Update(ctx, h, ids[1], 1, item.Change{Archived: &yes}); err != nil {
			t.Fatal(err)
		}
		active, err := repo.List(ctx, h, false, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil || len(active) != 6 {
			t.Fatalf("active=%d err=%v", len(active), err)
		}
		archived, err := repo.List(ctx, h, true, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil || len(archived) != 1 || archived[0].ID != ids[1] {
			t.Fatalf("archived=%+v err=%v", archived, err)
		}
	})
	t.Run("version mismatch leaves row unchanged", func(t *testing.T) {
		i := create(hA, sA.ID, "stable", nil, nil)
		title := "changed"
		if _, err := repo.Update(ctx, hA, i.ID, 9, item.Change{Title: &title}); !errors.Is(err, item.ErrVersionMismatch) {
			t.Fatalf("err=%v", err)
		}
		got, err := repo.Get(ctx, hA, i.ID)
		if err != nil || got != i {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		updated, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Title: &title})
		if err != nil || updated.Version != 2 || updated.Title != title {
			t.Fatalf("updated=%+v err=%v", updated, err)
		}
	})
	t.Run("concurrent updates exactly one succeeds", func(t *testing.T) {
		i := create(hA, sA.ID, "race", nil, nil)
		const workers = 8
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, workers)
		for n := range workers {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				v := "writer" + string(rune('a'+n))
				_, errs[n] = repo.Update(ctx, hA, i.ID, 1, item.Change{Title: &v})
			}(n)
		}
		close(start)
		wg.Wait()
		ok, stale := 0, 0
		for _, err := range errs {
			if err == nil {
				ok++
			} else if errors.Is(err, item.ErrVersionMismatch) {
				stale++
			} else {
				t.Fatalf("err=%v", err)
			}
		}
		if ok != 1 || stale != workers-1 {
			t.Fatalf("success=%d stale=%d", ok, stale)
		}
	})
	t.Run("completion concurrency", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.October, 7)
		next, _ := schedule.NewDate(2027, time.October, 7)
		i := create(hA, sA.ID, "completion concurrency", nil, &anchor)
		var version int64 = 1
		makeDecide := func(key string) item.CompletionDecider {
			return func(current item.Item, existing *item.Completion) (item.CompletionPlan, error) {
				if existing != nil {
					return item.CompletionPlan{Receipt: *existing, Item: current}, nil
				}
				if current.Version != version {
					return item.CompletionPlan{}, item.ErrVersionMismatch
				}
				receipt := item.Completion{HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: anchor, PriorWorkflowState: current.WorkflowState, ItemVersionBefore: current.Version, IdempotencyKey: key}
				change := current
				change.AttentionOn = &next
				change.Version++
				return item.CompletionPlan{Receipt: receipt, Item: change}, nil
			}
		}
		const workers = 12
		start := make(chan struct{})
		errs := make([]error, workers)
		var wg sync.WaitGroup
		for x := range workers {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				key := "different-" + strconv.Itoa(n)
				_, _, errs[n] = repo.Complete(ctx, hA, i.ID, key, [32]byte{byte(n + 1)}, makeDecide(key))
			}(x)
		}
		close(start)
		wg.Wait()
		wins := 0
		for _, err := range errs {
			if err == nil {
				wins++
			} else if !errors.Is(err, item.ErrVersionMismatch) {
				t.Fatalf("concurrent err=%v unwrap=%v", err, errors.Unwrap(err))
			}
		}
		if wins != 1 {
			t.Fatalf("wins=%d errors=%v", wins, errs)
		}
		var rows int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&rows); err != nil || rows != 1 {
			t.Fatalf("completion count=%d err=%v", rows, err)
		}
		current, _ := repo.Get(ctx, hA, i.ID)
		version = current.Version
		start2 := make(chan struct{})
		results := make([]item.Completion, workers)
		errs2 := make([]error, workers)
		for x := range workers {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start2
				results[n], _, errs2[n] = repo.Complete(ctx, hA, i.ID, "same-key", [32]byte{99}, makeDecide("same-key"))
			}(x)
		}
		close(start2)
		wg.Wait()
		id := results[0].ID
		for x := range workers {
			if errs2[x] != nil || results[x].ID != id {
				t.Fatalf("same key result %d=%+v err=%v first=%s", x, results[x], errs2[x], id)
			}
		}
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&rows); err != nil || rows != 2 {
			t.Fatalf("same-key count=%d err=%v", rows, err)
		}
	})
	t.Run("completion vs patch race", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.October, 7)
		next, _ := schedule.NewDate(2027, time.October, 7)
		i := create(hA, sA.ID, "completion patch race", nil, &anchor)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var ce, pe error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, _, ce = repo.Complete(ctx, hA, i.ID, "race-key", [32]byte{77}, func(cur item.Item, ex *item.Completion) (item.CompletionPlan, error) {
				if ex != nil {
					return item.CompletionPlan{Receipt: *ex, Item: cur}, nil
				}
				if cur.Version != 1 {
					return item.CompletionPlan{}, item.ErrVersionMismatch
				}
				receipt := item.Completion{HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: anchor, PriorWorkflowState: cur.WorkflowState, ItemVersionBefore: 1, IdempotencyKey: "race-key"}
				updated := cur
				updated.AttentionOn = &next
				updated.Version++
				return item.CompletionPlan{Receipt: receipt, Item: updated}, nil
			})
		}()
		go func() {
			defer wg.Done()
			<-start
			title := "patched"
			_, pe = repo.Update(ctx, hA, i.ID, 1, item.Change{Title: &title})
		}()
		close(start)
		wg.Wait()
		if ce != nil && !errors.Is(ce, item.ErrVersionMismatch) {
			t.Fatalf("completion error %v", ce)
		}
		if pe != nil && !errors.Is(pe, item.ErrVersionMismatch) {
			t.Fatalf("patch error %v", pe)
		}
		if (ce == nil) == (pe == nil) {
			t.Fatalf("expected exactly one winner completion=%v patch=%v", ce, pe)
		}
		stored, err := repo.Get(ctx, hA, i.ID)
		if err != nil || stored.Version != 2 {
			t.Fatalf("state=%+v err=%v", stored, err)
		}
		var count int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if ce == nil && (count != 1 || stored.AttentionOn == nil || stored.AttentionOn.String() != "2027-10-07") {
			t.Fatalf("completion winner state=%+v receipts=%d", stored, count)
		}
		if pe == nil && (count != 0 || stored.Title != "patched" || stored.AttentionOn == nil || stored.AttentionOn.String() != "2026-10-07") {
			t.Fatalf("patch winner state=%+v receipts=%d", stored, count)
		}
	})
	t.Run("completion rollback after insert hook", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.October, 7)
		i := create(hA, sA.ID, "hook rollback", nil, &anchor)
		completeAfterInsertHook = func() error { return item.ErrUnavailable }
		_, _, err := repo.Complete(ctx, hA, i.ID, "hook-rollback", [32]byte{88}, func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			receipt := item.Completion{HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: anchor, PriorWorkflowState: cur.WorkflowState, ItemVersionBefore: cur.Version, IdempotencyKey: "hook-rollback"}
			changed := cur
			changed.Done = true
			changed.Version++
			return item.CompletionPlan{Receipt: receipt, Item: changed}, nil
		})
		completeAfterInsertHook = nil
		if !errors.Is(err, item.ErrUnavailable) {
			t.Fatalf("forced failure err=%v", err)
		}
		stored, err := repo.Get(ctx, hA, i.ID)
		if err != nil || stored.Version != 1 || stored.Done {
			t.Fatalf("stored after rollback=%+v err=%v", stored, err)
		}
		var count int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("receipt after rollback=%d err=%v", count, err)
		}
	})
	t.Run("completion overflow rolls back", func(t *testing.T) {
		end, _ := schedule.NewDate(9999, time.December, 31)
		iid := create(hA, sA.ID, "overflow completion", nil, &end).ID
		policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
		_, err := repo.Update(ctx, hA, iid, 1, item.Change{Recurrence: item.Some(policy)})
		if err != nil {
			t.Fatal(err)
		}
		before, err := repo.Get(ctx, hA, iid)
		if err != nil {
			t.Fatal(err)
		}
		var historyBefore int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, iid).Scan(&historyBefore); err != nil || historyBefore != 0 {
			t.Fatalf("pre-overflow history=%d err=%v", historyBefore, err)
		}
		houses := func(context.Context, string, string) (string, error) { return "UTC", nil }
		subjects := func(context.Context, string, string, string) (bool, error) { return false, nil }
		svc := item.NewService(repo, houses, subjects, func() time.Time { return time.Date(9999, time.December, 31, 12, 0, 0, 0, time.UTC) })
		_, _, err = svc.Complete(ctx, completionUser, hA, iid, before.Version, "overflow", item.CompletionRequest{})
		var validation *item.ValidationError
		if !errors.As(err, &validation) || validation.Field != "recurrence" || validation.Code != "date_overflow" {
			t.Fatalf("overflow err=%v", err)
		}
		after, err := repo.Get(ctx, hA, iid)
		if err != nil || after.Version != before.Version || after.Done != before.Done || after.AttentionOn.String() != before.AttentionOn.String() || after.Title != before.Title || after.Recurrence == nil || *after.Recurrence != *before.Recurrence {
			t.Fatalf("overflow changed item before=%+v after=%+v err=%v", before, after, err)
		}
		var n int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, iid).Scan(&n); err != nil || n != historyBefore {
			t.Fatalf("overflow receipt count=%d err=%v", n, err)
		}
	})

	t.Run("completion receipts", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.October, 7)
		next, _ := schedule.NewDate(2027, time.October, 7)
		policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
		i := create(hA, sA.ID, "completion target", nil, &anchor)
		updated, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Recurrence: item.Some(*policy)})
		if err != nil {
			t.Fatal(err)
		}
		decide := func(current item.Item, existing *item.Completion) (item.CompletionPlan, error) {
			if existing != nil {
				return item.CompletionPlan{Receipt: *existing, Item: current}, nil
			}
			receipt := item.Completion{HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: anchor, CycleAttentionOn: updated.AttentionOn, Recurrence: updated.Recurrence, PriorWorkflowState: updated.WorkflowState, NextAttentionOn: &next, ItemVersionBefore: updated.Version, IdempotencyKey: "same"}
			change := current
			change.AttentionOn = &next
			change.Version++
			return item.CompletionPlan{Receipt: receipt, Item: change}, nil
		}
		finger := [32]byte{1}
		first, replayed, err := repo.Complete(ctx, hA, i.ID, "same", finger, decide)
		if err != nil || replayed {
			t.Fatalf("complete=%+v replay=%v err=%v", first, replayed, err)
		}
		after, _ := repo.Get(ctx, hA, i.ID)
		if after.Version != 3 || after.AttentionOn == nil || after.AttentionOn.String() != "2027-10-07" {
			t.Fatalf("stored item=%+v", after)
		}
		replay, replayed, err := repo.Complete(ctx, hA, i.ID, "same", finger, decide)
		if err != nil || !replayed || replay.ID != first.ID {
			t.Fatalf("replay=%+v replayed=%v err=%v", replay, replayed, err)
		}
		after, _ = repo.Get(ctx, hA, i.ID)
		if after.Version != 3 || replay.CompletedOn != first.CompletedOn || replay.ID != first.ID {
			t.Fatalf("replay changed receipt or version first=%+v replay=%+v item=%+v", first, replay, after)
		}
		rows, err := repo.ListCompletions(ctx, hA, i.ID, "00000000-0000-0000-0000-000000000000", 10)
		if err != nil || len(rows) != 1 || rows[0].Recurrence == nil || rows[0].NextAttentionOn.String() != "2027-10-07" {
			t.Fatalf("history=%+v err=%v", rows, err)
		}
		if _, err = repo.ListCompletions(ctx, hA, first.ID, "00000000-0000-0000-0000-000000000000", 10); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("foreign/nonexistent item list=%v", err)
		}
	})
	t.Run("descending completion dates preserve version-order latest", func(t *testing.T) {
		anchor, _ := schedule.NewDate(2026, time.March, 1)
		policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
		i := create(hA, sA.ID, "descending dates", nil, &anchor)
		updated, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Recurrence: item.Some(policy)})
		if err != nil {
			t.Fatal(err)
		}
		dates := []string{"2026-03-20", "2026-03-10"}
		version := updated.Version
		var lastID string
		for n := range dates {
			on, _ := schedule.NewDate(2026, time.March, 20-10*n)
			next, _ := schedule.NewDate(2027, time.March, 1)
			key := strconv.Itoa(n)
			created, _, err := repo.Complete(ctx, hA, i.ID, key, [32]byte{byte(n + 1)}, func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
				receipt := item.Completion{ID: "", HouseholdID: hA, ItemID: i.ID, CompletedByUserID: completionUser, CompletedOn: on, PriorWorkflowState: cur.WorkflowState, Recurrence: cur.Recurrence, ItemVersionBefore: cur.Version, IdempotencyKey: key, NextAttentionOn: &next}
				changed := cur
				changed.Version++
				changed.AttentionOn = &next
				return item.CompletionPlan{Receipt: receipt, Item: changed}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			lastID = created.ID
			version++
		}
		got, err := repo.Get(ctx, hA, i.ID)
		if err != nil || got.LastCompletedOn == nil || got.LastCompletedOn.String() != "2026-03-10" {
			t.Fatalf("last completion=%v err=%v", got.LastCompletedOn, err)
		}
		var storedDate string
		if err = admin.QueryRow(ctx, `SELECT completed_on::text FROM item_completions WHERE id=$1::uuid`, lastID).Scan(&storedDate); err != nil || storedDate != "2026-03-10" {
			t.Fatalf("stored latest receipt date=%s err=%v", storedDate, err)
		}
		list, err := repo.List(ctx, hA, false, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil || len(list) == 0 || list[len(list)-1].LastCompletedOn == nil || list[len(list)-1].LastCompletedOn.String() != "2026-03-10" {
			t.Fatalf("list latest=%+v err=%v", list, err)
		}
		title := "after dates"
		patched, err := repo.Update(ctx, hA, i.ID, version, item.Change{Title: &title})
		if err != nil || patched.LastCompletedOn == nil || patched.LastCompletedOn.String() != "2026-03-10" {
			t.Fatalf("patch latest=%+v err=%v", patched, err)
		}
	})
	t.Run("foreign household completion is not found", func(t *testing.T) {
		i := create(hB, sB.ID, "foreign completion", nil, nil)
		decide := func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			return item.CompletionPlan{}, errors.New("decide should not be called")
		}
		if _, _, err := repo.Complete(ctx, hA, i.ID, "foreign", [32]byte{1}, decide); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("complete err=%v", err)
		}
		if _, err := repo.ListCompletions(ctx, hA, i.ID, "00000000-0000-0000-0000-000000000000", 10); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("list err=%v", err)
		}
		stored, err := repo.Get(ctx, hB, i.ID)
		if err != nil || stored.Version != 1 || stored.Done {
			t.Fatalf("foreign item mutated: %+v err=%v", stored, err)
		}
		var n int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("foreign receipts=%d err=%v", n, err)
		}
	})
	t.Run("household isolation and foreign subject rejected", func(t *testing.T) {
		i := create(hB, sB.ID, "private", nil, nil)
		beforeItem, err := repo.Get(ctx, hB, i.ID)
		if err != nil {
			t.Fatal(err)
		}
		var beforeCount int
		if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&beforeCount); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Get(ctx, hA, i.ID); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("get=%v", err)
		}
		title := "stolen"
		if _, err := repo.Update(ctx, hA, i.ID, 1, item.Change{Title: &title}); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("update=%v", err)
		}
		rows, err := repo.List(ctx, hA, false, false, "00000000-0000-0000-0000-000000000000", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range rows {
			if v.ID == i.ID || v.HouseholdID != hA {
				t.Fatalf("foreign list entry=%+v", v)
			}
		}
		if _, _, err := repo.Complete(ctx, hA, i.ID, "foreign-key", [32]byte{5}, func(cur item.Item, _ *item.Completion) (item.CompletionPlan, error) {
			return item.CompletionPlan{}, errors.New("should not reach decision")
		}); !errors.Is(err, item.ErrNotFound) {
			t.Fatalf("foreign completion post err=%v", err)
		}
		if _, err := repo.Create(ctx, hA, item.Draft{SubjectID: sB.ID, Title: "bad"}); !errors.Is(err, item.ErrInvalidReference) {
			t.Fatalf("foreign subject err=%v", err)
		}
		var foreignCount int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM items WHERE household_id=$1::uuid AND subject_id=$2::uuid`, hA, sB.ID).Scan(&foreignCount); err != nil || foreignCount != 0 {
			t.Fatalf("foreign item persisted; count=%d err=%v", foreignCount, err)
		}
		afterItem, err := repo.Get(ctx, hB, i.ID)
		if err != nil || afterItem != beforeItem {
			t.Fatalf("foreign POST changed item before=%+v after=%+v err=%v", beforeItem, afterItem, err)
		}
		var afterCount int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&afterCount); err != nil || afterCount != beforeCount {
			t.Fatalf("foreign POST changed receipts before=%d after=%d err=%v", beforeCount, afterCount, err)
		}
	})
	t.Run("undo completion persistence behavior", func(t *testing.T) {
		today := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
		houses := func(_ context.Context, userID, householdID string) (string, error) {
			if userID != completionUser || householdID != hA {
				return "", item.ErrNotFound
			}
			return "UTC", nil
		}
		subjects := func(context.Context, string, string, string) (bool, error) { return false, nil }
		svc := item.NewService(repo, houses, subjects, func() time.Time { return today })
		mustDate := func(text string) *string { return &text }
		setPolicy := func(i item.Item, policy schedule.Policy) item.Item {
			t.Helper()
			got, err := svc.Update(ctx, completionUser, hA, i.ID, i.Version, item.Patch{Recurrence: item.Some(policy)})
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		getReceipt := func(itemID, receiptID string) item.Completion {
			t.Helper()
			page, err := svc.ListCompletions(ctx, completionUser, hA, itemID, 100, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, receipt := range page.Items {
				if receipt.ID == receiptID {
					return receipt
				}
			}
			t.Fatalf("receipt %s missing from %+v", receiptID, page.Items)
			return item.Completion{}
		}

		t.Run("fixed restore waiting snapshot and exact row", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.May, 1)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "undo fixed", nil, &anchor)
			i = setPolicy(i, policy)
			waiting := string(item.StateWaiting)
			i, err := svc.Update(ctx, completionUser, hA, i.ID, i.Version, item.Patch{WorkflowState: &waiting})
			if err != nil {
				t.Fatal(err)
			}
			completed, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "undo-fixed", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			beforeUndo, err := repo.Get(ctx, hA, i.ID)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := svc.UndoCompletion(ctx, completionUser, hA, i.ID, completed.ID, beforeUndo.Version)
			if err != nil {
				t.Fatal(err)
			}
			var storedAnchor, state string
			var done bool
			var version int64
			var undoneAt *time.Time
			var undoneBy *string
			if err = admin.QueryRow(ctx, `SELECT attention_on::text,workflow_state,done,version FROM items WHERE id=$1::uuid`, i.ID).Scan(&storedAnchor, &state, &done, &version); err != nil {
				t.Fatal(err)
			}
			if err = admin.QueryRow(ctx, `SELECT undone_at,undone_by_user_id::text FROM item_completions WHERE id=$1::uuid`, completed.ID).Scan(&undoneAt, &undoneBy); err != nil {
				t.Fatal(err)
			}
			if storedAnchor != "2026-05-01" || state != "waiting" || done || version != beforeUndo.Version+1 || undoneAt == nil || undoneBy == nil || *undoneBy != completionUser || receipt.UndoneAt == nil || receipt.UndoneByUserID == nil || *receipt.UndoneByUserID != completionUser {
				t.Fatalf("restored row attention=%s state=%s done=%v version=%d receipt=%+v dbUndo=%v/%v", storedAnchor, state, done, version, receipt, undoneAt, undoneBy)
			}
			got, err := repo.Get(ctx, hA, i.ID)
			if err != nil || got.LastCompletedOn != nil {
				t.Fatalf("lastCompletedOn=%v err=%v", got.LastCompletedOn, err)
			}
		})

		t.Run("one-off retained and listed", func(t *testing.T) {
			i := create(hA, sA.ID, "undo one-off", nil, nil)
			completed, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "undo-oneoff", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			done, err := repo.Get(ctx, hA, i.ID)
			if err != nil || !done.Done {
				t.Fatalf("before undo=%+v err=%v", done, err)
			}
			undone, err := svc.UndoCompletion(ctx, completionUser, hA, i.ID, completed.ID, done.Version)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := repo.Get(ctx, hA, i.ID)
			if err != nil || stored.Done || stored.Version != done.Version+1 {
				t.Fatalf("after undo=%+v err=%v", stored, err)
			}
			listed := getReceipt(i.ID, completed.ID)
			var n int
			if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE id=$1::uuid`, completed.ID).Scan(&n); err != nil || n != 1 || listed.UndoneAt == nil || undone.UndoneAt == nil {
				t.Fatalf("receipt rows=%d listed=%+v returned=%+v err=%v", n, listed, undone, err)
			}
		})

		t.Run("latest active chained undo uses version order", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.March, 1)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "undo chain", nil, &anchor)
			i = setPolicy(i, policy)
			first, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "chain-first", item.CompletionRequest{CompletedOn: mustDate("2026-03-20")})
			if err != nil {
				t.Fatal(err)
			}
			second, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version+1, "chain-second", item.CompletionRequest{CompletedOn: mustDate("2026-03-10")})
			if err != nil {
				t.Fatal(err)
			}
			current, err := repo.Get(ctx, hA, i.ID)
			if err != nil || current.LastCompletedOn == nil || current.LastCompletedOn.String() != "2026-03-10" {
				t.Fatalf("latest by version before undo=%+v err=%v", current, err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, i.ID, first.ID, current.Version); !errors.Is(err, item.ErrCompletionNotLatest) {
				t.Fatalf("first undo error=%v", err)
			}
			unchanged, err := repo.Get(ctx, hA, i.ID)
			if err != nil || unchanged.Version != current.Version || unchanged.LastCompletedOn == nil || unchanged.LastCompletedOn.String() != "2026-03-10" {
				t.Fatalf("rejected undo mutated state %+v err=%v", unchanged, err)
			}
			firstStillActive := getReceipt(i.ID, first.ID)
			secondStillActive := getReceipt(i.ID, second.ID)
			if firstStillActive.UndoneAt != nil || secondStillActive.UndoneAt != nil {
				t.Fatalf("rejected undo changed receipt rows first=%+v second=%+v", firstStillActive, secondStillActive)
			}
			var activeCount int
			if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid AND undone_at IS NULL`, i.ID).Scan(&activeCount); err != nil || activeCount != 2 {
				t.Fatalf("rejected undo active receipt count=%d err=%v", activeCount, err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, i.ID, second.ID, current.Version); err != nil {
				t.Fatal(err)
			}
			middle, err := repo.Get(ctx, hA, i.ID)
			if err != nil || middle.AttentionOn == nil || middle.AttentionOn.String() != "2027-03-01" || middle.LastCompletedOn == nil || middle.LastCompletedOn.String() != "2026-03-20" {
				t.Fatalf("after second undo=%+v err=%v", middle, err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, i.ID, first.ID, middle.Version); err != nil {
				t.Fatal(err)
			}
			final, err := repo.Get(ctx, hA, i.ID)
			if err != nil || final.AttentionOn == nil || final.AttentionOn.String() != "2026-03-01" || final.LastCompletedOn != nil {
				t.Fatalf("after chained undo=%+v err=%v", final, err)
			}
		})

		t.Run("undo replay stale version one bump and complete again", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.January, 15)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "undo replay complete again", nil, &anchor)
			i = setPolicy(i, policy)
			completed, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "undo-replay", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			before, err := repo.Get(ctx, hA, i.ID)
			if err != nil {
				t.Fatal(err)
			}
			first, err := svc.UndoCompletion(ctx, completionUser, hA, i.ID, completed.ID, before.Version)
			if err != nil {
				t.Fatal(err)
			}
			after, err := repo.Get(ctx, hA, i.ID)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := svc.UndoCompletion(ctx, completionUser, hA, i.ID, completed.ID, 1)
			if err != nil || replay.ID != first.ID || replay.UndoneAt == nil || after.Version != before.Version+1 {
				t.Fatalf("replay=%+v after=%+v err=%v", replay, after, err)
			}
			repeated, _, err := svc.Complete(ctx, completionUser, hA, i.ID, after.Version, "undo-recomplete", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			var beforeVersion int64
			var next string
			if err = admin.QueryRow(ctx, `SELECT item_version_before,next_attention_on::text FROM item_completions WHERE id=$1::uuid`, repeated.ID).Scan(&beforeVersion, &next); err != nil {
				t.Fatal(err)
			}
			if beforeVersion != after.Version || next != "2027-01-15" {
				t.Fatalf("new receipt version=%d want=%d next=%s", beforeVersion, after.Version, next)
			}
			originalReplay, replayed, err := svc.Complete(ctx, completionUser, hA, i.ID, after.Version, "undo-replay", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil || !replayed || originalReplay.ID != completed.ID || originalReplay.CompletedOn.String() != "2026-10-06" || originalReplay.UndoneAt == nil || originalReplay.NextAttentionOn == nil || originalReplay.NextAttentionOn.String() != "2027-01-15" {
				t.Fatalf("original key/body replay=%+v replayed=%v err=%v", originalReplay, replayed, err)
			}
			replayedItem, err := repo.Get(ctx, hA, i.ID)
			if err != nil || replayedItem.Version != after.Version+1 {
				t.Fatalf("idempotent original replay changed item=%+v err=%v", replayedItem, err)
			}
			var count int
			if err = admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid AND item_version_before=$2`, i.ID, beforeVersion).Scan(&count); err != nil || count != 1 {
				t.Fatalf("version uniqueness count=%d err=%v", count, err)
			}
		})

		t.Run("foreign item and household completion IDs not found", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.January, 1)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "foreign undo source", nil, &anchor)
			i = setPolicy(i, policy)
			c, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "foreign-undo", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			wrong := create(hA, sA.ID, "wrong target", nil, &anchor)
			if _, err = svc.UndoCompletion(ctx, completionUser, hA, wrong.ID, c.ID, wrong.Version); !errors.Is(err, item.ErrNotFound) {
				t.Fatalf("wrong-item undo=%v", err)
			}
			if _, err = svc.UndoCompletion(ctx, completionUser, hB, i.ID, c.ID, i.Version+1); !errors.Is(err, item.ErrNotFound) {
				t.Fatalf("foreign-household undo=%v", err)
			}
			stored, err := repo.Get(ctx, hA, i.ID)
			if err != nil || stored.Version != i.Version+1 {
				t.Fatalf("source changed=%+v err=%v", stored, err)
			}
			if _, err = admin.Exec(ctx, `UPDATE household_memberships SET role='owner' WHERE household_id=$1::uuid AND user_id=$2::uuid`, hA, completionUser); err != nil {
				t.Fatal(err)
			}
		})

		t.Run("concurrent duplicate undo and patch race", func(t *testing.T) {
			anchor, _ := schedule.NewDate(2026, time.January, 1)
			policy := schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
			i := create(hA, sA.ID, "undo concurrent", nil, &anchor)
			i = setPolicy(i, policy)
			c, _, err := svc.Complete(ctx, completionUser, hA, i.ID, i.Version, "undo-concurrent", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			current, err := repo.Get(ctx, hA, i.ID)
			if err != nil {
				t.Fatal(err)
			}
			const workers = 8
			start := make(chan struct{})
			var wg sync.WaitGroup
			errs := make([]error, workers)
			for n := range workers {
				wg.Add(1)
				go func(n int) {
					defer wg.Done()
					<-start
					_, errs[n] = svc.UndoCompletion(ctx, completionUser, hA, i.ID, c.ID, current.Version)
				}(n)
			}
			close(start)
			wg.Wait()
			for n, callErr := range errs {
				if callErr != nil && !errors.Is(callErr, item.ErrVersionMismatch) {
					t.Fatalf("undo call %d err=%v", n, callErr)
				}
			}
			final, err := repo.Get(ctx, hA, i.ID)
			if err != nil || final.Version != current.Version+1 || final.AttentionOn == nil || final.AttentionOn.String() != "2026-01-01" {
				t.Fatalf("concurrent undo final=%+v err=%v", final, err)
			}
			// A fresh item has one undo and one ordinary item PATCH competing for its row version.
			j := create(hA, sA.ID, "undo patch race", nil, &anchor)
			j = setPolicy(j, policy)
			receipt, _, err := svc.Complete(ctx, completionUser, hA, j.ID, j.Version, "undo-patch-race", item.CompletionRequest{CompletedOn: mustDate("2026-10-06")})
			if err != nil {
				t.Fatal(err)
			}
			versioned, err := repo.Get(ctx, hA, j.ID)
			if err != nil {
				t.Fatal(err)
			}
			start = make(chan struct{})
			var undoErr, patchErr error
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				_, undoErr = svc.UndoCompletion(ctx, completionUser, hA, j.ID, receipt.ID, versioned.Version)
			}()
			go func() {
				defer wg.Done()
				<-start
				title := "raced edit"
				_, patchErr = svc.Update(ctx, completionUser, hA, j.ID, versioned.Version, item.Patch{Title: &title})
			}()
			close(start)
			wg.Wait()
			if (undoErr == nil) == (patchErr == nil) || (undoErr != nil && !errors.Is(undoErr, item.ErrVersionMismatch)) || (patchErr != nil && !errors.Is(patchErr, item.ErrVersionMismatch)) {
				t.Fatalf("undo/patch race undo=%v patch=%v", undoErr, patchErr)
			}
			winner, err := repo.Get(ctx, hA, j.ID)
			if err != nil || winner.Version != versioned.Version+1 {
				t.Fatalf("race winner state=%+v err=%v", winner, err)
			}
		})
	})

	t.Run("completion table restrictions and constraints", func(t *testing.T) {
		i := create(hA, sA.ID, "completion protected", nil, nil)
		uid := completionUser
		if _, err := admin.Exec(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint) VALUES($1::uuid,$2::uuid,'2026-10-07',$3::uuid,'open',1,'key',decode(repeat('01',32),'hex'))`, hA, i.ID, uid); err != nil {
			t.Fatalf("baseline receipt insert: %v", err)
		}
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE item_id=$1::uuid`, i.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("baseline receipt count=%d want=1", n)
		}
		if _, err := app.Exec(ctx, `UPDATE item_completions SET undone_at=now(), undone_by_user_id=$2::uuid WHERE id=(SELECT id FROM item_completions WHERE item_id=$1::uuid)`, i.ID, uid); err != nil {
			t.Fatalf("runtime undo column update: %v", err)
		}
		if _, err := admin.Exec(ctx, `UPDATE item_completions SET undone_at=NULL, undone_by_user_id=NULL WHERE item_id=$1::uuid`, i.ID); err != nil {
			t.Fatal(err)
		}
		for _, stmt := range []string{`UPDATE item_completions SET id=uuidv7()`, `UPDATE item_completions SET completed_on=completed_on`, `DELETE FROM item_completions`, `TRUNCATE item_completions`} {
			if _, err := app.Exec(ctx, stmt); err == nil {
				t.Fatalf("runtime role allowed %s", stmt)
			} else {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
					t.Fatalf("%s error=%v", stmt, err)
				}
			}
		}
		for _, tc := range []struct{ name, key, fp, value, unit, mode, state string }{{"key", "bad key ", strings.Repeat("01", 32), "", "", "", "open"}, {"fingerprint", "bad-fingerprint", "01", "", "", "", "open"}, {"partial recurrence", "partial", strings.Repeat("01", 32), "1", "", "", "open"},
			{"interval null value", "null-value", strings.Repeat("01", 32), "", "day", "fixed", "open"},
			{"interval null unit", "null-unit", strings.Repeat("01", 32), "1", "", "fixed", "open"},
			{"interval null mode", "null-mode", strings.Repeat("01", 32), "1", "day", "", "open"}, {"interval lower bound", "low", strings.Repeat("01", 32), "0", "day", "fixed", "open"}, {"interval upper bound", "high", strings.Repeat("01", 32), "1000", "day", "fixed", "open"}, {"unit enum", "unit", strings.Repeat("01", 32), "1", "fortnight", "fixed", "open"}, {"mode enum", "mode", strings.Repeat("01", 32), "1", "day", "weekly", "open"}, {"workflow enum", "state", strings.Repeat("01", 32), "", "", "", "done"}} {
			_, err := admin.Exec(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES($1::uuid,$2::uuid,'2026-10-07',$3::uuid,$4,2,$5,decode($6,'hex'),NULLIF($7,'')::int,NULLIF($8,''),NULLIF($9,''))`, hA, i.ID, uid, tc.state, tc.key, tc.fp, tc.value, tc.unit, tc.mode)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("%s SQLSTATE err=%v", tc.name, err)
			}
			want := map[string]string{"key": "item_completions_idempotency_key_check", "fingerprint": "item_completions_request_fingerprint_check", "partial recurrence": "item_completions_check", "interval null value": "item_completions_check", "interval null unit": "item_completions_check", "interval null mode": "item_completions_check", "interval lower bound": "item_completions_recurrence_interval_value_check", "interval upper bound": "item_completions_recurrence_interval_value_check", "unit enum": "item_completions_recurrence_interval_unit_check", "mode enum": "item_completions_recurrence_mode_check", "workflow enum": "item_completions_prior_workflow_state_check"}[tc.name]
			if pgErr.ConstraintName != want {
				t.Fatalf("%s constraint=%s want=%s err=%v", tc.name, pgErr.ConstraintName, want, err)
			}
		}
		_, pairErr := admin.Exec(ctx, `UPDATE item_completions SET undone_at=now(), undone_by_user_id=NULL WHERE item_id=$1::uuid`, i.ID)
		var pairPG *pgconn.PgError
		if !errors.As(pairErr, &pairPG) || pairPG.Code != "23514" || pairPG.ConstraintName != "item_completions_undo_actor_pair_check" {
			t.Fatalf("undo actor pair constraint err=%v", pairErr)
		}
		if _, err := admin.Exec(ctx, `UPDATE item_completions SET undone_at=NULL, undone_by_user_id=NULL WHERE item_id=$1::uuid`, i.ID); err != nil {
			t.Fatal(err)
		}
		_, err := admin.Exec(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,prior_workflow_state,item_version_before,idempotency_key,request_fingerprint) VALUES($1::uuid,uuidv7(),'2026-10-07',$2::uuid,'open',2,'bad-fk',decode(repeat('01',32),'hex'))`, hA, uid)
		var fk *pgconn.PgError
		if !errors.As(err, &fk) || fk.Code != "23503" || fk.ConstraintName != "item_completions_household_id_item_id_fkey" {
			t.Fatalf("composite FK err=%v", err)
		}
		_ = n
	})
	t.Run("runtime permissions and database constraints", func(t *testing.T) {
		i := create(hA, sA.ID, "protected", nil, nil)
		for _, stmt := range []string{`DELETE FROM items WHERE id=$1::uuid`, `TRUNCATE items`} {
			args := []any{i.ID}
			if stmt == `TRUNCATE items` {
				args = nil
			}
			if _, err := app.Exec(ctx, stmt, args...); err == nil || !strings.Contains(err.Error(), "permission denied") {
				t.Fatalf("%s err=%v", stmt, err)
			}
		}
		for field, stmt := range map[string]string{"id": `UPDATE items SET id=uuidv7() WHERE id=$1::uuid`, "household_id": `UPDATE items SET household_id=$2::uuid WHERE id=$1::uuid`, "created_at": `UPDATE items SET created_at=now() WHERE id=$1::uuid`} {
			args := []any{i.ID}
			if field == "household_id" {
				args = append(args, hB)
			}
			_, err := app.Exec(ctx, stmt, args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("update %s err=%v", field, err)
			}
		}
		for label, stmt := range map[string]string{"blank title": `INSERT INTO items(household_id,subject_id,title) VALUES ($1::uuid,$2::uuid,'  ')`, "empty notes": `INSERT INTO items(household_id,subject_id,title,notes) VALUES ($1::uuid,$2::uuid,'ok','')`, "bad state": `INSERT INTO items(household_id,subject_id,title,workflow_state) VALUES ($1::uuid,$2::uuid,'ok','done')`} {
			if _, err := admin.Exec(ctx, stmt, hA, sA.ID); err == nil {
				t.Fatalf("%s accepted", label)
			}
		}
		for _, tc := range []struct{ name, statement, constraint string }{
			{"partial value", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value) VALUES ($1::uuid,$2::uuid,'bad',1)`, "items_recurrence_all_or_none"},
			{"partial unit", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_unit) VALUES ($1::uuid,$2::uuid,'bad','day')`, "items_recurrence_all_or_none"},
			{"partial mode", `INSERT INTO items(household_id,subject_id,title,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad','fixed')`, "items_recurrence_all_or_none"},
			{"out of range low", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad',0,'day','fixed')`, "items_recurrence_interval_value_check"},
			{"out of range high", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad',1000,'day','fixed')`, "items_recurrence_interval_value_check"},
			{"invalid unit", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad',1,'fortnight','fixed')`, "items_recurrence_interval_unit_check"},
			{"invalid mode", `INSERT INTO items(household_id,subject_id,title,recurrence_interval_value,recurrence_interval_unit,recurrence_mode) VALUES ($1::uuid,$2::uuid,'bad',1,'day','weekly')`, "items_recurrence_mode_check"},
		} {
			_, err := admin.Exec(ctx, tc.statement, hA, sA.ID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != tc.constraint {
				t.Fatalf("%s constraint err=%v pgErr=%+v want=%s", tc.name, err, pgErr, tc.constraint)
			}
		}
	})
	t.Run("household deletion cascades", func(t *testing.T) {
		h := household("cascade")
		s := newSubject(h, "child")
		create(h, s.ID, "cascade item", nil, nil)
		if _, err := admin.Exec(ctx, `DELETE FROM households WHERE id=$1::uuid`, h); err != nil {
			t.Fatal(err)
		}
		if count(h) != 0 {
			t.Fatal("items survived household deletion")
		}
		var completionCount int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM item_completions WHERE household_id=$1::uuid`, h).Scan(&completionCount); err != nil || completionCount != 0 {
			t.Fatalf("completion rows survived household deletion count=%d err=%v", completionCount, err)
		}
	})
}

func TestMigrationUpgradeFromV7PreservesUndoableReceipt(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
	const dbName = "tendo_upgrade_v7_undo_test"
	_, _ = admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	cfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = dbName
	upAdmin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upAdmin.Close()
	appCfg, err := pgxpool.ParseConfig(appURL)
	if err != nil {
		t.Fatal(err)
	}
	appCfg.ConnConfig.Database = dbName
	if _, err = admin.Exec(ctx, `GRANT CONNECT ON DATABASE `+dbName+` TO tendo`); err != nil {
		t.Fatal(err)
	}
	upApp, err := pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upApp.Close()
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `REVOKE UPDATE (undone_at, undone_by_user_id) ON item_completions FROM tendo; ALTER TABLE item_completions DROP CONSTRAINT item_completions_undo_actor_pair_check; ALTER TABLE item_completions DROP COLUMN undone_by_user_id; ALTER TABLE item_completions DROP COLUMN undone_at; DELETE FROM tendo_schema_migrations WHERE version=8`); err != nil {
		t.Fatal(err)
	}
	var hid, sid, actor, iid, cid string
	if err = upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES('v7 undo','UTC') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES('v7_undo_actor') RETURNING id::text`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err = upAdmin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'member')`, actor, hid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','v7 subject') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title,attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,version) VALUES($1::uuid,$2::uuid,'v7 item','2027-01-01',1,'year','fixed',2) RETURNING id::text`, hid, sid).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if err = upAdmin.QueryRow(ctx, `INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,cycle_attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,prior_workflow_state,next_attention_on,item_version_before,idempotency_key,request_fingerprint) VALUES($1::uuid,$2::uuid,'2026-10-06',$3::uuid,'2026-01-01',1,'year','fixed','waiting','2027-01-01',1,'v7-receipt',decode(repeat('01',32),'hex')) RETURNING id::text`, hid, iid, actor).Scan(&cid); err != nil {
		t.Fatal(err)
	}
	if err = database.ValidateSchema(ctx, upAdmin); err == nil {
		t.Fatal("v7 schema accepted")
	}
	if err = database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	var completed string
	var undoneAt *time.Time
	if err = upAdmin.QueryRow(ctx, `SELECT completed_on::text,undone_at FROM item_completions WHERE id=$1::uuid`, cid).Scan(&completed, &undoneAt); err != nil || completed != "2026-10-06" || undoneAt != nil {
		t.Fatalf("upgraded receipt date=%s undone=%v err=%v", completed, undoneAt, err)
	}
	repo := NewRepository(upApp)
	svc := item.NewService(repo, func(_ context.Context, user, house string) (string, error) {
		if user != actor || house != hid {
			return "", item.ErrNotFound
		}
		return "UTC", nil
	}, func(context.Context, string, string, string) (bool, error) { return false, nil }, func() time.Time { return time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC) })
	undone, err := svc.UndoCompletion(ctx, actor, hid, iid, cid, 2)
	if err != nil || undone.UndoneAt == nil {
		t.Fatalf("undo migrated receipt=%+v err=%v", undone, err)
	}
	var attention, state string
	var version int64
	if err = upAdmin.QueryRow(ctx, `SELECT attention_on::text,workflow_state,version FROM items WHERE id=$1::uuid`, iid).Scan(&attention, &state, &version); err != nil || attention != "2026-01-01" || state != "waiting" || version != 3 {
		t.Fatalf("migrated undo state=%s %s %d err=%v", attention, state, version, err)
	}
}

func TestMigrationUpgradeFromVersionFivePreservesData(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
	const dbName = "tendo_upgrade_v5_items_test"
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, cc := context.WithTimeout(context.Background(), 20*time.Second)
		defer cc()
		_, _ = admin.Exec(c, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	}()
	adminCfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	adminCfg.ConnConfig.Database = dbName
	upAdmin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upAdmin.Close()
	appCfg, err := pgxpool.ParseConfig(appURL)
	if err != nil {
		t.Fatal(err)
	}
	appCfg.ConnConfig.Database = dbName
	if _, err := admin.Exec(ctx, `GRANT CONNECT ON DATABASE `+dbName+` TO tendo`); err != nil {
		t.Fatal(err)
	}
	upApp, err := pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upApp.Close()
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := upAdmin.Exec(ctx, `DROP TABLE item_completions; ALTER TABLE items DROP CONSTRAINT items_household_id_subject_id_fkey; ALTER TABLE items DROP CONSTRAINT items_recurrence_all_or_none; ALTER TABLE items DROP COLUMN recurrence_interval_value, DROP COLUMN recurrence_interval_unit, DROP COLUMN recurrence_mode; ALTER TABLE items DROP CONSTRAINT items_household_id_id_key; DROP INDEX items_household_archived_done_id_idx; CREATE INDEX items_household_archived_id_idx ON items(household_id,archived,id); ALTER TABLE items DROP COLUMN done; DELETE FROM tendo_schema_migrations WHERE version IN (6,7,8)`); err != nil {
		t.Fatal(err)
	}
	var hid, sid, iid string
	if err := upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES ('Legacy','Europe/Prague') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err := upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES ($1::uuid,'person','Legacy person') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := upAdmin.QueryRow(ctx, `INSERT INTO items(household_id,subject_id,title,attention_on) VALUES ($1::uuid,$2::uuid,'Legacy item','2030-01-02') RETURNING id::text`, hid, sid).Scan(&iid); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upAdmin); err == nil {
		t.Fatal("v5 schema accepted by v8 application")
	}
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upApp); err != nil {
		t.Fatal(err)
	}
	got, err := NewRepository(upApp).Get(ctx, hid, iid)
	if err != nil || got.Title != "Legacy item" || got.Recurrence != nil || got.AttentionOn == nil || got.AttentionOn.String() != "2030-01-02" {
		t.Fatalf("legacy item=%+v err=%v", got, err)
	}
	var max int64
	if err := upAdmin.QueryRow(ctx, `SELECT max(version) FROM tendo_schema_migrations`).Scan(&max); err != nil || max != 8 {
		t.Fatalf("version=%d err=%v", max, err)
	}
}

func TestMigrationUpgradeFromVersionFourPreservesData(t *testing.T) {
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin := pool(t, ctx, adminURL)
	const dbName = "tendo_upgrade_v4_items_test"
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatal(err)
	}
	defer func() {
		c, cc := context.WithTimeout(context.Background(), 20*time.Second)
		defer cc()
		_, _ = admin.Exec(c, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	}()
	adminCfg, err := pgxpool.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	adminCfg.ConnConfig.Database = dbName
	upAdmin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upAdmin.Close()
	appCfg, err := pgxpool.ParseConfig(appURL)
	if err != nil {
		t.Fatal(err)
	}
	appCfg.ConnConfig.Database = dbName
	if _, err := admin.Exec(ctx, `GRANT CONNECT ON DATABASE `+dbName+` TO tendo`); err != nil {
		t.Fatal(err)
	}
	upApp, err := pgxpool.NewWithConfig(ctx, appCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer upApp.Close()
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := upAdmin.Exec(ctx, `DROP TABLE item_completions; DROP TABLE items; ALTER TABLE subjects DROP CONSTRAINT subjects_household_id_id_key; DELETE FROM tendo_schema_migrations WHERE version IN (5,6,7,8)`); err != nil {
		t.Fatal(err)
	}
	var hid, sid string
	if err := upAdmin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES ('Legacy','Europe/Prague') RETURNING id::text`).Scan(&hid); err != nil {
		t.Fatal(err)
	}
	if err := upAdmin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES ($1::uuid,'person','Legacy person') RETURNING id::text`, hid).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upAdmin); err == nil {
		t.Fatal("v4 database accepted by v8 application")
	}
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	if err := database.ValidateSchema(ctx, upApp); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := upApp.QueryRow(ctx, `SELECT name FROM subjects WHERE id=$1::uuid`, sid).Scan(&name); err != nil || name != "Legacy person" {
		t.Fatalf("legacy data=%q err=%v", name, err)
	}
	created, err := NewRepository(upApp).Create(ctx, hid, item.Draft{SubjectID: sid, Title: "After upgrade"})
	if err != nil || created.Version != 1 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	var max int64
	var dirty bool
	if err := upAdmin.QueryRow(ctx, `SELECT max(version),bool_or(dirty) FROM tendo_schema_migrations`).Scan(&max, &dirty); err != nil || max != 8 || dirty {
		t.Fatalf("version=%d dirty=%v err=%v", max, dirty, err)
	}
}
