package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/jackc/pgx/v5/pgtype"
)

type receiptDecodeValues struct {
	id, household, itemID, userID string
	completed, cycle, next        pgtype.Date
	interval                      pgtype.Int4
	unit, mode                    pgtype.Text
	state, key                    string
	fingerprint                   []byte
	version                       int64
	created                       pgtype.Timestamptz
}

func validReceiptValues() receiptDecodeValues {
	date := func(year int, month time.Month, day int) pgtype.Date {
		return pgtype.Date{Time: time.Date(year, month, day, 0, 0, 0, 0, time.UTC), Valid: true}
	}
	return receiptDecodeValues{
		id: "0198a2f0-7c1e-7a53-9b0e-000000000001", household: "0198a2f0-7c1e-7a53-9b0e-000000000002",
		itemID: "0198a2f0-7c1e-7a53-9b0e-000000000003", userID: "0198a2f0-7c1e-7a53-9b0e-000000000004",
		completed: date(2026, time.March, 1), cycle: date(2026, time.March, 2), next: date(2026, time.March, 3),
		state: string(item.StatePaused), key: "valid-key", fingerprint: make([]byte, sha256Size), version: 2,
		created: pgtype.Timestamptz{Time: time.Date(2026, time.March, 1, 1, 2, 3, 0, time.FixedZone("offset", 3600)), Valid: true, InfinityModifier: pgtype.Finite},
	}
}

func (v receiptDecodeValues) decode() (item.Completion, error) {
	return decodeReceipt(v.id, v.household, v.itemID, v.userID, v.completed, v.cycle, v.next, v.interval, v.unit, v.mode, v.state, v.key, v.fingerprint, v.version, v.created)
}

func TestDecodeReceiptStoredVariants(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cycleNull  bool
		recurrence *schedule.Policy
	}{
		{name: "one-off with cycle"},
		{name: "initialization without cycle", cycleNull: true},
		{name: "fixed recurrence", recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 2, Unit: schedule.UnitWeek}, Mode: schedule.ModeFixed}},
		{name: "fluid recurrence", recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 3, Unit: schedule.UnitMonth}, Mode: schedule.ModeAfterCompletion}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := validReceiptValues()
			if tc.cycleNull {
				values.cycle = pgtype.Date{}
			}
			recurrence := tc.recurrence
			if tc.cycleNull && recurrence == nil {
				recurrence = &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}
			}
			if recurrence != nil {
				values.interval = pgtype.Int4{Int32: int32(recurrence.Interval.Value), Valid: true}
				values.unit = pgtype.Text{String: string(recurrence.Interval.Unit), Valid: true}
				values.mode = pgtype.Text{String: string(recurrence.Mode), Valid: true}
			}
			got, err := values.decode()
			if err != nil {
				t.Fatal(err)
			}
			if (got.Recurrence == nil) != (recurrence == nil) || (recurrence != nil && *got.Recurrence != *recurrence) {
				t.Fatalf("recurrence=%+v want=%+v", got.Recurrence, tc.recurrence)
			}
			if (got.CycleAttentionOn == nil) != tc.cycleNull {
				t.Fatalf("cycle=%+v cycleNull=%v", got.CycleAttentionOn, tc.cycleNull)
			}
			if got.CreatedAt.Location() != time.UTC || got.CreatedAt.Hour() != 0 || got.PriorWorkflowState != item.StatePaused {
				t.Fatalf("decoded stored values=%+v", got)
			}
		})
	}
}

func TestDecodeReceiptRejectsMalformedStoredRows(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*receiptDecodeValues)
	}{
		{"short fingerprint", func(v *receiptDecodeValues) { v.fingerprint = v.fingerprint[:sha256Size-1] }},
		{"zero prior version", func(v *receiptDecodeValues) { v.version = 0 }},
		{"invalid key", func(v *receiptDecodeValues) { v.key = "bad key" }},
		{"bad uuid", func(v *receiptDecodeValues) { v.id = "not-a-uuid" }},
		{"infinite created time", func(v *receiptDecodeValues) { v.created.InfinityModifier = pgtype.Infinity }},
		{"missing completion date", func(v *receiptDecodeValues) { v.completed = pgtype.Date{} }},
		{"infinite cycle date", func(v *receiptDecodeValues) { v.cycle.InfinityModifier = pgtype.Infinity }},
		{"partial recurrence", func(v *receiptDecodeValues) { v.interval = pgtype.Int4{Int32: 1, Valid: true} }},
		{"invalid recurrence", func(v *receiptDecodeValues) {
			v.interval = pgtype.Int4{Int32: 1000, Valid: true}
			v.unit = pgtype.Text{String: "day", Valid: true}
			v.mode = pgtype.Text{String: "fixed", Valid: true}
		}},
		{"unknown workflow", func(v *receiptDecodeValues) { v.state = "unknown" }},
		{"cycle-free next without recurrence", func(v *receiptDecodeValues) { v.cycle = pgtype.Date{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := validReceiptValues()
			tc.change(&values)
			if _, err := values.decode(); !errors.Is(err, errPersistence) {
				t.Fatalf("malformed row err=%v, want persistence error", err)
			}
		})
	}
}
