package postgres

import (
	"testing"

	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/bigtcze/tendo/backend/internal/schedule/scheduletest"
)

// TestRecurrenceGateUndoAfterLateCompletionSkipsCycles covers RECURRENCE
// "completion and undo" for a late completion that skips occurrences. The contract
// ("late fixed skips missed anchors") chooses the earliest anchor occurrence
// strictly after the completion: Jan 31 -> Feb 28 -> Mar 31 -> Apr 30, so a
// 2027-04-15 completion lands on 2027-04-30 with anchor 31. Undo must restore the
// exact completed cycle, anchor, version and ledger.
func TestRecurrenceGateUndoAfterLateCompletionSkipsCycles(t *testing.T) {
	f := newRecurrenceGateFixture(t)
	policy := monthlyFixedGatePolicy()
	engineNext, err := schedule.NextCycle(
		&schedule.Cycle{On: scheduletest.Date("2027-01-31"), AnchorDay: 31},
		scheduletest.Date("2027-04-15"), *policy)
	if err != nil || engineNext == nil || engineNext.On.String() != "2027-04-30" || engineNext.AnchorDay != 31 {
		t.Fatalf("engine late completion next=%v err=%v, want 2027-04-30 anchor 31", engineNext, err)
	}

	f.setNow(t, "2027-04-15")
	created := f.create(t, "gate-late-undo", "2027-01-31", policy)
	late := f.complete(t, created.ID, created.Version, "gate-late-1")
	assertReceiptSchedule(t, late, "2027-01-31", 31, "2027-04-30", 31)
	f.assertItem(t, created.ID, "2027-04-30", 31, false, 2)

	f.undo(t, created.ID, late.ID, 2)
	f.assertItem(t, created.ID, "2027-01-31", 31, false, 3)
	f.assertReceipt(t, late.ID, "2027-01-31", 31, "2027-04-30", 31, true)

	// A second late completion followed by an exact one, so undoing the exact
	// receipt restores a clamped cycle date (2027-04-30) that must keep anchor 31.
	f.setNow(t, "2027-04-15")
	lateAgain := f.complete(t, created.ID, 3, "gate-late-2")
	assertReceiptSchedule(t, lateAgain, "2027-01-31", 31, "2027-04-30", 31)
	f.setNow(t, "2027-04-30")
	exact := f.complete(t, created.ID, 4, "gate-late-3")
	assertReceiptSchedule(t, exact, "2027-04-30", 31, "2027-05-31", 31)
	f.assertItem(t, created.ID, "2027-05-31", 31, false, 5)

	f.undo(t, created.ID, exact.ID, 5)
	f.assertItem(t, created.ID, "2027-04-30", 31, false, 6)
	f.assertReceipt(t, exact.ID, "2027-04-30", 31, "2027-05-31", 31, true)
	f.undo(t, created.ID, lateAgain.ID, 6)
	f.assertItem(t, created.ID, "2027-01-31", 31, false, 7)
	if total, undone := f.ledgerCounts(t, created.ID); total != 3 || undone != 3 {
		t.Fatalf("late undo ledger total=%d undone=%d, want 3/3", total, undone)
	}
	for _, receiptID := range []string{late.ID, lateAgain.ID, exact.ID} {
		if row := f.readReceipt(t, receiptID); !row.undone {
			t.Fatalf("receipt %s not undone after the late undo chain", receiptID)
		}
	}
}

// TestRecurrenceGateUndoAfterEarlyCompletion completes before the due date and
// undoes it, restoring the exact original due date and anchor.
func TestRecurrenceGateUndoAfterEarlyCompletion(t *testing.T) {
	f := newRecurrenceGateFixture(t)
	policy := monthlyFixedGatePolicy()
	engineNext, err := schedule.NextCycle(
		&schedule.Cycle{On: scheduletest.Date("2027-01-31"), AnchorDay: 31},
		scheduletest.Date("2027-01-10"), *policy)
	if err != nil || engineNext == nil || engineNext.On.String() != "2027-02-28" || engineNext.AnchorDay != 31 {
		t.Fatalf("engine early completion next=%v err=%v, want 2027-02-28 anchor 31", engineNext, err)
	}

	f.setNow(t, "2027-01-10")
	created := f.create(t, "gate-early-undo", "2027-01-31", policy)
	early := f.complete(t, created.ID, created.Version, "gate-early-1")
	assertReceiptSchedule(t, early, "2027-01-31", 31, "2027-02-28", 31)
	f.assertItem(t, created.ID, "2027-02-28", 31, false, 2)

	f.undo(t, created.ID, early.ID, 2)
	f.assertItem(t, created.ID, "2027-01-31", 31, false, 3)
	f.assertReceipt(t, early.ID, "2027-01-31", 31, "2027-02-28", 31, true)
	if total, undone := f.ledgerCounts(t, created.ID); total != 1 || undone != 1 {
		t.Fatalf("early undo ledger total=%d undone=%d, want 1/1", total, undone)
	}
}
