package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/bigtcze/tendo/backend/internal/testpostgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrationRecurrenceAnchorBackfill covers migration 021 on a populated
// pre-021 fixture: the conservative default, the exact-recovery branch and the
// ambiguities that must keep the default. It also asserts that attention dates
// and receipts are never rewritten, that Down then Up reproduces the anchors and
// that the new CHECK constraints hold.
func TestMigrationRecurrenceAnchorBackfill(t *testing.T) {
	ctx := context.Background()
	appURL, adminURL := integrationURLs(t)
	admin := pool(t, ctx, adminURL)
	dbName := testpostgres.CreateDatabase(t, ctx, admin)
	upAdmin := poolFromConfig(t, ctx, testpostgres.PoolConfigForDatabase(t, adminURL, dbName))
	upApp := poolFromConfig(t, ctx, testpostgres.PoolConfigForDatabase(t, appURL, dbName))
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatal(err)
	}
	var household, actor, subjectID string
	mustScan(t, ctx, upAdmin, &household, `INSERT INTO households(name,timezone) VALUES('anchor backfill','UTC') RETURNING id::text`)
	mustScan(t, ctx, upAdmin, &actor, `INSERT INTO user_accounts(login) VALUES('anchor backfill actor') RETURNING id::text`)
	if _, err := upAdmin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'admin')`, actor, household); err != nil {
		t.Fatal(err)
	}
	mustScan(t, ctx, upAdmin, &subjectID, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person','anchor backfill') RETURNING id::text`, household)

	// Rewind to the exact pre-021 schema and seed the populated fixture there.
	if _, err := upAdmin.Exec(ctx, testpostgres.RevertVersion21SQL); err != nil {
		t.Fatal(err)
	}
	insertItem := func(title, attention string, version int64, interval *int, unit, mode string) string {
		t.Helper()
		var intervalValue, attentionValue, unitValue, modeValue any
		if interval != nil {
			intervalValue = *interval
		}
		if attention != "" {
			attentionValue = attention
		}
		if unit != "" {
			unitValue, modeValue = unit, mode
		}
		var id string
		mustScan(t, ctx, upAdmin, &id,
			`INSERT INTO items(household_id,subject_id,title,attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,version)
			 VALUES($1::uuid,$2::uuid,$3,$4::date,$5,$6,$7,$8) RETURNING id::text`,
			household, subjectID, title, attentionValue, intervalValue, unitValue, modeValue, version)
		return id
	}
	insertReceipt := func(itemID, unit, completedOn, cycle, next, key string, before int64, undone bool) {
		t.Helper()
		var cycleValue, undoneAt, undoneBy any
		if cycle != "" {
			cycleValue = cycle
		}
		if undone {
			undoneAt, undoneBy = time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC), actor
		}
		if _, err := upAdmin.Exec(ctx,
			`INSERT INTO item_completions(household_id,item_id,completed_on,completed_by_user_id,cycle_attention_on,recurrence_interval_value,recurrence_interval_unit,recurrence_mode,prior_workflow_state,next_attention_on,item_version_before,idempotency_key,request_fingerprint,undone_at,undone_by_user_id)
			 VALUES($1::uuid,$2::uuid,$3::date,$4::uuid,$5::date,1,$11,'fixed','open',$6::date,$7,$8,decode(repeat('ab',32),'hex'),$9,$10::uuid)`,
			household, itemID, completedOn, actor, cycleValue, next, before, key, undoneAt, undoneBy, unit); err != nil {
			t.Fatal(err)
		}
	}
	month := 1
	// Exact recovery: full receipt continuity and a provable earlier anchor.
	exact := insertItem("exact", "2026-04-30", 4, &month, "month", "fixed")
	insertReceipt(exact, "month", "2026-02-01", "2026-01-31", "2026-02-28", "exact-1", 1, false)
	insertReceipt(exact, "month", "2026-03-01", "2026-02-28", "2026-03-31", "exact-2", 2, false)
	insertReceipt(exact, "month", "2026-04-01", "2026-03-31", "2026-04-30", "exact-3", 3, false)
	// Exact recovery from the first completed_on when the first cycle date is NULL.
	initialized := insertItem("initialized", "2026-04-30", 4, &month, "month", "fixed")
	insertReceipt(initialized, "month", "2026-01-31", "", "2026-02-28", "init-1", 1, false)
	insertReceipt(initialized, "month", "2026-03-01", "2026-02-28", "2026-03-31", "init-2", 2, false)
	insertReceipt(initialized, "month", "2026-04-01", "2026-03-31", "2026-04-30", "init-3", 3, false)
	// Ambiguous: version gap.
	gap := insertItem("gap", "2026-04-30", 4, &month, "month", "fixed")
	insertReceipt(gap, "month", "2026-02-01", "2026-01-31", "2026-02-28", "gap-1", 1, false)
	insertReceipt(gap, "month", "2026-03-01", "2026-03-31", "2026-04-30", "gap-3", 3, false)
	// Ambiguous: an undone receipt.
	undone := insertItem("undone", "2026-04-30", 4, &month, "month", "fixed")
	insertReceipt(undone, "month", "2026-02-01", "2026-01-31", "2026-02-28", "undone-1", 1, false)
	insertReceipt(undone, "month", "2026-03-01", "2026-02-28", "2026-03-31", "undone-2", 2, true)
	insertReceipt(undone, "month", "2026-04-01", "2026-03-31", "2026-04-30", "undone-3", 3, false)
	// Ambiguous: the last receipt no longer matches the stored attention date.
	edited := insertItem("edited", "2026-04-30", 4, &month, "month", "fixed")
	insertReceipt(edited, "month", "2026-02-01", "2026-01-31", "2026-02-28", "edited-1", 1, false)
	insertReceipt(edited, "month", "2026-03-01", "2026-02-28", "2026-03-29", "edited-2", 2, false)
	// Ambiguous: receipts start after version 1 (an edit before the first completion).
	lateStart := insertItem("late start", "2026-04-30", 5, &month, "month", "fixed")
	insertReceipt(lateStart, "month", "2026-02-01", "2026-01-31", "2026-02-28", "late-2", 2, false)
	insertReceipt(lateStart, "month", "2026-03-01", "2026-02-28", "2026-03-31", "late-3", 3, false)
	insertReceipt(lateStart, "month", "2026-04-01", "2026-03-31", "2026-04-30", "late-4", 4, false)
	// Ambiguous: an edit after the last completion kept the date but bumped the version.
	editedAfter := insertItem("edited after", "2026-04-30", 5, &month, "month", "fixed")
	insertReceipt(editedAfter, "month", "2026-02-01", "2026-01-31", "2026-02-28", "after-1", 1, false)
	insertReceipt(editedAfter, "month", "2026-03-01", "2026-02-28", "2026-03-31", "after-2", 2, false)
	insertReceipt(editedAfter, "month", "2026-04-01", "2026-03-31", "2026-04-30", "after-3", 3, false)
	// Ambiguous: one receipt's next date does not link to the following cycle date.
	brokenLink := insertItem("broken link", "2026-04-30", 4, &month, "month", "fixed")
	insertReceipt(brokenLink, "month", "2026-02-01", "2026-01-31", "2026-02-28", "link-1", 1, false)
	insertReceipt(brokenLink, "month", "2026-03-01", "2026-02-27", "2026-03-31", "link-2", 2, false)
	insertReceipt(brokenLink, "month", "2026-04-01", "2026-03-31", "2026-04-30", "link-3", 3, false)
	// Non-recoverable schedules keep the conservative default.
	fluid := insertItem("fluid", "2026-04-30", 4, &month, "month", "after_completion")
	insertReceipt(fluid, "month", "2026-02-01", "2026-01-31", "2026-02-28", "fluid-1", 1, false)
	insertItem("daily", "2026-04-30", 1, &month, "day", "fixed")
	insertItem("one-off", "2026-04-30", 1, nil, "", "")
	insertItem("null date", "", 1, &month, "month", "fixed")
	year := 1
	leap := insertItem("yearly leap", "2026-02-28", 3, &year, "year", "fixed")
	insertReceipt(leap, "year", "2025-02-01", "2024-02-29", "2025-02-28", "leap-1", 1, false)
	insertReceipt(leap, "year", "2026-02-01", "2025-02-28", "2026-02-28", "leap-2", 2, false)

	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatalf("migration 021: %v", err)
	}
	want := map[string]*int{
		"exact": dayPtr(31), "initialized": dayPtr(31), "yearly leap": dayPtr(29),
		"gap": dayPtr(30), "undone": dayPtr(30), "edited": dayPtr(30),
		"late start": dayPtr(30), "edited after": dayPtr(30), "broken link": dayPtr(30),
		"fluid": dayPtr(30), "daily": dayPtr(30), "one-off": dayPtr(30),
		"null date": nil,
	}
	assertAnchors := func(stage string) {
		t.Helper()
		rows, err := upAdmin.Query(ctx, `SELECT title, coalesce(attention_on::text,''), attention_anchor_day FROM items WHERE household_id=$1::uuid`, household)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		seen := 0
		for rows.Next() {
			var title, attention string
			var anchor *int
			if err := rows.Scan(&title, &attention, &anchor); err != nil {
				t.Fatal(err)
			}
			seen++
			expected, ok := want[title]
			if !ok {
				t.Fatalf("%s: unexpected item %q", stage, title)
			}
			if (expected == nil) != (anchor == nil) || (expected != nil && *anchor != *expected) {
				t.Fatalf("%s: %s anchor=%s want=%s", stage, title, anchorText(anchor), anchorText(expected))
			}
			if title == "null date" {
				if attention != "" {
					t.Fatalf("%s: null date item attention=%q", stage, attention)
				}
				continue
			}
			if attention == "" {
				t.Fatalf("%s: %s lost its attention date", stage, title)
			}
			if anchor != nil && attention != "" && attention[8:] != fmt.Sprintf("%02d", *anchor) &&
				title != "exact" && title != "initialized" && title != "yearly leap" {
				t.Fatalf("%s: %s anchor=%d does not match attention %s", stage, title, *anchor, attention)
			}
		}
		if seen != len(want) {
			t.Fatalf("%s: items=%d want %d", stage, seen, len(want))
		}
	}
	assertAnchors("up")
	var receipts, anchoredReceipts int
	if err := upAdmin.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE cycle_anchor_day IS NOT NULL OR next_anchor_day IS NOT NULL) FROM item_completions WHERE household_id=$1::uuid`, household).Scan(&receipts, &anchoredReceipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 25 || anchoredReceipts != 0 {
		t.Fatalf("receipts=%d with anchors=%d, want 25 unchanged receipts without anchors", receipts, anchoredReceipts)
	}

	// Down is lossy and a repeated Up must reproduce the same anchors.
	if _, err := upAdmin.Exec(ctx, testpostgres.RevertVersion21SQL); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, upAdmin); err != nil {
		t.Fatalf("second migration 021: %v", err)
	}
	assertAnchors("second up")

	// Runtime role can write the anchor, and the CHECKs reject inconsistent rows.
	if _, err := upApp.Exec(ctx, `UPDATE items SET attention_anchor_day=31 WHERE id=$1::uuid`, exact); err != nil {
		t.Fatalf("runtime anchor update: %v", err)
	}
	mustViolate := func(name, sql string, args ...any) {
		t.Helper()
		if _, err := upAdmin.Exec(ctx, sql, args...); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	mustAccept := func(name, sql string, args ...any) {
		t.Helper()
		if _, err := upAdmin.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s rejected: %v", name, err)
		}
	}
	const insertColumns = `INSERT INTO items(household_id,subject_id,title,attention_on,attention_anchor_day) VALUES($1::uuid,$2::uuid,$3,$4::date,$5)`
	mustViolate("date without anchor", insertColumns, household, subjectID, "check-missing-anchor", "2026-05-10", nil)
	mustViolate("anchor without date", insertColumns, household, subjectID, "check-missing-date", nil, 5)
	mustViolate("anchor out of range", insertColumns, household, subjectID, "check-range", "2026-02-10", 32)
	mustViolate("anchor above a non-month-end day", insertColumns, household, subjectID, "check-clamp", "2026-02-10", 11)
	mustAccept("anchor equal to day", insertColumns, household, subjectID, "check-equal", "2026-02-10", 10)
	mustAccept("clamped month end", insertColumns, household, subjectID, "check-clamp-ok", "2026-04-30", 31)
	mustViolate("receipt cycle anchor out of range", `UPDATE item_completions SET cycle_anchor_day=32 WHERE item_id=$1::uuid`, exact)
	mustViolate("receipt next anchor zero", `UPDATE item_completions SET next_anchor_day=0 WHERE item_id=$1::uuid`, exact)
}

func poolFromConfig(t *testing.T, ctx context.Context, config *pgxpool.Config) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func mustScan(t *testing.T, ctx context.Context, db *pgxpool.Pool, into *string, sql string, args ...any) {
	t.Helper()
	if err := db.QueryRow(ctx, sql, args...).Scan(into); err != nil {
		t.Fatal(err)
	}
}

func dayPtr(day int) *int { return &day }

func anchorText(anchor *int) string {
	if anchor == nil {
		return "NULL"
	}
	return fmt.Sprint(*anchor)
}

// anchorFixture creates an item due on 2026-01-31 with anchor 31 and returns a
// service whose clock sits on the supplied instant.
func anchorFixture(t *testing.T, ctx context.Context, now time.Time) (*completionFixture, *item.Service, *pgxpool.Pool, string) {
	t.Helper()
	appURL, adminURL := integrationURLs(t)
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := newCompletionFixture(t, ctx, admin, app)
	policy := &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}
	created := f.create(t, "anchor persistence", &f.today, policy)
	if _, err := admin.Exec(ctx, `UPDATE items SET attention_on='2026-01-31',attention_anchor_day=31 WHERE id=$1::uuid`, created.ID); err != nil {
		t.Fatal(err)
	}
	service := item.NewService(f.repo, func(context.Context, string, string) (string, error) { return "UTC", nil }, nil, nil, func() time.Time { return now })
	return f, service, admin, created.ID
}

// TestCompletionPersistsAnchorsAndUndoRestoresThem checks both anchor columns and
// the item anchor in one transaction, and that undo restores the snapshot.
func TestCompletionPersistsAnchorsAndUndoRestoresThem(t *testing.T) {
	ctx := context.Background()
	f, service, admin, createdID := anchorFixture(t, ctx, time.Date(2026, time.February, 1, 12, 0, 0, 0, time.UTC))
	created, err := f.repo.Get(ctx, f.household, createdID)
	if err != nil {
		t.Fatal(err)
	}
	completed := "2026-02-01"
	receipt, _, err := service.Complete(ctx, f.actor, f.household, created.ID, created.Version, "anchor-key", item.CompletionRequest{CompletedOn: &completed})
	if err != nil {
		t.Fatal(err)
	}
	var itemAttention, storedCycle, storedNext string
	var itemAnchor, cycleAnchor, nextAnchor *int
	if err := admin.QueryRow(ctx, `SELECT attention_on::text,attention_anchor_day FROM items WHERE id=$1::uuid`, created.ID).Scan(&itemAttention, &itemAnchor); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT coalesce(cycle_attention_on::text,''),cycle_anchor_day,coalesce(next_attention_on::text,''),next_anchor_day FROM item_completions WHERE id=$1::uuid`, receipt.ID).Scan(&storedCycle, &cycleAnchor, &storedNext, &nextAnchor); err != nil {
		t.Fatal(err)
	}
	if itemAttention != "2026-02-28" || itemAnchor == nil || *itemAnchor != 31 {
		t.Fatalf("item attention=%s anchor=%v", itemAttention, itemAnchor)
	}
	if storedCycle != "2026-01-31" || cycleAnchor == nil || *cycleAnchor != 31 || storedNext != "2026-02-28" || nextAnchor == nil || *nextAnchor != 31 {
		t.Fatalf("receipt cycle=%s/%v next=%s/%v", storedCycle, cycleAnchor, storedNext, nextAnchor)
	}
	if _, err := service.UndoCompletion(ctx, f.actor, f.household, created.ID, receipt.ID, created.Version+1); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `SELECT attention_on::text,attention_anchor_day FROM items WHERE id=$1::uuid`, created.ID).Scan(&itemAttention, &itemAnchor); err != nil {
		t.Fatal(err)
	}
	if itemAttention != "2026-01-31" || itemAnchor == nil || *itemAnchor != 31 {
		t.Fatalf("restored attention=%s anchor=%v", itemAttention, itemAnchor)
	}
}

// TestUndoCompletionSupersededLeavesRowUnchanged proves the 409 path persists
// nothing: the item row and the receipt keep their previous state.
func TestUndoCompletionSupersededLeavesRowUnchanged(t *testing.T) {
	ctx := context.Background()
	f, service, admin, createdID := anchorFixture(t, ctx, time.Date(2026, time.February, 1, 12, 0, 0, 0, time.UTC))
	created, err := f.repo.Get(ctx, f.household, createdID)
	if err != nil {
		t.Fatal(err)
	}
	completed := "2026-02-01"
	receipt, _, err := service.Complete(ctx, f.actor, f.household, created.ID, created.Version, "superseded-key", item.CompletionRequest{CompletedOn: &completed})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Update(ctx, f.actor, f.household, created.ID, created.Version+1, item.Patch{AttentionOn: item.Some("2030-01-05")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.UndoCompletion(ctx, f.actor, f.household, created.ID, receipt.ID, updated.Version); !errors.Is(err, item.ErrCompletionSuperseded) {
		t.Fatalf("undo err=%v, want superseded", err)
	}
	var attention string
	var anchor *int
	var version int64
	if err := admin.QueryRow(ctx, `SELECT attention_on::text,attention_anchor_day,version FROM items WHERE id=$1::uuid`, created.ID).Scan(&attention, &anchor, &version); err != nil {
		t.Fatal(err)
	}
	if attention != "2030-01-05" || anchor == nil || *anchor != 5 || version != updated.Version {
		t.Fatalf("superseded undo mutated item: attention=%s anchor=%v version=%d", attention, anchor, version)
	}
	var undone *time.Time
	if err := admin.QueryRow(ctx, `SELECT undone_at FROM item_completions WHERE id=$1::uuid`, receipt.ID).Scan(&undone); err != nil {
		t.Fatal(err)
	}
	if undone != nil {
		t.Fatalf("superseded undo marked receipt undone at %v", undone)
	}
}
