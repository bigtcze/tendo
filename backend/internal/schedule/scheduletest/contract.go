// Package scheduletest holds the V5 recurrence contract golden table shared by the pure engine,
// item service and real-PostgreSQL tests, so every layer asserts the same rows.
package scheduletest

import (
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/schedule"
)

// Step is one Done of the current occurrence on Completion and the exact next date it must produce
// ("" means no next occurrence).
type Step struct{ Completion, Want string }

// Case starts at the scheduled due date Due; each step consumes the current due date and its result
// becomes the next due date, exactly as repeated Done presses do.
//
// PreFix records a known defect: step number (1-based) -> the exact date the pre-fix engine produces
// instead of Want. It is a strict expected failure, not a skip: Check fails if the engine produces
// anything other than that recorded wrong date, including the contract date, so the fix (#447) must
// delete the entry in the same change. Want always stays the contract value.
type Case struct {
	Name   string
	Due    string
	Policy *schedule.Policy
	Steps  []Step
	PreFix map[int]string
}

func policy(unit schedule.Unit, n int, mode schedule.Mode) *schedule.Policy {
	return &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: n, Unit: unit}, Mode: mode}
}

// anchorDrift records the pre-fix defect: the engine re-anchors on the clipped previous due date
// instead of the persisted original anchor (see docs/development/recurrence-audit.md, finding R1).
func anchorDrift(step int, preFix string) map[int]string { return map[int]string{step: preFix} }

// Golden is the golden-case table of .autonomous/V5_RECURRENCE_CONTRACT.md, row for row.
func Golden() []Case {
	monthly := policy(schedule.UnitMonth, 1, schedule.ModeFixed)
	return []Case{
		{"one-off", "2026-12-31", nil, []Step{{"2026-12-11", ""}}, nil},
		{"monthly fixed", "2026-11-08", monthly, []Step{{"2026-11-08", "2026-12-08"}}, nil},
		{"monthly fixed year-rollover", "2026-12-31", monthly, []Step{{"2026-12-31", "2027-01-31"}}, nil},
		{"monthly fixed early", "2026-12-31", monthly, []Step{{"2026-12-11", "2027-01-31"}}, nil},
		{"monthly fluid year-rollover", "2026-12-31", policy(schedule.UnitMonth, 1, schedule.ModeAfterCompletion), []Step{{"2026-12-11", "2027-01-11"}}, nil},
		{"monthly fixed short month", "2027-01-31", monthly, []Step{{"2027-01-31", "2027-02-28"}}, nil},
		// The contract row starts at 2027-02-28 with original anchor 31; the only way to reach that
		// state through the product is a Done on 2027-01-31, so the chain starts there.
		{"fixed anchor restoration", "2027-01-31", monthly, []Step{{"2027-01-31", "2027-02-28"}, {"2027-02-28", "2027-03-31"}}, anchorDrift(2, "2027-03-28")},
		{"leap year monthly", "2028-01-31", monthly, []Step{{"2028-01-31", "2028-02-29"}}, nil},
		{"yearly leap day", "2028-02-29", policy(schedule.UnitYear, 1, schedule.ModeFixed), []Step{
			{"2028-02-29", "2029-02-28"}, {"2029-02-28", "2030-02-28"}, {"2030-02-28", "2031-02-28"}, {"2031-02-28", "2032-02-29"},
		}, anchorDrift(4, "2032-02-28")},
		{"late fixed skips missed anchors", "2026-09-08", monthly, []Step{{"2026-12-11", "2027-01-08"}}, nil},
		{"fixed 2-month interval", "2026-11-30", policy(schedule.UnitMonth, 2, schedule.ModeFixed), []Step{{"2026-11-30", "2027-01-30"}}, nil},
		{"daily fixed", "2026-11-11", policy(schedule.UnitDay, 1, schedule.ModeFixed), []Step{{"2026-11-11", "2026-11-12"}}, nil},
		{"weekly fixed", "2026-12-30", policy(schedule.UnitWeek, 1, schedule.ModeFixed), []Step{{"2026-12-30", "2027-01-06"}}, nil},
	}
}

// Coverage applies the contract definitions to its "Additional mandatory coverage" calendar list:
// 3/6/12-month and 7/14-day intervals, early/late with interval >1, January/December crossing and
// Gregorian leap centuries (2000 is a leap year, 2100 is not). Anchors never drift after a clip.
func Coverage() []Case {
	yearly := policy(schedule.UnitYear, 1, schedule.ModeFixed)
	return []Case{
		{"quarterly month-end restores anchor", "2026-11-30", policy(schedule.UnitMonth, 3, schedule.ModeFixed), []Step{{"2026-11-30", "2027-02-28"}, {"2027-02-28", "2027-05-30"}}, anchorDrift(2, "2027-05-28")},
		{"half-yearly month-end restores anchor", "2026-08-31", policy(schedule.UnitMonth, 6, schedule.ModeFixed), []Step{{"2026-08-31", "2027-02-28"}, {"2027-02-28", "2027-08-31"}}, anchorDrift(2, "2027-08-28")},
		{"12-month leap day", "2028-02-29", policy(schedule.UnitMonth, 12, schedule.ModeFixed), []Step{
			{"2028-02-29", "2029-02-28"}, {"2029-02-28", "2030-02-28"}, {"2030-02-28", "2031-02-28"}, {"2031-02-28", "2032-02-29"},
		}, anchorDrift(4, "2032-02-28")},
		{"2-month late skips missed anchors", "2026-09-30", policy(schedule.UnitMonth, 2, schedule.ModeFixed), []Step{{"2026-12-15", "2027-01-30"}}, nil},
		{"2-month early consumes current occurrence", "2026-12-31", policy(schedule.UnitMonth, 2, schedule.ModeFixed), []Step{{"2026-12-01", "2027-02-28"}, {"2027-02-28", "2027-04-30"}}, anchorDrift(2, "2027-04-28")},
		{"7-day fixed December crossing", "2026-12-29", policy(schedule.UnitDay, 7, schedule.ModeFixed), []Step{{"2026-12-29", "2027-01-05"}}, nil},
		{"14-day fluid December crossing", "2026-12-20", policy(schedule.UnitDay, 14, schedule.ModeAfterCompletion), []Step{{"2026-12-24", "2027-01-07"}}, nil},
		{"daily fixed into leap day", "2028-02-28", policy(schedule.UnitDay, 1, schedule.ModeFixed), []Step{{"2028-02-28", "2028-02-29"}, {"2028-02-29", "2028-03-01"}}, nil},
		{"leap century 2000", "1996-02-29", yearly, []Step{
			{"1996-02-29", "1997-02-28"}, {"1997-02-28", "1998-02-28"}, {"1998-02-28", "1999-02-28"}, {"1999-02-28", "2000-02-29"},
		}, anchorDrift(4, "2000-02-28")},
		{"non-leap century 2100", "2096-02-29", yearly, []Step{
			{"2096-02-29", "2097-02-28"}, {"2097-02-28", "2098-02-28"}, {"2098-02-28", "2099-02-28"}, {"2099-02-28", "2100-02-28"},
			{"2100-02-28", "2101-02-28"}, {"2101-02-28", "2102-02-28"}, {"2102-02-28", "2103-02-28"}, {"2103-02-28", "2104-02-29"},
		}, anchorDrift(8, "2104-02-28")},
	}
}

// Check asserts the exact next date one step produced: the contract date, and never the date that
// was just completed. stepIndex is zero-based; due is the occurrence the step completed. For a step
// listed in PreFix it instead requires exactly the recorded pre-fix date (strict expected failure).
func (c Case) Check(t testing.TB, stepIndex int, due, got string) {
	t.Helper()
	step := c.Steps[stepIndex]
	name := c.Name
	if preFix, known := c.PreFix[stepIndex+1]; known {
		if got != preFix {
			t.Fatalf("%s step %d: known defect R1 changed: got %q, recorded pre-fix %q, contract %q; if this is the #447 fix, delete the PreFix entry", name, stepIndex+1, got, preFix, step.Want)
		}
		t.Logf("%s step %d: KNOWN DEFECT R1 (#447): due %s completed %s gives %s, contract requires %s", name, stepIndex+1, due, step.Completion, got, step.Want)
		return
	}
	if got != step.Want {
		t.Fatalf("%s step %d: due %s completed %s: got %q, want %q", name, stepIndex+1, due, step.Completion, got, step.Want)
	}
	if got != "" && got == due {
		t.Fatalf("%s step %d: Done kept the same date %s", name, stepIndex+1, due)
	}
}

// Date parses a YYYY-MM-DD table value; table values are constants, so a bad one panics.
func Date(value string) schedule.Date {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	d, err := schedule.NewDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		panic(err)
	}
	return d
}
