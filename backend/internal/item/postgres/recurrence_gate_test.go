package postgres

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
)

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
// advance, a replay across the Prague midnight boundary is a no-op, and the local
// calendar date stays exact across the DST offset change.
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

	// Prague is UTC+1 in January: 22:59:59Z is 23:59:59 local on 2027-01-31 and
	// 23:00:00Z is exactly the start of 2027-02-01 local. The completion uses the
	// first business date and the replay after the boundary must not advance it.
	midnight := f.create(t, "gate-replay-midnight", "2027-01-31", monthlyFixedGatePolicy())
	f.now = time.Date(2027, time.January, 31, 22, 59, 59, 0, time.UTC)
	if got := f.businessDate(t); got.String() != "2027-01-31" {
		t.Fatalf("pre-midnight business date %s, want 2027-01-31", got)
	}
	if date, offset := f.businessLocal(t); date != "2027-01-31" || offset != "+01:00" {
		t.Fatalf("pre-midnight local %s %s, want 2027-01-31 +01:00", date, offset)
	}
	first := f.complete(t, midnight.ID, midnight.Version, "gate-replay-midnight")
	assertReceiptSchedule(t, first, "2027-01-31", 31, "2027-02-28", 31)
	f.now = time.Date(2027, time.January, 31, 23, 0, 0, 0, time.UTC)
	if got := f.businessDate(t); got.String() != "2027-02-01" {
		t.Fatalf("post-midnight business date %s, want 2027-02-01", got)
	}
	if date, offset := f.businessLocal(t); date != "2027-02-01" || offset != "+01:00" {
		t.Fatalf("post-midnight local %s %s, want 2027-02-01 +01:00", date, offset)
	}
	replayed, isReplay, err = f.svc.Complete(f.ctx, f.actor, f.household, midnight.ID, 2, "gate-replay-midnight", item.CompletionRequest{})
	if err != nil || !isReplay || replayed.ID != first.ID {
		t.Fatalf("midnight replay: id=%s replayed=%v err=%v, want %s replayed", replayed.ID, isReplay, err, first.ID)
	}
	f.assertItem(t, midnight.ID, "2027-02-28", 31, false, 2)
	if total, undone := f.ledgerCounts(t, midnight.ID); total != 1 || undone != 0 {
		t.Fatalf("midnight replay ledger total=%d undone=%d, want 1/0", total, undone)
	}

	// Prague switches from UTC+1 to UTC+2 at 01:00Z on 2027-03-28. Both 00:30Z
	// (01:30 local CET) and 01:30Z (03:30 local CEST) are the same calendar date,
	// so the exact business date and the replay must not depend on the offset.
	dst := f.create(t, "gate-replay-dst", "2027-03-28", monthlyFixedGatePolicy())
	f.now = time.Date(2027, time.March, 28, 0, 30, 0, 0, time.UTC)
	if got := f.businessDate(t); got.String() != "2027-03-28" {
		t.Fatalf("pre-transition business date %s, want 2027-03-28", got)
	}
	if date, offset := f.businessLocal(t); date != "2027-03-28" || offset != "+01:00" {
		t.Fatalf("pre-transition local %s %s, want 2027-03-28 +01:00", date, offset)
	}
	dstReceipt := f.complete(t, dst.ID, dst.Version, "gate-replay-dst")
	assertReceiptSchedule(t, dstReceipt, "2027-03-28", 28, "2027-04-28", 28)
	f.now = time.Date(2027, time.March, 28, 1, 30, 0, 0, time.UTC)
	if got := f.businessDate(t); got.String() != "2027-03-28" {
		t.Fatalf("post-transition business date %s, want 2027-03-28", got)
	}
	if date, offset := f.businessLocal(t); date != "2027-03-28" || offset != "+02:00" {
		t.Fatalf("post-transition local %s %s, want 2027-03-28 +02:00", date, offset)
	}
	replayed, isReplay, err = f.svc.Complete(f.ctx, f.actor, f.household, dst.ID, 2, "gate-replay-dst", item.CompletionRequest{})
	if err != nil || !isReplay || replayed.ID != dstReceipt.ID {
		t.Fatalf("DST replay: id=%s replayed=%v err=%v, want %s replayed", replayed.ID, isReplay, err, dstReceipt.ID)
	}
	f.assertItem(t, dst.ID, "2027-04-28", 28, false, 2)
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
