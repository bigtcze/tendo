package postgres

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	characterizationHousehold = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
	characterizationActor     = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60"
	characterizationSubject   = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70"
)

type faultDB struct {
	rowErrors []error
}

func (db *faultDB) Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (db *faultDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}
func (db *faultDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	if len(db.rowErrors) == 0 {
		return faultRow{err: errors.New("unexpected QueryRow")}
	}
	err := db.rowErrors[0]
	db.rowErrors = db.rowErrors[1:]
	return faultRow{err: err}
}

type faultRow struct{ err error }

func (r faultRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for _, value := range dest {
		v := reflect.ValueOf(value)
		if v.Kind() != reflect.Pointer || v.IsNil() {
			return errors.New("invalid scan destination")
		}
		v.Elem().Set(reflect.Zero(v.Elem().Type()))
	}
	return nil
}

func TestMalformedIdentifiersAreRejectedBeforeQuery(t *testing.T) {
	repo := NewRepository(nil)
	ctx := context.Background()
	good := characterizationHousehold
	bad := "not-a-uuid"
	checks := []struct {
		name string
		call func() error
	}{
		{"create household", func() error {
			_, _, e := repo.CreateIdempotent(ctx, bad, good, "k", "n", subject.TypeHome, nil, nil)
			return e
		}},
		{"create actor", func() error {
			_, _, e := repo.CreateIdempotent(ctx, good, bad, "k", "n", subject.TypeHome, nil, nil)
			return e
		}},
		{"find household", func() error { _, _, e := repo.FindCreation(ctx, bad, good, "k"); return e }},
		{"find actor", func() error { _, _, e := repo.FindCreation(ctx, good, bad, "k"); return e }},
		{"get household", func() error { _, e := repo.Get(ctx, bad, good); return e }},
		{"get subject", func() error { _, e := repo.Get(ctx, good, bad); return e }},
		{"list household", func() error { _, e := repo.List(ctx, bad, false, good, 1); return e }},
		{"list after", func() error { _, e := repo.List(ctx, good, false, bad, 1); return e }},
		{"list ids household", func() error { _, e := repo.ListByIDs(ctx, bad, false, nil, good, 1); return e }},
		{"list ids after", func() error { _, e := repo.ListByIDs(ctx, good, false, nil, bad, 1); return e }},
		{"list ids subject", func() error { _, e := repo.ListByIDs(ctx, good, false, []string{bad}, good, 1); return e }},
		{"update household", func() error { _, e := repo.Update(ctx, bad, good, 1, subject.Patch{}); return e }},
		{"update subject", func() error { _, e := repo.Update(ctx, good, bad, 1, subject.Patch{}); return e }},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, subject.ErrNotFound) {
				t.Fatalf("err=%v, want ErrNotFound", err)
			}
		})
	}
}

func TestMalformedLinkedUserIsValidationErrorBeforeQuery(t *testing.T) {
	repo := NewRepository(nil)
	bad := "bad"
	_, _, createErr := repo.CreateIdempotent(context.Background(), characterizationHousehold, characterizationActor, "k", "n", subject.TypePerson, &bad, nil)
	_, updateErr := repo.Update(context.Background(), characterizationHousehold, characterizationSubject, 1, subject.Patch{LinkedUserID: &bad, LinkedUserIDSet: true})
	for _, err := range []error{createErr, updateErr} {
		var validation *subject.ValidationError
		if !errors.As(err, &validation) || validation.Field != "linkedUserId" || validation.Code != "invalid_reference" {
			t.Errorf("err=%v, want linkedUserId/invalid_reference", err)
		}
	}
}

func TestToSubjectCharacterization(t *testing.T) {
	instant := time.Date(2026, 10, 30, 12, 34, 56, 789, time.FixedZone("east", 3*60*60))
	for _, tc := range []struct {
		name string
		link pgtype.UUID
		want *string
	}{
		{"null link", pgtype.UUID{}, nil},
		{"valid link", pgtype.UUID{Bytes: [16]byte{0x01, 0x98, 0xa2, 0xf0, 0x7c, 0x1e, 0x7a, 0x53, 0x9b, 0x0e, 0x5d, 0x3f, 0x2c, 0x1a, 0x4b, 0x62}, Valid: true}, ptrSubject("0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := toSubject(row{ID: "i", HouseholdID: "h", Type: "person", Name: "n", LinkedUserID: tc.link, CreatedAt: pgtype.Timestamptz{Time: instant, Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: instant, Valid: true}})
			if (got.LinkedUserID == nil) != (tc.want == nil) || tc.want != nil && *got.LinkedUserID != *tc.want {
				t.Fatalf("link=%v want=%v", got.LinkedUserID, tc.want)
			}
			if got.CreatedAt.Location() != time.UTC || !got.CreatedAt.Equal(instant) || got.UpdatedAt.Location() != time.UTC {
				t.Fatalf("timestamps=%v, %v", got.CreatedAt, got.UpdatedAt)
			}
		})
	}
}

func TestRepositoryCreateConstraintAndFailureMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"membership FK", &pgconn.PgError{Code: "23503", ConstraintName: "subjects_linked_membership_fk"}, &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}},
		{"person check", &pgconn.PgError{Code: "23514", ConstraintName: "subjects_linked_user_person_only"}, &subject.ValidationError{Field: "linkedUserId", Code: "requires_person"}},
		{"other FK", &pgconn.PgError{Code: "23503", ConstraintName: "other_constraint"}, subject.ErrNotFound},
		{"generic", errors.New("database down"), errPersistence},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewRepository(&faultDB{rowErrors: []error{tc.err}})
			_, _, err := repo.CreateIdempotent(context.Background(), characterizationHousehold, characterizationActor, "k", "n", subject.TypePerson, nil, nil)
			assertRepositoryError(t, err, tc.want)
		})
	}
	t.Run("no rows returns full zero tuple", func(t *testing.T) {
		got, inserted, err := NewRepository(&faultDB{rowErrors: []error{pgx.ErrNoRows}}).CreateIdempotent(context.Background(), characterizationHousehold, characterizationActor, "k", "n", subject.TypePerson, nil, nil)
		if got != (subject.Subject{}) || inserted || err != nil {
			t.Fatalf("got=%+v inserted=%v err=%v", got, inserted, err)
		}
	})
}

func TestRepositoryUpdateConstraintAndFailureMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"membership FK", &pgconn.PgError{Code: "23503", ConstraintName: "subjects_linked_membership_fk"}, &subject.ValidationError{Field: "linkedUserId", Code: "invalid_reference"}},
		{"person check", &pgconn.PgError{Code: "23514", ConstraintName: "subjects_linked_user_person_only"}, &subject.ValidationError{Field: "linkedUserId", Code: "requires_person"}},
		{"other FK", &pgconn.PgError{Code: "23503", ConstraintName: "other_constraint"}, errPersistence},
		{"generic", errors.New("database down"), errPersistence},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewRepository(&faultDB{rowErrors: []error{tc.err}})
			_, err := repo.Update(context.Background(), characterizationHousehold, characterizationSubject, 1, subject.Patch{})
			assertRepositoryError(t, err, tc.want)
		})
	}
}

func TestRepositoryUpdateNoRowsProbeMapping(t *testing.T) {
	for _, tc := range []struct {
		name           string
		probeErr, want error
	}{
		{"missing", pgx.ErrNoRows, subject.ErrNotFound},
		{"stale", nil, subject.ErrVersionMismatch},
		{"probe failure", errors.New("database down"), errPersistence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := NewRepository(&faultDB{rowErrors: []error{pgx.ErrNoRows, tc.probeErr}})
			_, err := repo.Update(context.Background(), characterizationHousehold, characterizationSubject, 1, subject.Patch{})
			assertRepositoryError(t, err, tc.want)
		})
	}
}

func assertRepositoryError(t *testing.T, got, want error) {
	t.Helper()
	var expectedValidation, actualValidation *subject.ValidationError
	if errors.As(want, &expectedValidation) {
		if !errors.As(got, &actualValidation) || actualValidation.Field != expectedValidation.Field || actualValidation.Code != expectedValidation.Code {
			t.Fatalf("got=%v want validation %s/%s", got, expectedValidation.Field, expectedValidation.Code)
		}
		return
	}
	if !errors.Is(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func ptrSubject(s string) *string { return &s }
