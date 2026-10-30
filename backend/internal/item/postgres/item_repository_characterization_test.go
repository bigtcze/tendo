package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/jackc/pgx/v5/pgtype"
)

// These cases are rejected before any query runs, so the repository needs no
// database. They pin pre-query error mapping in List and Update.
const (
	characterizationHousehold = "0198a3f3-0000-7000-8000-000000000001"
	characterizationItem      = "0198a3f3-0000-7000-8000-000000000002"
	characterizationAfter     = "00000000-0000-0000-0000-000000000000"
)

func TestRepositoryListRejectsUnknownSortBeforeQuerying(t *testing.T) {
	repo := NewRepository(nil)
	_, err := repo.List(context.Background(), characterizationHousehold, item.ListOptions{Sort: item.ListSort("bogus"), AfterID: characterizationAfter, Limit: 5})
	if !errors.Is(err, item.ErrUnavailable) {
		t.Fatalf("unknown sort err=%v, want ErrUnavailable", err)
	}
}

func TestRepositoryListAndUpdateMapMalformedIdentifiers(t *testing.T) {
	repo := NewRepository(nil)
	ctx := context.Background()
	bad := "not-a-uuid"
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"list household", listErr(repo.List(ctx, bad, item.ListOptions{AfterID: characterizationAfter})), item.ErrNotFound},
		{"list after", listErr(repo.List(ctx, characterizationHousehold, item.ListOptions{AfterID: bad})), item.ErrNotFound},
		{"list empty after", listErr(repo.List(ctx, characterizationHousehold, item.ListOptions{})), item.ErrNotFound},
		{"list subject filter", listErr(repo.List(ctx, characterizationHousehold, item.ListOptions{AfterID: characterizationAfter, Filters: item.ListFilters{SubjectID: bad}})), item.ErrNotFound},
		{"list responsible filter", listErr(repo.List(ctx, characterizationHousehold, item.ListOptions{AfterID: characterizationAfter, Filters: item.ListFilters{ResponsibleUserID: item.Some(bad)}})), item.ErrNotFound},
		{"update household", updateErr(repo.Update(ctx, bad, characterizationItem, 1, item.Change{})), item.ErrNotFound},
		{"update item", updateErr(repo.Update(ctx, characterizationHousehold, bad, 1, item.Change{})), item.ErrNotFound},
		{"update subject", updateErr(repo.Update(ctx, characterizationHousehold, characterizationItem, 1, item.Change{SubjectID: &bad})), item.ErrInvalidReference},
		{"update responsible", updateErr(repo.Update(ctx, characterizationHousehold, characterizationItem, 1, item.Change{ResponsibleUserID: item.Some(bad)})), item.ErrInvalidResponsibleReference},
	}
	for _, tc := range cases {
		if !errors.Is(tc.err, tc.want) {
			t.Errorf("%s: err=%v, want %v", tc.name, tc.err, tc.want)
		}
	}
}

func TestToItemRejectsOutOfRangeStoredRecurrenceInterval(t *testing.T) {
	for _, value := range []int32{0, 1000} {
		_, err := toItem(row{
			RecurrenceIntervalValue: pgtype.Int4{Int32: value, Valid: true},
			RecurrenceIntervalUnit:  pgtype.Text{String: "day", Valid: true},
			RecurrenceMode:          pgtype.Text{String: "fixed", Valid: true},
		})
		if !errors.Is(err, errPersistence) {
			t.Errorf("interval %d err=%v, want errPersistence", value, err)
		}
	}
	if _, err := toItem(row{
		RecurrenceIntervalValue: pgtype.Int4{Int32: 999, Valid: true},
		RecurrenceIntervalUnit:  pgtype.Text{String: "year", Valid: true},
		RecurrenceMode:          pgtype.Text{String: "after_completion", Valid: true},
	}); err != nil {
		t.Errorf("interval 999 err=%v", err)
	}
}

func listErr(_ []item.Item, err error) error { return err }

func updateErr(_ item.Item, err error) error { return err }
