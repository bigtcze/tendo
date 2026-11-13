// Package schedule contains pure date-only attention and recurrence calculations.
package schedule

import (
	"errors"
	"fmt"
	"time"
)

var ErrInvalidDate = errors.New("invalid date")
var ErrInvalidPolicy = errors.New("invalid recurrence policy")
var ErrDateOverflow = errors.New("date overflow")

// Date is a validated Gregorian date in the inclusive range 0001-01-01..9999-12-31.
type Date struct {
	year  int
	month time.Month
	day   int
}

func NewDate(year int, month time.Month, day int) (Date, error) {
	if year < 1 || year > 9999 || month < time.January || month > time.December || day < 1 || day > daysInMonth(year, month) {
		return Date{}, ErrInvalidDate
	}
	return Date{year, month, day}, nil
}
func (d Date) Valid() bool       { _, err := NewDate(d.year, d.month, d.day); return err == nil }
func (d Date) Year() int         { return d.year }
func (d Date) Month() time.Month { return d.month }
func (d Date) Day() int          { return d.day }
func (d Date) String() string {
	if !d.Valid() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.year, d.month, d.day)
}
func (d Date) Compare(other Date) int {
	if d.year != other.year {
		if d.year < other.year {
			return -1
		}
		return 1
	}
	if d.month != other.month {
		if d.month < other.month {
			return -1
		}
		return 1
	}
	if d.day < other.day {
		return -1
	}
	if d.day > other.day {
		return 1
	}
	return 0
}
func (d Date) Before(other Date) bool { return d.Compare(other) < 0 }
func (d Date) AddDays(n int) (Date, error) {
	if !d.Valid() {
		return Date{}, ErrInvalidDate
	}
	// Bound work and avoid overflowing intermediate integer arithmetic.
	if n > 3652058 || n < -3652058 {
		return Date{}, ErrDateOverflow
	}
	t := time.Date(d.year, d.month, d.day+n, 0, 0, 0, 0, time.UTC)
	if t.Year() < 1 || t.Year() > 9999 {
		return Date{}, ErrDateOverflow
	}
	result, err := NewDate(t.Year(), t.Month(), t.Day())
	if err != nil {
		return Date{}, ErrDateOverflow
	}
	return result, nil
}
func (d Date) AddInterval(i Interval) (Date, error) {
	if !d.Valid() {
		return Date{}, ErrInvalidDate
	}
	if i.Value <= 0 {
		return Date{}, ErrInvalidPolicy
	}
	switch i.Unit {
	case UnitDay:
		return d.AddDays(i.Value)
	case UnitWeek:
		if i.Value > 521722 {
			return Date{}, ErrDateOverflow
		}
		return d.AddDays(i.Value * 7)
	case UnitMonth, UnitYear:
		months := i.Value
		if i.Unit == UnitYear {
			if months > 9999 {
				return Date{}, ErrDateOverflow
			}
			months *= 12
		}
		if months > 119987 {
			return Date{}, ErrDateOverflow
		}
		total := d.year*12 + int(d.month) - 1 + months
		y, m := total/12, time.Month(total%12+1)
		if y > 9999 {
			return Date{}, ErrDateOverflow
		}
		day := d.day
		if max := daysInMonth(y, m); day > max {
			day = max
		}
		return NewDate(y, m, day)
	default:
		return Date{}, ErrInvalidPolicy
	}
}
func daysInMonth(y int, m time.Month) int { return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day() }

type Unit string

const (
	UnitDay   Unit = "day"
	UnitWeek  Unit = "week"
	UnitMonth Unit = "month"
	UnitYear  Unit = "year"
)

type Mode string

const (
	ModeFixed           Mode = "fixed"
	ModeAfterCompletion Mode = "after_completion"
)

type Interval struct {
	Value int
	Unit  Unit
}
type Policy struct {
	Enabled  bool
	Interval Interval
	Mode     Mode
}
type Attention string

const (
	NeedsAttention Attention = "needs_attention"
	Upcoming       Attention = "upcoming"
)

func (p Policy) Validate() error {
	if !p.Enabled {
		return nil
	}
	if p.Interval.Value <= 0 {
		return ErrInvalidPolicy
	}
	switch p.Interval.Unit {
	case UnitDay, UnitWeek, UnitMonth, UnitYear:
	default:
		return ErrInvalidPolicy
	}
	if p.Mode != ModeFixed && p.Mode != ModeAfterCompletion {
		return ErrInvalidPolicy
	}
	return nil
}

// Cycle is a due date together with the day of month the recurring schedule is
// anchored to. On may be clamped to the last day of its month, while AnchorDay
// keeps the original day so the next occurrence can restore it.
type Cycle struct {
	On        Date
	AnchorDay int
}

// NewCycle validates a cycle: AnchorDay is 1..31, no earlier than On.Day(), and
// later than On.Day() only when On is the last day of its month (a clamp).
func NewCycle(on Date, anchorDay int) (Cycle, error) {
	c := Cycle{On: on, AnchorDay: anchorDay}
	if !c.Valid() {
		return Cycle{}, ErrInvalidDate
	}
	return c, nil
}

func (c Cycle) Valid() bool {
	if !c.On.Valid() || c.AnchorDay < 1 || c.AnchorDay > 31 || c.AnchorDay < c.On.Day() {
		return false
	}
	return c.AnchorDay == c.On.Day() || c.On.Day() == daysInMonth(c.On.Year(), c.On.Month())
}

func (c Cycle) String() string {
	if !c.Valid() {
		return ""
	}
	return fmt.Sprintf("%s anchor %d", c.On, c.AnchorDay)
}

// NextCycle computes the next cycle without mutating the supplied current cycle.
// With no current cycle, both modes initialize at completion+interval (including
// historical dates). Fluid mode always advances from completion and re-anchors on
// the completion day. Fixed month/year occurrences are computed directly as
// current month + k*interval with day min(AnchorDay, daysInMonth), clamped once per
// occurrence, so the anchor never drifts: Jan 31 -> Feb 28 -> Mar 31. Fixed
// day/week mode keeps day arithmetic and re-anchors on the resulting day.
func NextCycle(current *Cycle, completion Date, p Policy) (*Cycle, error) {
	if !completion.Valid() {
		return nil, ErrInvalidDate
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if !p.Enabled {
		return nil, nil
	}
	if current == nil {
		next, err := completion.AddInterval(p.Interval)
		if err != nil {
			return nil, err
		}
		return &Cycle{On: next, AnchorDay: anchorDayFor(p.Interval, next, completion)}, nil
	}
	if !current.Valid() {
		return nil, ErrInvalidDate
	}
	if p.Mode == ModeAfterCompletion {
		next, err := completion.AddInterval(p.Interval)
		if err != nil {
			return nil, err
		}
		return &Cycle{On: next, AnchorDay: anchorDayFor(p.Interval, next, completion)}, nil
	}
	if p.Interval.Unit == UnitDay || p.Interval.Unit == UnitWeek {
		next, err := current.On.AddInterval(p.Interval)
		if err != nil {
			return nil, err
		}
		for next.Compare(completion) <= 0 {
			next, err = next.AddInterval(p.Interval)
			if err != nil {
				return nil, err
			}
		}
		return &Cycle{On: next, AnchorDay: next.Day()}, nil
	}
	return nextFixedMonthOrYear(*current, completion, p.Interval)
}

// anchorDayFor picks the anchor of a cycle that starts from a completion date.
// Day/week cycles re-anchor on the resulting day; month/year cycles keep the
// completion day so a clamped month restores the later anchor.
func anchorDayFor(interval Interval, next, completion Date) int {
	if interval.Unit == UnitDay || interval.Unit == UnitWeek {
		return next.Day()
	}
	return completion.Day()
}

// nextFixedMonthOrYear returns the smallest occurrence k >= 1 strictly after
// completion. The occurrence month is current month + k*interval, clamped once to
// min(AnchorDay, daysInMonth), so a completion that skips occurrences cannot drift.
func nextFixedMonthOrYear(current Cycle, completion Date, interval Interval) (*Cycle, error) {
	months := interval.Value
	if interval.Unit == UnitYear {
		if months > 9999 {
			return nil, ErrDateOverflow
		}
		months *= 12
	}
	if months > 119987 {
		return nil, ErrDateOverflow
	}
	base := current.On.Year()*12 + int(current.On.Month()) - 1
	target := completion.Year()*12 + int(completion.Month()) - 1
	k := 1
	if target > base {
		k = (target - base) / months
		if k < 1 {
			k = 1
		}
		occurrence, err := monthOccurrence(current.AnchorDay, base, k*months)
		if err != nil {
			return nil, err
		}
		if occurrence.On.Compare(completion) <= 0 {
			k++
		}
	}
	return monthOccurrence(current.AnchorDay, base, k*months)
}

func monthOccurrence(anchorDay, base, offset int) (*Cycle, error) {
	total := base + offset
	year, month := total/12, time.Month(total%12+1)
	if year > 9999 {
		return nil, ErrDateOverflow
	}
	day := anchorDay
	if last := daysInMonth(year, month); day > last {
		day = last
	}
	on, err := NewDate(year, month, day)
	if err != nil {
		return nil, ErrDateOverflow
	}
	return &Cycle{On: on, AnchorDay: anchorDay}, nil
}

func DeriveAttention(attentionOn *Date, today Date) (Attention, error) {
	if !today.Valid() {
		return "", ErrInvalidDate
	}
	if attentionOn == nil {
		return NeedsAttention, nil
	}
	if !attentionOn.Valid() {
		return "", ErrInvalidDate
	}
	if attentionOn.Compare(today) <= 0 {
		return NeedsAttention, nil
	}
	return Upcoming, nil
}

// BusinessDate converts an instant to its calendar date in an IANA timezone.
func BusinessDate(at time.Time, zone string) (Date, error) {
	if zone == "" || zone == "Local" {
		return Date{}, fmt.Errorf("invalid IANA timezone %q", zone)
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return Date{}, err
	}
	local := at.In(loc)
	return NewDate(local.Year(), local.Month(), local.Day())
}
