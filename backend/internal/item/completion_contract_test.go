package item_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/bigtcze/tendo/backend/internal/schedule/scheduletest"
)

// TestCompletionContractGoldenReturnedAndPersisted drives every V5_RECURRENCE_CONTRACT golden row and
// its additional calendar coverage through the item completion service (the Done entry point behind
// the HTTP API) and asserts the exact returned receipt date, the persisted item date and one ledger
// entry per Done. The pure engine counterpart is backend/internal/schedule/schedule_contract_test.go.
func TestCompletionContractGoldenReturnedAndPersisted(t *testing.T) {
	for _, tc := range append(scheduletest.Golden(), scheduletest.Coverage()...) {
		t.Run(tc.Name, func(t *testing.T) {
			// The household clock sits on the last completion so every submitted date is valid (not future).
			last := tc.Steps[len(tc.Steps)-1].Completion
			e := completionEnv("Europe/Prague", last, item.Item{ID: missingID, AttentionOn: d(tc.Due), Recurrence: tc.Policy})
			due := tc.Due
			for index, step := range tc.Steps {
				on := step.Completion
				version := int64(index + 1)
				receipt, replayed, err := runCompletion(t, e, missingID, fmt.Sprintf("contract-%d", index+1), version, &on)
				if err != nil || replayed {
					t.Fatalf("step %d: replayed=%v err=%v", index+1, replayed, err)
				}
				row, _ := e.repo.Row(missingID)
				due = checkContractStep(t, tc.Name, index, due, step, receipt, row)
			}
			history, err := e.svc.ListCompletions(context.Background(), userID, householdID, missingID, 50, "", item.CompletionOrderOldest)
			if err != nil || len(history.Items) != len(tc.Steps) {
				t.Fatalf("ledger entries=%d err=%v, want %d", len(history.Items), err, len(tc.Steps))
			}
		})
	}
}

// checkContractStep asserts one Done's receipt and persisted row and returns the new due date.
func checkContractStep(t *testing.T, name string, index int, due string, step scheduletest.Step, receipt item.Completion, row item.Item) string {
	t.Helper()
	if receipt.CompletedOn.String() != step.Completion || receipt.CycleAttentionOn == nil || receipt.CycleAttentionOn.String() != due {
		t.Fatalf("%s step %d: receipt completed=%s cycle=%v, want %s/%s", name, index+1, receipt.CompletedOn, receipt.CycleAttentionOn, step.Completion, due)
	}
	if row.Version != int64(index+2) || row.LastCompletedOn == nil || row.LastCompletedOn.String() != step.Completion {
		t.Fatalf("%s step %d: persisted version=%d last=%v", name, index+1, row.Version, row.LastCompletedOn)
	}
	if step.Want == "" {
		if !row.Done || receipt.NextAttentionOn != nil {
			t.Fatalf("%s one-off: done=%v next=%v", name, row.Done, receipt.NextAttentionOn)
		}
		return due
	}
	returned, persisted := "", ""
	if receipt.NextAttentionOn != nil {
		returned = receipt.NextAttentionOn.String()
	}
	if row.AttentionOn != nil {
		persisted = row.AttentionOn.String()
	}
	if row.Done {
		t.Fatalf("%s step %d: recurring item marked done", name, index+1)
	}
	scheduletest.Check(t, name+" (returned)", index, due, step, returned)
	scheduletest.Check(t, name+" (persisted)", index, due, step, persisted)
	return persisted
}

// TestCompletionContractDaylightSavingLocalDate pins that an omitted completedOn resolves to the
// household-local calendar date across Europe/Prague DST transitions (injected clock, no real time),
// and that the next date is calendar arithmetic on that local date, not UTC-hour arithmetic.
func TestCompletionContractDaylightSavingLocalDate(t *testing.T) {
	cases := []struct {
		name, instant, due, wantCompleted, wantNext string
		p                                           *schedule.Policy
	}{
		// 2026-03-28T23:30Z is 00:30 CET on March 29, the spring-forward day.
		{"spring forward monthly fixed", "2026-03-28T23:30:00Z", "2026-03-29", "2026-03-29", "2026-04-29", &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitMonth}, Mode: schedule.ModeFixed}},
		// 2026-10-24T22:30Z is 00:30 CEST on October 25, the fall-back day (25 local hours).
		{"fall back daily fixed", "2026-10-24T22:30:00Z", "2026-10-25", "2026-10-25", "2026-10-26", &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}},
		// 2026-10-25T22:30Z is 23:30 CET, still October 25 locally although UTC+1 hours later.
		{"fall back fluid weekly late evening", "2026-10-25T22:30:00Z", "2026-10-20", "2026-10-25", "2026-11-01", &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitWeek}, Mode: schedule.ModeAfterCompletion}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := completionEnv("Europe/Prague", tc.due, item.Item{ID: missingID, AttentionOn: d(tc.due), Recurrence: tc.p})
			now, err := time.Parse(time.RFC3339, tc.instant)
			if err != nil {
				t.Fatal(err)
			}
			e.now = now
			receipt, _, err := runCompletion(t, e, missingID, "dst", 1, nil)
			if err != nil {
				t.Fatal(err)
			}
			row, _ := e.repo.Row(missingID)
			if receipt.CompletedOn.String() != tc.wantCompleted || receipt.NextAttentionOn == nil || receipt.NextAttentionOn.String() != tc.wantNext || row.AttentionOn == nil || row.AttentionOn.String() != tc.wantNext {
				t.Fatalf("completed=%s returned=%v persisted=%v, want %s -> %s", receipt.CompletedOn, receipt.NextAttentionOn, row.AttentionOn, tc.wantCompleted, tc.wantNext)
			}
		})
	}
}
