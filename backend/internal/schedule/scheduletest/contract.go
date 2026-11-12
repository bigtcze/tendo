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
type Case struct {
	Name   string
	Due    string
	Policy *schedule.Policy
	Steps  []Step
}

func policy(unit schedule.Unit, n int, mode schedule.Mode) *schedule.Policy {
	return &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: n, Unit: unit}, Mode: mode}
}

// Golden is the golden-case table of .autonomous/V5_RECURRENCE_CONTRACT.md, row for row.
func Golden() []Case {
	monthly := policy(schedule.UnitMonth, 1, schedule.ModeFixed)
	return []Case{
		{"one-off", "2026-12-31", nil, []Step{{"2026-12-11", ""}}},
		{"monthly fixed", "2026-11-08", monthly, []Step{{"2026-11-08", "2026-12-08"}}},
		{"monthly fixed year-rollover", "2026-12-31", monthly, []Step{{"2026-12-31", "2027-01-31"}}},
		{"monthly fixed early", "2026-12-31", monthly, []Step{{"2026-12-11", "2027-01-31"}}},
		{"monthly fluid year-rollover", "2026-12-31", policy(schedule.UnitMonth, 1, schedule.ModeAfterCompletion), []Step{{"2026-12-11", "2027-01-11"}}},
		{"monthly fixed short month", "2027-01-31", monthly, []Step{{"2027-01-31", "2027-02-28"}}},
		// The contract row starts at 2027-02-28 with original anchor 31; the only way to reach that
		// state through the product is a Done on 2027-01-31, so the chain starts there.
		{"fixed anchor restoration", "2027-01-31", monthly, []Step{{"2027-01-31", "2027-02-28"}, {"2027-02-28", "2027-03-31"}}},
		{"leap year monthly", "2028-01-31", monthly, []Step{{"2028-01-31", "2028-02-29"}}},
		{"yearly leap day", "2028-02-29", policy(schedule.UnitYear, 1, schedule.ModeFixed), []Step{
			{"2028-02-29", "2029-02-28"}, {"2029-02-28", "2030-02-28"}, {"2030-02-28", "2031-02-28"}, {"2031-02-28", "2032-02-29"},
		}},
		{"late fixed skips missed anchors", "2026-09-08", monthly, []Step{{"2026-12-11", "2027-01-08"}}},
		{"fixed 2-month interval", "2026-11-30", policy(schedule.UnitMonth, 2, schedule.ModeFixed), []Step{{"2026-11-30", "2027-01-30"}}},
		{"daily fixed", "2026-11-11", policy(schedule.UnitDay, 1, schedule.ModeFixed), []Step{{"2026-11-11", "2026-11-12"}}},
		{"weekly fixed", "2026-12-30", policy(schedule.UnitWeek, 1, schedule.ModeFixed), []Step{{"2026-12-30", "2027-01-06"}}},
	}
}

// Coverage applies the contract definitions to its "Additional mandatory coverage" calendar list:
// 3/6/12-month and 7/14-day intervals, early/late with interval >1, January/December crossing and
// Gregorian leap centuries (2000 is a leap year, 2100 is not). Anchors never drift after a clip.
func Coverage() []Case {
	yearly := policy(schedule.UnitYear, 1, schedule.ModeFixed)
	return []Case{
		{"quarterly month-end restores anchor", "2026-11-30", policy(schedule.UnitMonth, 3, schedule.ModeFixed), []Step{{"2026-11-30", "2027-02-28"}, {"2027-02-28", "2027-05-30"}}},
		{"half-yearly month-end restores anchor", "2026-08-31", policy(schedule.UnitMonth, 6, schedule.ModeFixed), []Step{{"2026-08-31", "2027-02-28"}, {"2027-02-28", "2027-08-31"}}},
		{"12-month leap day", "2028-02-29", policy(schedule.UnitMonth, 12, schedule.ModeFixed), []Step{
			{"2028-02-29", "2029-02-28"}, {"2029-02-28", "2030-02-28"}, {"2030-02-28", "2031-02-28"}, {"2031-02-28", "2032-02-29"},
		}},
		{"2-month late skips missed anchors", "2026-09-30", policy(schedule.UnitMonth, 2, schedule.ModeFixed), []Step{{"2026-12-15", "2027-01-30"}}},
		{"2-month early consumes current occurrence", "2026-12-31", policy(schedule.UnitMonth, 2, schedule.ModeFixed), []Step{{"2026-12-01", "2027-02-28"}, {"2027-02-28", "2027-04-30"}}},
		{"7-day fixed December crossing", "2026-12-29", policy(schedule.UnitDay, 7, schedule.ModeFixed), []Step{{"2026-12-29", "2027-01-05"}}},
		{"14-day fluid December crossing", "2026-12-20", policy(schedule.UnitDay, 14, schedule.ModeAfterCompletion), []Step{{"2026-12-24", "2027-01-07"}}},
		{"daily fixed into leap day", "2028-02-28", policy(schedule.UnitDay, 1, schedule.ModeFixed), []Step{{"2028-02-28", "2028-02-29"}, {"2028-02-29", "2028-03-01"}}},
		{"leap century 2000", "1996-02-29", yearly, []Step{
			{"1996-02-29", "1997-02-28"}, {"1997-02-28", "1998-02-28"}, {"1998-02-28", "1999-02-28"}, {"1999-02-28", "2000-02-29"},
		}},
		{"non-leap century 2100", "2096-02-29", yearly, []Step{
			{"2096-02-29", "2097-02-28"}, {"2097-02-28", "2098-02-28"}, {"2098-02-28", "2099-02-28"}, {"2099-02-28", "2100-02-28"},
			{"2100-02-28", "2101-02-28"}, {"2101-02-28", "2102-02-28"}, {"2102-02-28", "2103-02-28"}, {"2103-02-28", "2104-02-29"},
		}},
	}
}

// Check asserts the exact next date one step produced: the contract date, and never the date that
// was just completed. stepIndex is zero-based; due is the occurrence the step completed.
func Check(t testing.TB, name string, stepIndex int, due string, step Step, got string) {
	t.Helper()
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
