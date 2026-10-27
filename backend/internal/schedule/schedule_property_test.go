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

func TestNextCyclePropertyAgainstIndependentReference(t *testing.T) {
	units := []Unit{UnitDay, UnitWeek, UnitMonth, UnitYear}
	intervals := []int{1, 2, 3, 6, 12, 13, 52, 999}
	fixedDiffersFromFluid := 0
	for start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC); !start.After(time.Date(2029, 12, 31, 0, 0, 0, 0, time.UTC)); start = start.AddDate(0, 0, 1) {
		anchor := referenceDate(start.Year(), start.Month(), start.Day())
		for _, unit := range units {
			for _, value := range intervals {
				interval := Interval{Value: value, Unit: unit}
				completion := referenceAdd(referenceAdd(anchor, interval), interval)
				dayBefore := Interval{Value: -1, Unit: UnitDay}
				dayAfter := Interval{Value: 1, Unit: UnitDay}
				// On-cadence dates plus early and off-cadence completions, where fixed and fluid results differ.
				completions := []Date{
					anchor, referenceAdd(anchor, interval), completion,
					referenceAdd(anchor, dayBefore),
					referenceAdd(referenceAdd(anchor, interval), dayBefore),
					referenceAdd(referenceAdd(anchor, interval), dayAfter),
				}
				for _, completion := range completions {
					for _, mode := range []Mode{ModeFixed, ModeAfterCompletion} {
						policy := Policy{Enabled: true, Interval: interval, Mode: mode}
						before := anchor
						got, err := NextCycle(&anchor, completion, policy)
						if err != nil || got == nil {
							t.Fatalf("anchor=%s completion=%s policy=%+v: got=%v err=%v", anchor, completion, policy, got, err)
						}
						if anchor != before {
							t.Fatalf("anchor mutated: before=%s after=%s", before, anchor)
						}
						if mode == ModeAfterCompletion {
							if *got != referenceAdd(completion, interval) {
								t.Fatalf("fluid anchor=%s completion=%s interval=%+v: got=%s", anchor, completion, interval, got)
							}
							for _, other := range []Date{anchor, completion, referenceAdd(completion, interval)} {
								fluid, err := NextCycle(&other, completion, policy)
								if err != nil || fluid == nil || *fluid != *got {
									t.Fatalf("fluid depended on anchor %s: got=%v err=%v want=%s", other, fluid, err, got)
								}
							}
							continue
						}
						if got.Compare(completion) <= 0 {
							t.Fatalf("fixed result not after completion: anchor=%s completion=%s got=%s", anchor, completion, got)
						}
						previous := anchor
						for next := referenceAdd(previous, interval); next.Compare(completion) <= 0; next = referenceAdd(next, interval) {
							previous = next
						}
						want := referenceAdd(previous, interval)
						if *got != referenceAdd(completion, interval) {
							fixedDiffersFromFluid++
						}
						if *got != want || (anchor.Compare(completion) <= 0 && previous.Compare(completion) > 0) {
							t.Fatalf("fixed chain anchor=%s completion=%s interval=%+v: previous=%s got=%s want=%s", anchor, completion, interval, previous, got, want)
						}
					}
				}
				for _, mode := range []Mode{ModeFixed, ModeAfterCompletion} {
					policy := Policy{Enabled: true, Interval: interval, Mode: mode}
					want := referenceAdd(completion, interval)
					got, err := NextCycle(nil, completion, policy)
					if err != nil || got == nil || *got != want {
						t.Fatalf("nil anchor mode=%s completion=%s interval=%+v: got=%v err=%v want=%s", mode, completion, interval, got, err, want)
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
