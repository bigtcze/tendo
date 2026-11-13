package schedule

import (
	"testing"
	"time"
)

func referenceDate(y int, m time.Month, day int) Date {
	d, err := NewDate(y, m, day)
	if err != nil {
		panic(err)
	}
	return d
}

// referenceAdd uses calendar arithmetic independently of Date.AddInterval.
func referenceAdd(start Date, interval Interval) Date {
	if interval.Unit == UnitDay || interval.Unit == UnitWeek {
		days := interval.Value
		if interval.Unit == UnitWeek {
			days *= 7
		}
		t := time.Date(start.Year(), start.Month(), start.Day()+days, 0, 0, 0, 0, time.UTC)
		return referenceDate(t.Year(), t.Month(), t.Day())
	}
	months := interval.Value
	if interval.Unit == UnitYear {
		months *= 12
	}
	total := start.Year()*12 + int(start.Month()) - 1 + months
	year, month := total/12, time.Month(total%12+1)
	day := start.Day()
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	return referenceDate(year, month, day)
}

func TestAddIntervalPropertyAgainstIndependentReference(t *testing.T) {
	units := []Unit{UnitDay, UnitWeek, UnitMonth, UnitYear}
	intervals := []int{1, 2, 3, 6, 12, 13, 52, 999}
	for start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC); !start.After(time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC)); start = start.AddDate(0, 0, 1) {
		gotStart := referenceDate(start.Year(), start.Month(), start.Day())
		for _, unit := range units {
			for _, value := range intervals {
				interval := Interval{Value: value, Unit: unit}
				got, err := gotStart.AddInterval(interval)
				want := referenceAdd(gotStart, interval)
				if err != nil || got != want || got.Compare(gotStart) <= 0 {
					t.Fatalf("start=%s interval=%+v: got=%s err=%v want=%s", gotStart, interval, got, err, want)
				}
				if unit == UnitMonth || unit == UnitYear {
					if got.Day() != gotStart.Day() && got.Day() != time.Date(got.Year(), got.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day() {
						t.Fatalf("start=%s interval=%+v: day changed without month-end clamp: %s", gotStart, interval, got)
					}
				}
			}
		}
	}
}

// fixedCycleOracle computes the next fixed month/year occurrence independently of
// the engine: anchor month + k*interval with the anchor day clamped once per
// occurrence, for the smallest k >= 1 strictly after completion.
func fixedCycleOracle(anchor Cycle, completion Date, interval Interval) Cycle {
	months := interval.Value
	if interval.Unit == UnitYear {
		months *= 12
	}
	base := anchor.On.Year()*12 + int(anchor.On.Month()) - 1
	for k := 1; ; k++ {
		total := base + k*months
		year, month := total/12, time.Month(total%12+1)
		day := anchor.AnchorDay
		if last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day(); day > last {
			day = last
		}
		occurrence := referenceDate(year, month, day)
		if occurrence.Compare(completion) > 0 {
			return Cycle{On: occurrence, AnchorDay: anchor.AnchorDay}
		}
	}
}

func TestNextCyclePropertyAgainstIndependentReference(t *testing.T) {
	units := []Unit{UnitDay, UnitWeek, UnitMonth, UnitYear}
	intervals := []int{1, 2, 3, 6, 12, 13, 52, 999}
	fixedDiffersFromFluid := 0
	for start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC); !start.After(time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC)); start = start.AddDate(0, 0, 1) {
		startDate := referenceDate(start.Year(), start.Month(), start.Day())
		anchorCycle := Cycle{On: startDate, AnchorDay: startDate.Day()}
		for _, unit := range units {
			for _, value := range intervals {
				interval := Interval{Value: value, Unit: unit}
				completion := referenceAdd(referenceAdd(startDate, interval), interval)
				dayBefore := Interval{Value: -1, Unit: UnitDay}
				dayAfter := Interval{Value: 1, Unit: UnitDay}
				// On-cadence dates plus early and off-cadence completions, where fixed and fluid results differ.
				completions := []Date{
					startDate, referenceAdd(startDate, interval), completion,
					referenceAdd(startDate, dayBefore),
					referenceAdd(referenceAdd(startDate, interval), dayBefore),
					referenceAdd(referenceAdd(startDate, interval), dayAfter),
				}
				for _, completionDate := range completions {
					for _, mode := range []Mode{ModeFixed, ModeAfterCompletion} {
						policy := Policy{Enabled: true, Interval: interval, Mode: mode}
						before := anchorCycle
						got, err := NextCycle(&anchorCycle, completionDate, policy)
						if err != nil || got == nil {
							t.Fatalf("anchor=%s completion=%s policy=%+v: got=%v err=%v", anchorCycle, completionDate, policy, got, err)
						}
						if anchorCycle != before {
							t.Fatalf("cycle mutated: before=%s after=%s", before, anchorCycle)
						}
						if !got.Valid() {
							t.Fatalf("invalid result cycle=%v anchor=%s completion=%s policy=%+v", got, anchorCycle, completionDate, policy)
						}
						if mode == ModeAfterCompletion {
							want := Cycle{On: referenceAdd(completionDate, interval), AnchorDay: completionDate.Day()}
							if unit == UnitDay || unit == UnitWeek {
								want.AnchorDay = want.On.Day()
							}
							if *got != want {
								t.Fatalf("fluid anchor=%s completion=%s interval=%+v: got=%v want=%v", anchorCycle, completionDate, interval, got, want)
							}
							for _, other := range []Date{startDate, completionDate, referenceAdd(completionDate, interval)} {
								fluid, err := NextCycle(&Cycle{On: other, AnchorDay: other.Day()}, completionDate, policy)
								if err != nil || fluid == nil || *fluid != *got {
									t.Fatalf("fluid depended on anchor %s: got=%v err=%v want=%v", other, fluid, err, got)
								}
							}
							continue
						}
						var want Cycle
						if unit == UnitDay || unit == UnitWeek {
							previous := startDate
							for next := referenceAdd(previous, interval); next.Compare(completionDate) <= 0; next = referenceAdd(next, interval) {
								previous = next
							}
							due := referenceAdd(previous, interval)
							want = Cycle{On: due, AnchorDay: due.Day()}
							if got.AnchorDay != got.On.Day() {
								t.Fatalf("day/week anchor=%d does not re-anchor on %s", got.AnchorDay, got.On)
							}
						} else {
							want = fixedCycleOracle(anchorCycle, completionDate, interval)
							if got.AnchorDay != anchorCycle.AnchorDay {
								t.Fatalf("fixed anchor drifted: %d -> %d anchor=%s completion=%s interval=%+v", anchorCycle.AnchorDay, got.AnchorDay, anchorCycle, completionDate, interval)
							}
						}
						if got.On.Compare(completionDate) <= 0 {
							t.Fatalf("fixed result not after completion: anchor=%s completion=%s got=%s", anchorCycle, completionDate, got)
						}
						if got.On != referenceAdd(completionDate, interval) {
							fixedDiffersFromFluid++
						}
						if *got != want {
							t.Fatalf("fixed chain anchor=%s completion=%s interval=%+v: got=%s want=%s", anchorCycle, completionDate, interval, got, want)
						}
					}
				}
				for _, mode := range []Mode{ModeFixed, ModeAfterCompletion} {
					policy := Policy{Enabled: true, Interval: interval, Mode: mode}
					want := Cycle{On: referenceAdd(completion, interval), AnchorDay: completion.Day()}
					if unit == UnitDay || unit == UnitWeek {
						want.AnchorDay = want.On.Day()
					}
					got, err := NextCycle(nil, completion, policy)
					if err != nil || got == nil || *got != want {
						t.Fatalf("nil cycle mode=%s completion=%s interval=%+v: got=%v err=%v want=%v", mode, completion, interval, got, err, want)
					}
				}
			}
		}
	}
	// Guards against a corpus where fixed mode could be implemented as completion + interval and still pass.
	if fixedDiffersFromFluid == 0 {
		t.Fatal("no generated fixed case distinguishes fixed from after-completion arithmetic")
	}
}

// TestCycleAnchorNeverDrifts chains completions through every unit and mode and
// asserts the Cycle invariant holds and that a fixed month/year anchor is never
// lost, including across early, late and skipped occurrences.
func TestCycleAnchorNeverDrifts(t *testing.T) {
	starts := []string{"2024-01-31", "2024-02-29", "2025-01-31", "2026-08-31", "2026-11-30", "2027-02-28", "2028-01-31", "2028-02-29", "2029-02-28", "2026-01-15"}
	intervals := []Interval{
		{Value: 1, Unit: UnitMonth}, {Value: 2, Unit: UnitMonth}, {Value: 3, Unit: UnitMonth},
		{Value: 6, Unit: UnitMonth}, {Value: 12, Unit: UnitMonth}, {Value: 1, Unit: UnitYear},
		{Value: 1, Unit: UnitDay}, {Value: 7, Unit: UnitDay}, {Value: 14, Unit: UnitDay},
	}
	for _, startText := range starts {
		parsed, err := time.Parse("2006-01-02", startText)
		if err != nil {
			t.Fatal(err)
		}
		start := referenceDate(parsed.Year(), parsed.Month(), parsed.Day())
		for _, interval := range intervals {
			for _, mode := range []Mode{ModeFixed, ModeAfterCompletion} {
				policy := Policy{Enabled: true, Interval: interval, Mode: mode}
				for _, late := range []bool{false, true} {
					cycle := Cycle{On: start, AnchorDay: start.Day()}
					wantAnchor := start.Day()
					for step := 0; step < 40; step++ {
						if !cycle.Valid() {
							t.Fatalf("start=%s interval=%+v mode=%s late=%v step %d: invalid cycle %s", startText, interval, mode, late, step, cycle)
						}
						completion := cycle.On
						if late {
							completion, err = completion.AddDays(5)
							if err != nil {
								t.Fatal(err)
							}
						}
						next, err := NextCycle(&cycle, completion, policy)
						if err != nil || next == nil {
							t.Fatalf("start=%s interval=%+v mode=%s late=%v step %d: got=%v err=%v", startText, interval, mode, late, step, next, err)
						}
						if !next.Valid() {
							t.Fatalf("start=%s interval=%+v mode=%s late=%v step %d: invalid next cycle %s", startText, interval, mode, late, step, next)
						}
						if next.On.Compare(completion) <= 0 {
							t.Fatalf("start=%s interval=%+v mode=%s late=%v step %d: %s not after %s", startText, interval, mode, late, step, next.On, completion)
						}
						if mode == ModeFixed && (interval.Unit == UnitMonth || interval.Unit == UnitYear) {
							if next.AnchorDay != wantAnchor {
								t.Fatalf("start=%s interval=%+v late=%v step %d: anchor drifted %d -> %d", startText, interval, late, step, wantAnchor, next.AnchorDay)
							}
							last := time.Date(next.On.Year(), next.On.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
							wantDay := wantAnchor
							if last < wantDay {
								wantDay = last
							}
							if next.On.Day() != wantDay {
								t.Fatalf("start=%s interval=%+v late=%v step %d: day %d want %d (anchor %d)", startText, interval, late, step, next.On.Day(), wantDay, wantAnchor)
							}
						} else if interval.Unit == UnitDay || interval.Unit == UnitWeek {
							if next.AnchorDay != next.On.Day() {
								t.Fatalf("start=%s interval=%+v mode=%s late=%v step %d: anchor %d not re-anchored on %s", startText, interval, mode, late, step, next.AnchorDay, next.On)
							}
						} else if next.AnchorDay != completion.Day() {
							t.Fatalf("start=%s interval=%+v mode=%s late=%v step %d: fluid anchor %d not the completion day %d", startText, interval, mode, late, step, next.AnchorDay, completion.Day())
						}
						cycle = *next
					}
				}
			}
		}
	}
}
