package schedule

import (
	"errors"
	"testing"
	"time"
)

func date(t *testing.T, s string) Date {
	t.Helper()
	x, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDate(x.Year(), x.Month(), x.Day())
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func policy(unit Unit, n int, mode Mode) Policy {
	return Policy{Enabled: true, Interval: Interval{Value: n, Unit: unit}, Mode: mode}
}
func wantDate(t *testing.T, got *Date, want string, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.String() != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestNextCycle(t *testing.T) {
	cases := []struct {
		name                     string
		anchor, completion, want string
		p                        Policy
	}{
		{"fixed early", "2026-09-01", "2026-08-20", "2027-09-01", policy(UnitYear, 1, ModeFixed)},
		{"fixed late", "2026-09-01", "2026-09-20", "2027-09-01", policy(UnitYear, 1, ModeFixed)},
		{"fixed skipped anchors", "2026-01-01", "2026-03-15", "2026-04-01", policy(UnitMonth, 1, ModeFixed)},
		{"fixed daily catch-up", "2026-01-01", "2026-01-03", "2026-01-04", policy(UnitDay, 1, ModeFixed)},
		{"fixed weekly catch-up", "2026-01-01", "2026-01-15", "2026-01-22", policy(UnitWeek, 1, ModeFixed)},
		{"absent monthly fixed", "", "2026-01-31", "2026-02-28", policy(UnitMonth, 1, ModeFixed)},
		{"absent monthly fluid", "", "2026-01-31", "2026-02-28", policy(UnitMonth, 1, ModeAfterCompletion)},
		{"historical monthly fixed", "", "2020-01-01", "2020-02-01", policy(UnitMonth, 1, ModeFixed)},
		{"historical monthly fluid", "", "2020-01-01", "2020-02-01", policy(UnitMonth, 1, ModeAfterCompletion)},
		{"fixed maximum catch-up", "0001-01-01", "9999-12-31", "", policy(UnitDay, 1, ModeFixed)},
		{"fixed at date-range bound", "0001-01-01", "9999-12-30", "9999-12-31", policy(UnitDay, 1, ModeFixed)},
		{"exact anchor", "2026-01-01", "2026-02-01", "2026-03-01", policy(UnitMonth, 1, ModeFixed)},
		{"daily interval", "2024-02-28", "2024-02-28", "2024-02-29", policy(UnitDay, 1, ModeAfterCompletion)},
		{"weekly interval", "2025-12-21", "2025-12-28", "2026-01-04", policy(UnitWeek, 1, ModeAfterCompletion)},
		{"fluid early", "2026-09-01", "2026-08-20", "2026-09-20", policy(UnitMonth, 1, ModeAfterCompletion)},
		{"fluid late", "2026-09-01", "2026-10-20", "2026-11-20", policy(UnitMonth, 1, ModeAfterCompletion)},
		{"leap clamp", "2024-02-29", "2024-02-29", "2025-02-28", policy(UnitYear, 1, ModeFixed)},
		{"consecutive clamp", "2025-01-31", "2025-03-01", "2025-03-28", policy(UnitMonth, 1, ModeFixed)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var a *Date
			if tc.anchor != "" {
				x := date(t, tc.anchor)
				a = &x
			}
			got, err := NextCycle(a, date(t, tc.completion), tc.p)
			if tc.want == "" {
				if !errors.Is(err, ErrDateOverflow) || got != nil {
					t.Fatalf("got %v, %v; want overflow", got, err)
				}
			} else {
				wantDate(t, got, tc.want, err)
				if tc.anchor == "" && tc.completion == "2020-01-01" {
					attention, err := DeriveAttention(got, date(t, "2026-01-01"))
					if err != nil || attention != NeedsAttention {
						t.Fatalf("historical next cycle attention: got %q, %v; want %q", attention, err, NeedsAttention)
					}
				}
			}
			if tc.anchor != "" && a.String() != tc.anchor {
				t.Fatal("anchor mutated")
			}
		})
	}
	got, err := NextCycle(nil, date(t, "2026-01-01"), Policy{})
	if err != nil || got != nil {
		t.Fatalf("disabled policy: %v %v", got, err)
	}
	for _, unit := range []Unit{UnitDay, UnitWeek, UnitMonth, UnitYear} {
		if _, err := date(t, "2020-01-01").AddInterval(Interval{int(^uint(0) >> 1), unit}); !errors.Is(err, ErrDateOverflow) {
			t.Errorf("max interval %s: %v", unit, err)
		}
	}
	if _, err := date(t, "0001-01-01").AddDays(-1); !errors.Is(err, ErrDateOverflow) {
		t.Errorf("negative boundary: %v", err)
	}
	if _, err := date(t, "9999-12-31").AddInterval(Interval{1, UnitMonth}); !errors.Is(err, ErrDateOverflow) {
		t.Errorf("month boundary: %v", err)
	}
	if _, err := NextCycle(nil, Date{}, policy(UnitDay, 1, ModeFixed)); !errors.Is(err, ErrInvalidDate) {
		t.Errorf("zero completion: %v", err)
	}
	zeroAnchor := Date{}
	if _, err := NextCycle(&zeroAnchor, date(t, "2025-01-01"), policy(UnitDay, 1, ModeFixed)); !errors.Is(err, ErrInvalidDate) {
		t.Errorf("zero fixed anchor: %v", err)
	}
	if got, err := NextCycle(nil, date(t, "2025-01-01"), Policy{Interval: Interval{Value: -1}}); err != nil || got != nil {
		t.Errorf("disabled malformed policy: %v, %v", got, err)
	}
}
func TestDateAndPolicyValidation(t *testing.T) {
	for _, tc := range []struct {
		y int
		m time.Month
		d int
	}{{0, 1, 1}, {10000, 1, 1}, {2025, 2, 29}, {2024, 13, 1}, {2024, 1, 0}} {
		if _, err := NewDate(tc.y, tc.m, tc.d); !errors.Is(err, ErrInvalidDate) {
			t.Errorf("%+v: %v", tc, err)
		}
	}
	for _, p := range []Policy{policy(UnitDay, 0, ModeFixed), policy(UnitDay, -1, ModeFixed), policy(UnitDay, 1, "bad"), policy("bad", 1, ModeFixed)} {
		if p.Validate() == nil {
			t.Errorf("accepted %+v", p)
		}
	}
	for _, tc := range []struct {
		start string
		i     Interval
	}{{"9999-12-31", Interval{1, UnitDay}}, {"9999-12-31", Interval{1, UnitYear}}, {"2020-01-01", Interval{int(^uint(0) >> 1), UnitWeek}}} {
		if _, err := date(t, tc.start).AddInterval(tc.i); !errors.Is(err, ErrDateOverflow) {
			t.Errorf("%+v: %v", tc, err)
		}
	}
}
func TestAttention(t *testing.T) {
	zero := Date{}
	if _, err := DeriveAttention(&zero, date(t, "2025-01-02")); !errors.Is(err, ErrInvalidDate) {
		t.Errorf("zero attention date: %v", err)
	}
	past := date(t, "2025-01-01")
	same := date(t, "2025-01-02")
	future := date(t, "2025-01-03")
	cases := []struct {
		name  string
		on    *Date
		today string
		want  Attention
	}{
		{"absent", nil, "2025-01-02", NeedsAttention},
		{"past", &past, "2025-01-02", NeedsAttention},
		{"today", &same, "2025-01-02", NeedsAttention},
		{"future", &future, "2025-01-02", Upcoming},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DeriveAttention(tc.on, date(t, tc.today))
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
func BenchmarkFixedMaximumCatchUp(b *testing.B) {
	anchor, _ := NewDate(1, time.January, 1)
	completion, _ := NewDate(9999, time.December, 30)
	p := policy(UnitDay, 1, ModeFixed)
	for i := 0; i < b.N; i++ {
		if _, err := NextCycle(&anchor, completion, p); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBusinessDateTimezone(t *testing.T) {
	cases := []struct{ instant, zone, want string }{
		{"2025-01-01T00:30:00Z", "America/Los_Angeles", "2024-12-31"},
		{"2025-03-09T09:30:00Z", "America/Los_Angeles", "2025-03-09"},
		{"2025-11-02T08:30:00Z", "America/Los_Angeles", "2025-11-02"},
		{"2025-07-01T07:30:00Z", "America/Los_Angeles", "2025-07-01"},
		{"2025-01-01T07:30:00Z", "America/Los_Angeles", "2024-12-31"},
		{"2025-01-01T23:30:00Z", "Europe/Prague", "2025-01-02"},
		{"2025-01-01T23:30:00Z", "Japan", "2025-01-02"},
		{"2025-01-01T00:30:00Z", "UTC", "2025-01-01"},
	}
	for _, tc := range cases {
		at, _ := time.Parse(time.RFC3339, tc.instant)
		got, err := BusinessDate(at, tc.zone)
		if err != nil || got.String() != tc.want {
			t.Errorf("%s in %s: got %s, %v want %s", tc.instant, tc.zone, got, err, tc.want)
		}
	}
	at, _ := time.Parse(time.RFC3339, "2025-01-01T00:30:00Z")
	for _, zone := range []string{"", "Local", "Not/A_Zone"} {
		if _, err := BusinessDate(at, zone); err == nil {
			t.Errorf("accepted timezone %q", zone)
		}
	}
}
