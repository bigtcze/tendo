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

// NextCycle computes the next date without mutating the supplied current anchor.
// With no anchor, both modes initialize at completion+interval (including historical dates).
// Fixed mode advances consecutively from the anchor, clamping after each addition, until
// strictly after completion; this intentionally makes Jan 31 monthly anchors drift to Feb 28 then Mar 28.
func NextCycle(anchor *Date, completion Date, p Policy) (*Date, error) {
	if !completion.Valid() {
		return nil, ErrInvalidDate
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if !p.Enabled {
		return nil, nil
	}
	if anchor == nil {
		d, err := completion.AddInterval(p.Interval)
		if err != nil {
			return nil, err
		}
		return &d, nil
	}
	if !anchor.Valid() {
		return nil, ErrInvalidDate
	}
	if p.Mode == ModeAfterCompletion {
		d, err := completion.AddInterval(p.Interval)
		if err != nil {
			return nil, err
		}
		return &d, nil
	}
	next, err := anchor.AddInterval(p.Interval)
	if err != nil {
		return nil, err
	}
	for next.Compare(completion) <= 0 {
		next, err = next.AddInterval(p.Interval)
		if err != nil {
			return nil, err
		}
	}
	return &next, nil
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
