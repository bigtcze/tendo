package postgres

import (
	"context"
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

// The V5-04 recurrence gate drives item.Service against a Europe/Prague household
// with an injected clock and asserts both returned receipts and committed item and
// ledger rows, so a service or storage regression cannot pass on returned values
// alone. This file holds the shared fixture; the tests live in the sibling files.

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

// businessDate is the household-local calendar date the service derives from the
// injected clock, using the same production rule rather than a test-local one.
func (f *recurrenceGateFixture) businessDate(t *testing.T) schedule.Date {
	t.Helper()
	d, err := schedule.BusinessDate(f.now, "Europe/Prague")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// businessLocal returns the exact local date and UTC offset at the injected clock,
// so a test can prove the Prague midnight or DST offset actually changed.
func (f *recurrenceGateFixture) businessLocal(t *testing.T) (date, offset string) {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Fatal(err)
	}
	local := f.now.In(loc)
	return local.Format("2006-01-02"), local.Format("-07:00")
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
