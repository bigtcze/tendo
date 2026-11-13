package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/database"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/bigtcze/tendo/backend/internal/schedule/scheduletest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file is the V5-04 service-level recurrence gate on real PostgreSQL. Every
// test drives item.Service against a Europe/Prague household with an injected
// clock and asserts both the returned receipts and the committed item and ledger
// rows, so a service or storage regression cannot pass on returned values alone.

const recurrenceGateRacers = 8

type recurrenceGateFixture struct {
	ctx                       context.Context
	admin, app                *pgxpool.Pool
	repo                      *Repository
	household, actor, subject string
	now                       time.Time
	svc                       *item.Service
}

func newRecurrenceGateFixture(t *testing.T) *recurrenceGateFixture {
	t.Helper()
	appURL, adminURL := integrationURLs(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	admin, app := pool(t, ctx, adminURL), pool(t, ctx, appURL)
	if err := database.Migrate(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := &recurrenceGateFixture{ctx: ctx, admin: admin, app: app, repo: NewRepository(app)}
	seq := attentionFixtureSeq.Add(1)
	if err := admin.QueryRow(ctx, `INSERT INTO households(name,timezone) VALUES($1,'Europe/Prague') RETURNING id::text`,
		fmt.Sprintf("recurrence-gate-%d", seq)).Scan(&f.household); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO user_accounts(login) VALUES($1) RETURNING id::text`,
		fmt.Sprintf("recurrence-gate-actor-%d", seq)).Scan(&f.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO household_memberships(user_id,household_id,role) VALUES($1::uuid,$2::uuid,'admin')`, f.actor, f.household); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, `INSERT INTO subjects(household_id,type,name) VALUES($1::uuid,'person',$2) RETURNING id::text`,
		f.household, fmt.Sprintf("recurrence-gate-subject-%d", seq)).Scan(&f.subject); err != nil {
		t.Fatal(err)
	}
	houses := func(_ context.Context, user, household string) (string, error) {
		var n int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM household_memberships WHERE user_id=$1::uuid AND household_id=$2::uuid`, user, household).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return "", item.ErrNotFound
		}
		return "Europe/Prague", nil
	}
	f.now = time.Date(2027, time.January, 31, 12, 0, 0, 0, time.UTC)
	f.svc = item.NewService(f.repo, houses, nil, nil, func() time.Time { return f.now })
	return f
}

func monthlyFixedGatePolicy() *schedule.Policy {
	return &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}
}

func yearlyFixedGatePolicy() *schedule.Policy {
	return &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitYear}, Mode: schedule.ModeFixed}
}

// setNow moves the injected clock to noon UTC on the given date, which is the same
// Europe/Prague calendar date in both winter and summer time.
func (f *recurrenceGateFixture) setNow(t *testing.T, date string) {
	t.Helper()
	d := scheduletest.Date(date)
	f.now = time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, time.UTC)
}

func (f *recurrenceGateFixture) create(t *testing.T, title, due string, policy *schedule.Policy) item.Item {
	t.Helper()
	d := scheduletest.Date(due)
	created, err := createForTest(f.ctx, f.repo, f.household, f.actor, item.Draft{
		SubjectID: f.subject, Title: title, AttentionOn: &d, Recurrence: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func (f *recurrenceGateFixture) complete(t *testing.T, itemID string, version int64, key string) item.Completion {
	t.Helper()
	receipt, replayed, err := f.svc.Complete(f.ctx, f.actor, f.household, itemID, version, key, item.CompletionRequest{})
	if err != nil || replayed {
		t.Fatalf("complete %s version %d key %s: replayed=%v err=%v", itemID, version, key, replayed, err)
	}
	return receipt
}

func (f *recurrenceGateFixture) undo(t *testing.T, itemID, receiptID string, version int64) item.Completion {
	t.Helper()
	receipt, err := f.svc.UndoCompletion(f.ctx, f.actor, f.household, itemID, receiptID, version)
	if err != nil {
		t.Fatalf("undo %s receipt %s version %d: %v", itemID, receiptID, version, err)
	}
	return receipt
}

type itemScheduleRow struct {
	attention string
	anchor    *int
	done      bool
	version   int64
}

func (f *recurrenceGateFixture) readItem(t *testing.T, itemID string) itemScheduleRow {
	t.Helper()
	var row itemScheduleRow
	if err := f.admin.QueryRow(f.ctx,
		`SELECT coalesce(attention_on::text,''), attention_anchor_day, done, version FROM items WHERE id=$1::uuid`,
		itemID).Scan(&row.attention, &row.anchor, &row.done, &row.version); err != nil {
		t.Fatal(err)
	}
	return row
}

func (f *recurrenceGateFixture) assertItem(t *testing.T, itemID, attention string, anchor int, done bool, version int64) {
	t.Helper()
	row := f.readItem(t, itemID)
	if row.attention != attention || row.anchor == nil || *row.anchor != anchor || row.done != done || row.version != version {
		t.Fatalf("item %s attention=%q anchor=%s done=%v version=%d, want %q anchor %d done=%v version %d",
			itemID, row.attention, anchorText(row.anchor), row.done, row.version, attention, anchor, done, version)
	}
}

type receiptScheduleRow struct {
	cycle, next             string
	cycleAnchor, nextAnchor *int
	undone                  bool
}

func (f *recurrenceGateFixture) readReceipt(t *testing.T, receiptID string) receiptScheduleRow {
	t.Helper()
	var row receiptScheduleRow
	if err := f.admin.QueryRow(f.ctx,
		`SELECT coalesce(cycle_attention_on::text,''), cycle_anchor_day, coalesce(next_attention_on::text,''), next_anchor_day, undone_at IS NOT NULL
		 FROM item_completions WHERE id=$1::uuid`, receiptID).
		Scan(&row.cycle, &row.cycleAnchor, &row.next, &row.nextAnchor, &row.undone); err != nil {
		t.Fatal(err)
	}
	return row
}

func (f *recurrenceGateFixture) assertReceipt(t *testing.T, receiptID, cycle string, cycleAnchor int, next string, nextAnchor int, undone bool) {
	t.Helper()
	row := f.readReceipt(t, receiptID)
	if row.cycle != cycle || row.cycleAnchor == nil || *row.cycleAnchor != cycleAnchor ||
		row.next != next || row.nextAnchor == nil || *row.nextAnchor != nextAnchor || row.undone != undone {
		t.Fatalf("receipt %s cycle=%q/%s next=%q/%s undone=%v, want %q/%d %q/%d undone=%v",
			receiptID, row.cycle, anchorText(row.cycleAnchor), row.next, anchorText(row.nextAnchor), row.undone,
			cycle, cycleAnchor, next, nextAnchor, undone)
	}
}

func (f *recurrenceGateFixture) ledgerCounts(t *testing.T, itemID string) (total, undone int) {
	t.Helper()
	if err := f.admin.QueryRow(f.ctx,
		`SELECT count(*), count(*) FILTER (WHERE undone_at IS NOT NULL) FROM item_completions WHERE item_id=$1::uuid`,
		itemID).Scan(&total, &undone); err != nil {
		t.Fatal(err)
	}
	return total, undone
}

// assertReceiptSchedule checks the returned receipt's recurrence snapshot and next
// cycle, which the caller then compares with the stored row via assertReceipt.
func assertReceiptSchedule(t *testing.T, receipt item.Completion, cycle string, cycleAnchor int, next string, nextAnchor int) {
	t.Helper()
	if receipt.CycleAttentionOn == nil || receipt.CycleAttentionOn.String() != cycle {
		t.Fatalf("receipt %s cycle attention=%v, want %s", receipt.ID, receipt.CycleAttentionOn, cycle)
	}
	if receipt.NextAttentionOn == nil || receipt.NextAttentionOn.String() != next {
		t.Fatalf("receipt %s next attention=%v, want %s", receipt.ID, receipt.NextAttentionOn, next)
	}
	if receipt.CycleAnchorDay == nil || *receipt.CycleAnchorDay != cycleAnchor ||
		receipt.NextAnchorDay == nil || *receipt.NextAnchorDay != nextAnchor {
		t.Fatalf("receipt %s anchors=%s/%s, want %d/%d", receipt.ID,
			anchorText(receipt.CycleAnchorDay), anchorText(receipt.NextAnchorDay), cycleAnchor, nextAnchor)
	}
}

type completionOutcome struct {
	receipt  item.Completion
	replayed bool
	err      error
}

// raceCompletions releases n goroutines together on the same item and returns their
// outcomes. Every goroutine reads the fixture clock concurrently but nothing writes
// it during the race.
func (f *recurrenceGateFixture) raceCompletions(itemID string, version int64, keys []string, bodies []item.CompletionRequest) []completionOutcome {
	outcomes := make([]completionOutcome, len(keys))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			outcomes[i].receipt, outcomes[i].replayed, outcomes[i].err =
				f.svc.Complete(f.ctx, f.actor, f.household, itemID, version, keys[i], bodies[i])
		}(i)
	}
	close(start)
	wg.Wait()
	return outcomes
}

// TestRecurrenceGateConcurrentSameKeyDoubleSubmit releases identical double
// submits with the same idempotency key and asserts exactly one ledger row, one
// replay=false, one schedule transition and the clamped anchor persisted.
func TestRecurrenceGateConcurrentSameKeyDoubleSubmit(t *testing.T) {
	f := newRecurrenceGateFixture(t)
	f.setNow(t, "2027-01-31")
	for iteration := 0; iteration < concurrencyIterations; iteration++ {
		created := f.create(t, fmt.Sprintf("gate-same-key-%d", iteration), "2027-01-31", monthlyFixedGatePolicy())
		keys := make([]string, recurrenceGateRacers)
		bodies := make([]item.CompletionRequest, recurrenceGateRacers)
		for i := range keys {
			keys[i] = fmt.Sprintf("gate-same-key-%d", iteration)
		}
		outcomes := f.raceCompletions(created.ID, created.Version, keys, bodies)
		winnerID := ""
		applied := 0
		for i, outcome := range outcomes {
			if outcome.err != nil {
				t.Fatalf("iteration %d racer %d: err=%v", iteration, i, outcome.err)
			}
			if !outcome.replayed {
				applied++
				winnerID = outcome.receipt.ID
			}
			if outcome.receipt.ID == "" {
				t.Fatalf("iteration %d racer %d: empty receipt", iteration, i)
			}
			if winnerID != "" && outcome.receipt.ID != winnerID {
				t.Fatalf("iteration %d racer %d: receipt %s differs from %s", iteration, i, outcome.receipt.ID, winnerID)
			}
		}
		if applied != 1 {
			t.Fatalf("iteration %d: %d racers advanced the schedule, want exactly 1", iteration, applied)
		}
		for i := range outcomes {
			if outcomes[i].receipt.ID != winnerID {
				t.Fatalf("iteration %d racer %d: receipt %s, want %s", iteration, i, outcomes[i].receipt.ID, winnerID)
			}
			assertReceiptSchedule(t, outcomes[i].receipt, "2027-01-31", 31, "2027-02-28", 31)
		}
		f.assertItem(t, created.ID, "2027-02-28", 31, false, created.Version+1)
		f.assertReceipt(t, winnerID, "2027-01-31", 31, "2027-02-28", 31, false)
		if total, undone := f.ledgerCounts(t, created.ID); total != 1 || undone != 0 {
			t.Fatalf("iteration %d: ledger total=%d undone=%d, want 1/0", iteration, total, undone)
		}
	}
}

// TestRecurrenceGateConcurrentDifferentKeysAtSameVersion races distinct
// idempotency keys at one version: one winner, the rest ErrVersionMismatch, and
// only one ledger row and schedule transition.
func TestRecurrenceGateConcurrentDifferentKeysAtSameVersion(t *testing.T) {
	f := newRecurrenceGateFixture(t)
	f.setNow(t, "2027-01-31")
	for iteration := 0; iteration < concurrencyIterations; iteration++ {
		created := f.create(t, fmt.Sprintf("gate-distinct-keys-%d", iteration), "2027-01-31", monthlyFixedGatePolicy())
		keys := make([]string, recurrenceGateRacers)
		bodies := make([]item.CompletionRequest, recurrenceGateRacers)
		for i := range keys {
			keys[i] = fmt.Sprintf("gate-distinct-%d-%d", iteration, i)
		}
		outcomes := f.raceCompletions(created.ID, created.Version, keys, bodies)
		winner := -1
		for i, outcome := range outcomes {
			if outcome.err != nil {
				if !errors.Is(outcome.err, item.ErrVersionMismatch) {
					t.Fatalf("iteration %d racer %d: err=%v, want version mismatch", iteration, i, outcome.err)
				}
				continue
			}
			if winner != -1 {
				t.Fatalf("iteration %d: racers %d and %d both committed", iteration, winner, i)
			}
			winner = i
		}
		if winner == -1 {
			t.Fatalf("iteration %d: no racer committed", iteration)
		}
		f.assertItem(t, created.ID, "2027-02-28", 31, false, created.Version+1)
		f.assertReceipt(t, outcomes[winner].receipt.ID, "2027-01-31", 31, "2027-02-28", 31, false)
		if total, undone := f.ledgerCounts(t, created.ID); total != 1 || undone != 0 {
			t.Fatalf("iteration %d: ledger total=%d undone=%d, want 1/0", iteration, total, undone)
		}
	}
}

// TestRecurrenceGateMonthlyClampUndoChain drives 2027-01-31 -> 2027-02-28 ->
// 2027-03-31, undoes, completes again with a new key and undoes back to the start,
// asserting the persisted row and ledger after every step.
func TestRecurrenceGateMonthlyClampUndoChain(t *testing.T) {
	f := newRecurrenceGateFixture(t)
	f.setNow(t, "2027-01-31")
	created := f.create(t, "gate-monthly-undo", "2027-01-31", monthlyFixedGatePolicy())

	first := f.complete(t, created.ID, created.Version, "gate-monthly-1")
	assertReceiptSchedule(t, first, "2027-01-31", 31, "2027-02-28", 31)
	f.assertItem(t, created.ID, "2027-02-28", 31, false, 2)
	f.assertReceipt(t, first.ID, "2027-01-31", 31, "2027-02-28", 31, false)

	f.setNow(t, "2027-02-28")
	second := f.complete(t, created.ID, 2, "gate-monthly-2")
	assertReceiptSchedule(t, second, "2027-02-28", 31, "2027-03-31", 31)
	f.assertItem(t, created.ID, "2027-03-31", 31, false, 3)

	f.undo(t, created.ID, second.ID, 3)
	f.assertItem(t, created.ID, "2027-02-28", 31, false, 4)
	f.assertReceipt(t, second.ID, "2027-02-28", 31, "2027-03-31", 31, true)

	third := f.complete(t, created.ID, 4, "gate-monthly-3")
	assertReceiptSchedule(t, third, "2027-02-28", 31, "2027-03-31", 31)
	if third.NextAttentionOn == nil || third.NextAttentionOn.String() != "2027-03-31" {
		t.Fatalf("completion after undo drifted: %v", third.NextAttentionOn)
	}
	f.assertItem(t, created.ID, "2027-03-31", 31, false, 5)

	f.undo(t, created.ID, third.ID, 5)
	f.assertItem(t, created.ID, "2027-02-28", 31, false, 6)
	f.undo(t, created.ID, first.ID, 6)
	f.assertItem(t, created.ID, "2027-01-31", 31, false, 7)
	if total, undone := f.ledgerCounts(t, created.ID); total != 3 || undone != 3 {
		t.Fatalf("monthly undo chain ledger total=%d undone=%d, want 3/3", total, undone)
	}
	for _, receiptID := range []string{first.ID, second.ID, third.ID} {
		row := f.readReceipt(t, receiptID)
		if !row.undone {
			t.Fatalf("receipt %s not undone after full chain", receiptID)
		}
	}
}

// TestRecurrenceGateYearlyFeb29UndoChain restores the leap-day anchor after undo
// and then chains 2028-02-29 through 2032-02-29 on real rows.
func TestRecurrenceGateYearlyFeb29UndoChain(t *testing.T) {
	f := newRecurrenceGateFixture(t)
	f.setNow(t, "2028-02-29")
	created := f.create(t, "gate-yearly-leap-undo", "2028-02-29", yearlyFixedGatePolicy())

	first := f.complete(t, created.ID, created.Version, "gate-yearly-1")
	assertReceiptSchedule(t, first, "2028-02-29", 29, "2029-02-28", 29)
	f.assertItem(t, created.ID, "2029-02-28", 29, false, 2)

	f.undo(t, created.ID, first.ID, 2)
	f.assertItem(t, created.ID, "2028-02-29", 29, false, 3)
	f.assertReceipt(t, first.ID, "2028-02-29", 29, "2029-02-28", 29, true)

	steps := []struct{ clock, want, key string }{
		{"2028-02-29", "2029-02-28", "gate-yearly-2"},
		{"2029-02-28", "2030-02-28", "gate-yearly-3"},
		{"2030-02-28", "2031-02-28", "gate-yearly-4"},
		{"2031-02-28", "2032-02-29", "gate-yearly-5"},
	}
	version := int64(3)
	for _, step := range steps {
		f.setNow(t, step.clock)
		receipt := f.complete(t, created.ID, version, step.key)
		assertReceiptSchedule(t, receipt, step.clock, 29, step.want, 29)
		version++
		f.assertItem(t, created.ID, step.want, 29, false, version)
	}
	if total, undone := f.ledgerCounts(t, created.ID); total != 5 || undone != 1 {
		t.Fatalf("yearly leap chain ledger total=%d undone=%d, want 5/1", total, undone)
	}
}

// TestRecurrenceGateReplayAfterUndoAndMidnight checks the documented replay
// semantics: the original receipt is returned (reporting undone), the item does not
// advance, and a replay across local midnight after the clock moves is a no-op.
func TestRecurrenceGateReplayAfterUndoAndMidnight(t *testing.T) {
	f := newRecurrenceGateFixture(t)
	f.setNow(t, "2027-01-31")

	created := f.create(t, "gate-replay-undo", "2027-01-31", monthlyFixedGatePolicy())
	receipt := f.complete(t, created.ID, created.Version, "gate-replay-undo")
	f.undo(t, created.ID, receipt.ID, 2)
	f.assertItem(t, created.ID, "2027-01-31", 31, false, 3)

	replayed, isReplay, err := f.svc.Complete(f.ctx, f.actor, f.household, created.ID, 3, "gate-replay-undo", item.CompletionRequest{})
	if err != nil || !isReplay {
		t.Fatalf("replay after undo: replayed=%v err=%v", isReplay, err)
	}
	if replayed.ID != receipt.ID || replayed.UndoneAt == nil {
		t.Fatalf("replay after undo returned %s undone=%v, want %s undone", replayed.ID, replayed.UndoneAt, receipt.ID)
	}
	f.assertItem(t, created.ID, "2027-01-31", 31, false, 3)
	if total, undone := f.ledgerCounts(t, created.ID); total != 1 || undone != 1 {
		t.Fatalf("replay after undo ledger total=%d undone=%d, want 1/1", total, undone)
	}

	midnight := f.create(t, "gate-replay-midnight", "2027-01-31", monthlyFixedGatePolicy())
	first := f.complete(t, midnight.ID, midnight.Version, "gate-replay-midnight")
	f.setNow(t, "2027-02-01")
	replayed, isReplay, err = f.svc.Complete(f.ctx, f.actor, f.household, midnight.ID, 2, "gate-replay-midnight", item.CompletionRequest{})
	if err != nil || !isReplay || replayed.ID != first.ID {
		t.Fatalf("midnight replay: id=%s replayed=%v err=%v, want %s replayed", replayed.ID, isReplay, err, first.ID)
	}
	f.assertItem(t, midnight.ID, "2027-02-28", 31, false, 2)
	if total, undone := f.ledgerCounts(t, midnight.ID); total != 1 || undone != 0 {
		t.Fatalf("midnight replay ledger total=%d undone=%d, want 1/0", total, undone)
	}
}

// TestRecurrenceGateUndoRacesAttentionEdit races undo with a concurrent attention
// edit: exactly one wins, and either outcome leaves a consistent item row, receipt
// and ledger.
func TestRecurrenceGateUndoRacesAttentionEdit(t *testing.T) {
	f := newRecurrenceGateFixture(t)
	f.setNow(t, "2027-01-31")
	for iteration := 0; iteration < 10; iteration++ {
		created := f.create(t, fmt.Sprintf("gate-undo-edit-%d", iteration), "2027-01-31", monthlyFixedGatePolicy())
		receipt := f.complete(t, created.ID, created.Version, fmt.Sprintf("gate-undo-edit-%d", iteration))
		var undoErr, editErr error
		runTogether(t, func() {
			_, undoErr = f.svc.UndoCompletion(f.ctx, f.actor, f.household, created.ID, receipt.ID, 2)
		}, func() {
			_, editErr = f.svc.Update(f.ctx, f.actor, f.household, created.ID, 2, item.Patch{AttentionOn: item.Some("2027-02-10")})
		})
		if (undoErr == nil) == (editErr == nil) {
			t.Fatalf("iteration %d: expected exactly one winner, undo=%v edit=%v", iteration, undoErr, editErr)
		}
		if total, _ := f.ledgerCounts(t, created.ID); total != 1 {
			t.Fatalf("iteration %d: ledger total=%d, want 1", iteration, total)
		}
		row := f.readItem(t, created.ID)
		stored := f.readReceipt(t, receipt.ID)
		if editErr == nil {
			if !errors.Is(undoErr, item.ErrCompletionSuperseded) && !errors.Is(undoErr, item.ErrVersionMismatch) {
				t.Fatalf("iteration %d: losing undo err=%v, want superseded or version mismatch", iteration, undoErr)
			}
			if row.attention != "2027-02-10" || row.anchor == nil || *row.anchor != 10 || row.version != 3 || stored.undone {
				t.Fatalf("iteration %d: edit winner left attention=%q anchor=%s version=%d undone=%v",
					iteration, row.attention, anchorText(row.anchor), row.version, stored.undone)
			}
			continue
		}
		if !errors.Is(editErr, item.ErrVersionMismatch) {
			t.Fatalf("iteration %d: losing edit err=%v, want version mismatch", iteration, editErr)
		}
		if row.attention != "2027-01-31" || row.anchor == nil || *row.anchor != 31 || row.version != 3 || !stored.undone {
			t.Fatalf("iteration %d: undo winner left attention=%q anchor=%s version=%d undone=%v",
				iteration, row.attention, anchorText(row.anchor), row.version, stored.undone)
		}
	}
}
