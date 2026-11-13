package schedule_test

import (
	"testing"

	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/bigtcze/tendo/backend/internal/schedule/scheduletest"
)

// TestNextCycleContractGolden asserts every V5_RECURRENCE_CONTRACT golden row against the pure engine.
func TestNextCycleContractGolden(t *testing.T) {
	runContractCases(t, scheduletest.Golden())
}

// TestNextCycleContractCoverage asserts the contract's additional mandatory calendar coverage.
func TestNextCycleContractCoverage(t *testing.T) {
	runContractCases(t, scheduletest.Coverage())
}

func runContractCases(t *testing.T, cases []scheduletest.Case) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			// The harness chains the new Cycle value because a bare Date cannot carry the anchor.
			due, err := schedule.NewCycle(scheduletest.Date(tc.Due), scheduletest.Date(tc.Due).Day())
			if err != nil {
				t.Fatal(err)
			}
			p := schedule.Policy{}
			if tc.Policy != nil {
				p = *tc.Policy
			}
			dueText := tc.Due
			for index, step := range tc.Steps {
				got, err := schedule.NextCycle(&due, scheduletest.Date(step.Completion), p)
				if err != nil {
					t.Fatalf("step %d: %v", index+1, err)
				}
				gotText := ""
				if got != nil {
					gotText = got.On.String()
				}
				scheduletest.Check(t, tc.Name, index, dueText, step, gotText)
				if got == nil {
					return
				}
				due, dueText = *got, gotText
			}
		})
	}
}
