package schedule

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
)

// This file adds seeded randomized chain coverage over a four-century range and a
// native Go fuzz target for NextCycle. Every oracle computation lives here and uses
// calendar arithmetic independent of the engine (year*12+month and time.Date for the
// last day of a month), so a drift in the engine cannot also drift the expectation.

const (
	chainSteps       = 30
	chainSeedCount   = 5
	randomStartCount = 4
)

func lastDayOf(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// referenceChainIntervals is the contract's mandatory calendar list:
// 1, 2, 3, 6, 12 months, 1 year, 1, 7, 14 days and 1 week.
func referenceChainIntervals() []Interval {
	intervals := []Interval{}
	for _, value := range []int{1, 2, 3, 6, 12} {
		intervals = append(intervals, Interval{Value: value, Unit: UnitMonth})
	}
	intervals = append(intervals, Interval{Value: 1, Unit: UnitYear})
	for _, value := range []int{1, 7, 14} {
		intervals = append(intervals, Interval{Value: value, Unit: UnitDay})
	}
	return append(intervals, Interval{Value: 1, Unit: UnitWeek})
}

// forcedChainStarts pins leap centuries (2000 and 2400 leap, 2100/2200/2300 not),
// month-end days 28/29/30/31, Feb 29 and January/December crossings.
func forcedChainStarts() []Date {
	return []Date{
		referenceDate(1896, time.January, 31),
		referenceDate(1896, time.February, 29),
		referenceDate(1999, time.December, 31),
		referenceDate(2000, time.January, 31),
		referenceDate(2000, time.February, 29),
		referenceDate(2026, time.December, 31),
		referenceDate(2027, time.January, 28),
		referenceDate(2027, time.January, 29),
		referenceDate(2027, time.January, 30),
		referenceDate(2027, time.January, 31),
		referenceDate(2028, time.February, 29),
		referenceDate(2100, time.February, 28),
		referenceDate(2200, time.February, 28),
		referenceDate(2300, time.February, 28),
		referenceDate(2400, time.February, 29),
		referenceDate(2404, time.December, 31),
	}
}

// randomChainStarts spans the whole 1896..2404 range with a deliberate bias towards
// month-end and leap-day starts.
func randomChainStarts(seed uint64) []Date {
	rng := rand.New(rand.NewPCG(seed, 0x5eed))
	starts := make([]Date, 0, randomStartCount)
	for len(starts) < randomStartCount {
		year := 1896 + rng.IntN(509)
		month := time.Month(1 + rng.IntN(12))
		last := lastDayOf(year, month)
		day := 1 + rng.IntN(last)
		if rng.IntN(2) == 0 {
			day = last - rng.IntN(4)
			if day < 1 {
				day = 1
			}
		}
		starts = append(starts, referenceDate(year, month, day))
	}
	return starts
}

// chainOracleNext is the independent expectation for a single NextCycle step.
func chainOracleNext(current Cycle, completion Date, policy Policy) Cycle {
	if policy.Mode == ModeAfterCompletion {
		next := referenceAdd(completion, policy.Interval)
		anchor := completion.Day()
		if policy.Interval.Unit == UnitDay || policy.Interval.Unit == UnitWeek {
			anchor = next.Day()
		}
		return Cycle{On: next, AnchorDay: anchor}
	}
	if policy.Interval.Unit == UnitMonth || policy.Interval.Unit == UnitYear {
		return fixedCycleOracle(current, completion, policy.Interval)
	}
	next := referenceAdd(current.On, policy.Interval)
	for next.Compare(completion) <= 0 {
		next = referenceAdd(next, policy.Interval)
	}
	return Cycle{On: next, AnchorDay: next.Day()}
}

// chainCompletion picks exact, early or late (skipping several occurrences)
// completions deterministically from the supplied generator.
func chainCompletion(rng *rand.Rand, on Date) (Date, error) {
	switch rng.IntN(10) {
	case 0, 1, 2, 3:
		return on, nil
	case 4, 5, 6:
		return on.AddDays(-(1 + rng.IntN(45)))
	default:
		late := [...]int{0, 1, 2, 3, 5, 10, 30, 95, 200, 400}
		return on.AddDays(late[rng.IntN(len(late))])
	}
}

func isMonthOrYear(unit Unit) bool { return unit == UnitMonth || unit == UnitYear }

// assertChainStep checks every per-step invariant: strict progress, a valid cycle,
// the independent oracle result, an unchanged fixed anchor, a correct clamped day
// and exact single-occurrence consumption after an early or exact completion.
func assertChainStep(t *testing.T, label string, current Cycle, completion Date, policy Policy, got *Cycle, err error) {
	t.Helper()
	if err != nil || got == nil {
		t.Fatalf("%s: completion=%s interval=%+v mode=%s: next=%v err=%v", label, completion, policy.Interval, policy.Mode, got, err)
	}
	if !got.Valid() {
		t.Fatalf("%s: invalid cycle %s anchor %d (completion %s)", label, got.On, got.AnchorDay, completion)
	}
	if got.On.Compare(completion) <= 0 {
		t.Fatalf("%s: %s is not strictly after completion %s", label, got.On, completion)
	}
	want := chainOracleNext(current, completion, policy)
	if *got != want {
		t.Fatalf("%s: completion=%s current=%s anchor %d: got %s anchor %d, want %s anchor %d",
			label, completion, current.On, current.AnchorDay, got.On, got.AnchorDay, want.On, want.AnchorDay)
	}
	if policy.Mode != ModeFixed {
		return
	}
	if !isMonthOrYear(policy.Interval.Unit) {
		if got.AnchorDay != got.On.Day() {
			t.Fatalf("%s: day/week anchor %d does not re-anchor on %s", label, got.AnchorDay, got.On)
		}
		return
	}
	if got.AnchorDay != current.AnchorDay {
		t.Fatalf("%s: fixed anchor drifted %d -> %d at %s", label, current.AnchorDay, got.AnchorDay, got.On)
	}
	last := lastDayOf(got.On.Year(), got.On.Month())
	wantDay := current.AnchorDay
	if wantDay > last {
		wantDay = last
	}
	if got.On.Day() != wantDay {
		t.Fatalf("%s: clamped day %d want %d (anchor %d) at %s", label, got.On.Day(), wantDay, current.AnchorDay, got.On)
	}
	if completion.Compare(current.On) <= 0 {
		one := chainOracleNext(current, current.On, policy)
		if *got != one {
			t.Fatalf("%s: early/exact completion %s did not consume exactly the next occurrence: got %s anchor %d, want %s anchor %d",
				label, completion, got.On, got.AnchorDay, one.On, one.AnchorDay)
		}
	}
}

// TestNextCycleSeededChainInvariants runs deterministic seeded chains of 30
// completions over forced and random starts from 1896 through 2404, for every
// mandatory interval and both modes, and asserts the per-step invariants.
func TestNextCycleSeededChainInvariants(t *testing.T) {
	intervals := referenceChainIntervals()
	for seed := uint64(1); seed <= chainSeedCount; seed++ {
		starts := append(forcedChainStarts(), randomChainStarts(seed)...)
		for intervalIndex, interval := range intervals {
			for modeIndex, mode := range []Mode{ModeFixed, ModeAfterCompletion} {
				policy := Policy{Enabled: true, Interval: interval, Mode: mode}
				for startIndex, start := range starts {
					caseSeed := seed*1_000_000 + uint64(intervalIndex*1_000+modeIndex*100+startIndex)
					rng := rand.New(rand.NewPCG(caseSeed, 448))
					cycle := Cycle{On: start, AnchorDay: start.Day()}
					for step := 0; step < chainSteps; step++ {
						label := fmt.Sprintf("seed=%d case=%d start=%s interval=%+v mode=%s step=%d",
							seed, caseSeed, start, interval, mode, step)
						before := cycle
						completion, err := chainCompletion(rng, cycle.On)
						if err != nil {
							t.Fatalf("%s: completion arithmetic: %v", label, err)
						}
						next, err := NextCycle(&cycle, completion, policy)
						if cycle != before {
							t.Fatalf("%s: NextCycle mutated the current cycle %s -> %s", label, before, cycle)
						}
						assertChainStep(t, label, before, completion, policy, next, err)
						cycle = *next
					}
				}
			}
		}
	}
}

// TestFixedYearlyFeb29AnchorAcrossCenturies asserts the century rule explicitly on a
// yearly fixed chain that starts on 2000-02-29 and ends on 2400-02-29.
func TestFixedYearlyFeb29AnchorAcrossCenturies(t *testing.T) {
	policy := Policy{Enabled: true, Interval: Interval{Value: 1, Unit: UnitYear}, Mode: ModeFixed}
	cycle := Cycle{On: referenceDate(2000, time.February, 29), AnchorDay: 29}
	seen := map[int]Date{2000: cycle.On}
	for step := 0; step < 400; step++ {
		next, err := NextCycle(&cycle, cycle.On, policy)
		if err != nil || next == nil {
			t.Fatalf("step %d from %s: next=%v err=%v", step, cycle.On, next, err)
		}
		if next.AnchorDay != 29 {
			t.Fatalf("step %d: anchor %d, want 29", step, next.AnchorDay)
		}
		cycle = *next
		seen[cycle.On.Year()] = cycle.On
	}
	want := map[int]string{
		2000: "2000-02-29", 2001: "2001-02-28", 2004: "2004-02-29",
		2100: "2100-02-28", 2200: "2200-02-28", 2300: "2300-02-28",
		2400: "2400-02-29",
	}
	for year, text := range want {
		if got := seen[year].String(); got != text {
			t.Fatalf("year %d: got %q, want %q", year, got, text)
		}
	}
	// A leap day is preserved in every leap year and clamped to Feb 28 otherwise.
	for year := 2001; year <= 2400; year++ {
		leap := year%4 == 0 && (year%100 != 0 || year%400 == 0)
		wantDay := 28
		if leap {
			wantDay = 29
		}
		if got := seen[year]; got.Month() != time.February || got.Day() != wantDay {
			t.Fatalf("year %d: got %s, want February %d", year, got, wantDay)
		}
	}
}

// FuzzNextCycle fuzzes a normalized (date, anchor, completion offset, interval,
// unit, mode) tuple and asserts the same invariants against the independent oracle.
// The seed corpus runs as part of go test; the golden and century rows are seeded.
func FuzzNextCycle(f *testing.F) {
	for _, tc := range goldenRowsForFuzz() {
		f.Add(tc.year, tc.month, tc.day, tc.anchor, tc.offset, tc.intervalValue, tc.unitIndex, tc.modeIndex)
	}
	f.Fuzz(func(t *testing.T, year, month, day, anchor, offset, intervalValue, unitIndex, modeIndex int) {
		start, anchorDay := normalizeFuzzCycle(year, month, day, anchor)
		interval := normalizeFuzzInterval(unitIndex, intervalValue)
		mode := ModeFixed
		if uint64(modeIndex)%2 == 1 {
			mode = ModeAfterCompletion
		}
		completion, err := start.AddDays(int(uint64(offset)%1200) - 400)
		if err != nil {
			t.Fatalf("completion arithmetic for %s offset %d: %v", start, offset, err)
		}
		policy := Policy{Enabled: true, Interval: interval, Mode: mode}
		cycle := Cycle{On: start, AnchorDay: anchorDay}
		if !cycle.Valid() {
			t.Fatalf("normalized cycle %s anchor %d is invalid (inputs %d/%d/%d/%d)", start, anchorDay, year, month, day, anchor)
		}
		next, err := NextCycle(&cycle, completion, policy)
		label := fmt.Sprintf("inputs y=%d m=%d d=%d anchor=%d offset=%d iv=%d unit=%d mode=%d",
			year, month, day, anchor, offset, intervalValue, unitIndex, modeIndex)
		assertChainStep(t, label, cycle, completion, policy, next, err)
	})
}

type fuzzSeed struct {
	year, month, day, anchor, offset, intervalValue, unitIndex, modeIndex int
}

// goldenRowsForFuzz seeds the fuzzer with the contract golden rows and leap-century
// cases, encoded in the same normalized space as the fuzz arguments. offset is
// days-400, so 400 means an exact completion.
func goldenRowsForFuzz() []fuzzSeed {
	const (
		unitDay   = 0
		unitWeek  = 1
		unitMonth = 2
		unitYear  = 3
	)
	return []fuzzSeed{
		{2027, 1, 31, 31, 400, 1, unitMonth, 0},  // monthly fixed short month
		{2027, 2, 28, 31, 400, 1, unitMonth, 0},  // fixed anchor restoration
		{2028, 1, 31, 31, 400, 1, unitMonth, 0},  // leap year monthly
		{2028, 2, 29, 29, 400, 1, unitYear, 0},   // yearly leap day
		{1999, 12, 31, 31, 401, 1, unitMonth, 0}, // January crossing, late
		{2026, 11, 30, 30, 385, 2, unitMonth, 0}, // 2-month interval, early
		{2026, 12, 31, 31, 401, 14, unitDay, 0},  // December crossing, late
		{1996, 2, 29, 29, 400, 1, unitYear, 0},   // leap century 2000
		{2096, 2, 29, 29, 400, 1, unitYear, 0},   // non-leap century 2100
		{2296, 2, 29, 29, 400, 1, unitYear, 0},   // non-leap century 2300
		{2396, 2, 29, 29, 400, 1, unitYear, 0},   // leap century 2400
		{2026, 12, 30, 30, 400, 1, unitWeek, 0},  // weekly fixed
		{2026, 12, 20, 20, 404, 14, unitDay, 1},  // 14-day fluid December crossing
	}
}

// normalizeIndex maps any integer into [base, base+span) deterministically and
// without overflow, so fuzzed negatives and extremes are still valid inputs.
func normalizeIndex(value, base, span int) int {
	return base + int((uint64(value)-uint64(base))%uint64(span))
}

// normalizeFuzzCycle maps arbitrary integers into a valid range 1896..2404 and a
// valid cycle whose anchor only exceeds the day for a month-end clamp.
func normalizeFuzzCycle(year, month, day, anchor int) (Date, int) {
	y := normalizeIndex(year, 1896, 509)
	m := time.Month(normalizeIndex(month, 1, 12))
	last := lastDayOf(y, m)
	d := normalizeIndex(day, 1, last)
	a := normalizeIndex(anchor, 1, 31)
	if a < d {
		a = d
	}
	if a > d && d != last {
		a = d
	}
	return referenceDate(y, m, d), a
}

func normalizeFuzzInterval(unitIndex, value int) Interval {
	unit := []Unit{UnitDay, UnitWeek, UnitMonth, UnitYear}[int(uint64(unitIndex)%4)]
	values := map[Unit][]int{
		UnitDay:   {1, 7, 14},
		UnitWeek:  {1},
		UnitMonth: {1, 2, 3, 6, 12},
		UnitYear:  {1},
	}[unit]
	return Interval{Value: values[int(uint64(value)%uint64(len(values)))], Unit: unit}
}
