package postgres

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/postgres/dbgen"
	"github.com/bigtcze/tendo/backend/internal/schedule"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestUpdateParamsPreserveAbsentAndExplicitClear(t *testing.T) {
	hid := "0198a3f3-0000-7000-8000-000000000001"
	iid := "0198a3f3-0000-7000-8000-000000000002"
	params, err := updateParams(hid, iid, 7, item.Change{})
	if err != nil {
		t.Fatal(err)
	}
	if params.SetResponsibleUserID || params.SetNotes || params.SetAttentionOn || params.SetRecurrence {
		t.Fatalf("absent fields marked for update: %+v", params)
	}

	params, err = updateParams(hid, iid, 7, item.Change{
		ResponsibleUserID: item.Null[string](),
		Notes:             item.Null[string](),
		AttentionOn:       item.Null[schedule.Date](),
		Recurrence:        item.Null[schedule.Policy](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !params.SetResponsibleUserID || params.ResponsibleUserID.Valid ||
		!params.SetNotes || params.Notes.Valid ||
		!params.SetAttentionOn || params.AttentionOn.Valid ||
		!params.SetRecurrence || params.RecurrenceIntervalValue.Valid ||
		params.RecurrenceIntervalUnit.Valid || params.RecurrenceMode.Valid {
		t.Fatalf("explicit clears not represented as SQL NULL: %+v", params)
	}
}

func TestListParamsMapFilterNullability(t *testing.T) {
	hid := "0198a3f3-0000-7000-8000-000000000001"
	after := "0198a3f3-0000-7000-8000-000000000002"
	assignee := "0198a3f3-0000-7000-8000-000000000003"
	recurring := false
	params, err := listParams(hid, item.ListOptions{
		AfterID: after,
		Filters: item.ListFilters{
			ResponsibleUserID: item.Null[string](),
			Recurring:         &recurring,
			Q:                 "needle",
		},
		Limit: 23,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !params.FilterResponsible || params.ResponsibleUserID.Valid {
		t.Fatalf("unassigned filter=%+v", params)
	}
	if !params.Recurring.Valid || params.Recurring.Bool || !params.Q.Valid || params.Q.String != "needle" || params.RowLimit != 23 {
		t.Fatalf("mapped list options=%+v", params)
	}
	params, err = listParams(hid, item.ListOptions{AfterID: after, Filters: item.ListFilters{ResponsibleUserID: item.Some(assignee)}})
	if err != nil || !params.FilterResponsible || !params.ResponsibleUserID.Valid {
		t.Fatalf("assigned filter=%+v err=%v", params, err)
	}
	if _, err := listParams(hid, item.ListOptions{AfterID: after, Filters: item.ListFilters{SubjectID: "invalid"}}); !errors.Is(err, item.ErrNotFound) {
		t.Fatalf("invalid subject filter err=%v", err)
	}
}

func TestToItemMapsNullableFieldsAndRejectsPartialRecurrence(t *testing.T) {
	created := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.FixedZone("local", 3600))
	mapped, err := toItem(row{
		Notes:                   pgtype.Text{String: "notes", Valid: true},
		CreatedAt:               pgtype.Timestamptz{Time: created, Valid: true},
		UpdatedAt:               pgtype.Timestamptz{Time: created, Valid: true},
		RecurrenceIntervalValue: pgtype.Int4{Int32: 1, Valid: true},
		RecurrenceIntervalUnit:  pgtype.Text{String: "day", Valid: true},
		RecurrenceMode:          pgtype.Text{String: "fixed", Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Notes == nil || *mapped.Notes != "notes" || mapped.Recurrence == nil ||
		mapped.CreatedAt.Location() != time.UTC || mapped.UpdatedAt.Location() != time.UTC {
		t.Fatalf("mapped item=%+v", mapped)
	}
	if _, err := toItem(row{RecurrenceIntervalValue: pgtype.Int4{Int32: 1, Valid: true}}); !errors.Is(err, errPersistence) {
		t.Fatalf("partial recurrence err=%v", err)
	}
	if _, err := toItem(row{AttentionOn: pgtype.Date{InfinityModifier: pgtype.Infinity, Valid: true}}); !errors.Is(err, errPersistence) {
		t.Fatalf("infinite attention date err=%v", err)
	}
}

func TestRowAdaptersCopyEveryColumn(t *testing.T) {
	at := pgtype.Timestamptz{Time: time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC), Valid: true}
	later := pgtype.Timestamptz{Time: at.Time.Add(time.Hour), Valid: true}
	want := row{
		ID: "id", HouseholdID: "household", SubjectID: "subject", Title: "title",
		ResponsibleUserID:       pgtype.UUID{Bytes: [16]byte{1}, Valid: true},
		Notes:                   pgtype.Text{String: "notes", Valid: true},
		AttentionOn:             pgtype.Date{Time: time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC), Valid: true},
		RecurrenceIntervalValue: pgtype.Int4{Int32: 3, Valid: true},
		RecurrenceIntervalUnit:  pgtype.Text{String: "week", Valid: true},
		RecurrenceMode:          pgtype.Text{String: "fixed", Valid: true},
		WorkflowState:           "waiting", Archived: true, Done: true,
		LastCompletedOn: pgtype.Date{Time: time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC), Valid: true},
		CreatedAt:       at, UpdatedAt: later, Version: 9,
	}
	got := map[string]row{
		"list":      listRow(fillFromRow[dbgen.ListItemsRow](t, want)),
		"attention": attentionRow(fillFromRow[dbgen.ListItemsByAttentionRow](t, want)),
		"update":    updateRow(fillFromRow[dbgen.UpdateItemRow](t, want)),
		"create":    createdRow(fillFromRow[dbgen.CreateItemRow](t, want)),
		"get":       getRow(fillFromRow[dbgen.GetItemRow](t, want)),
		"creation":  findCreationRow(fillFromRow[dbgen.FindItemCreationRow](t, want)),
	}
	for name, mapped := range got {
		if mapped != want {
			t.Errorf("%s row=%+v, want %+v", name, mapped, want)
		}
	}
}

// fillFromRow copies every row column into the same-named field of a generated
// sqlc row, failing when a row column has no generated counterpart.
func fillFromRow[T any](t *testing.T, source row) T {
	t.Helper()
	var target T
	src := reflect.ValueOf(source)
	dst := reflect.ValueOf(&target).Elem()
	for i := 0; i < src.NumField(); i++ {
		field := dst.FieldByName(src.Type().Field(i).Name)
		if !field.IsValid() {
			t.Fatalf("%T has no %s column", target, src.Type().Field(i).Name)
		}
		field.Set(src.Field(i))
	}
	return target
}

func TestToItemDecodesStoredRecurrenceAndDates(t *testing.T) {
	interval := pgtype.Int4{Int32: 1, Valid: true}
	unit := pgtype.Text{String: "day", Valid: true}
	mode := pgtype.Text{String: "fixed", Valid: true}
	finite := time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		row  row
		ok   bool
	}{
		{"no recurrence", row{}, true},
		{"interval only", row{RecurrenceIntervalValue: interval}, false},
		{"unit only", row{RecurrenceIntervalUnit: unit}, false},
		{"mode only", row{RecurrenceMode: mode}, false},
		{"missing mode", row{RecurrenceIntervalValue: interval, RecurrenceIntervalUnit: unit}, false},
		{"missing unit", row{RecurrenceIntervalValue: interval, RecurrenceMode: mode}, false},
		{"missing interval", row{RecurrenceIntervalUnit: unit, RecurrenceMode: mode}, false},
		{"invalid unit", row{RecurrenceIntervalValue: interval, RecurrenceIntervalUnit: pgtype.Text{String: "hour", Valid: true}, RecurrenceMode: mode}, false},
		{"invalid mode", row{RecurrenceIntervalValue: interval, RecurrenceIntervalUnit: unit, RecurrenceMode: pgtype.Text{String: "bad", Valid: true}}, false},
		{"infinite attention", row{AttentionOn: pgtype.Date{Time: finite, InfinityModifier: pgtype.Infinity, Valid: true}}, false},
		{"infinite last completion keeps stored calendar date", row{LastCompletedOn: pgtype.Date{Time: finite, InfinityModifier: pgtype.Infinity, Valid: true}}, true},
		{"attention outside calendar bounds", row{AttentionOn: pgtype.Date{Time: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC), Valid: true}}, false},
		{"last completion outside calendar bounds", row{LastCompletedOn: pgtype.Date{Time: time.Date(0, time.January, 1, 0, 0, 0, 0, time.UTC), Valid: true}}, false},
	}
	for _, tc := range cases {
		mapped, err := toItem(tc.row)
		if tc.ok != (err == nil) {
			t.Errorf("%s: err=%v, want ok=%v", tc.name, err, tc.ok)
			continue
		}
		if err != nil && !errors.Is(err, errPersistence) {
			t.Errorf("%s: err=%v, want errPersistence", tc.name, err)
		}
		if tc.ok && tc.row.LastCompletedOn.Valid && (mapped.LastCompletedOn == nil || mapped.LastCompletedOn.String() != "2026-03-04") {
			t.Errorf("%s: last completed=%v", tc.name, mapped.LastCompletedOn)
		}
	}
}
