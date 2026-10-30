package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestMalformedIdentifiersAreRejectedBeforeQuery(t *testing.T) {
	repo := NewRepository(nil)
	ctx := context.Background()
	good := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61"
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
	_, _, createErr := repo.CreateIdempotent(context.Background(), "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b60", "k", "n", subject.TypePerson, &bad, nil)
	_, updateErr := repo.Update(context.Background(), "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b61", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b70", 1, subject.Patch{LinkedUserID: &bad, LinkedUserIDSet: true})
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

func TestConstraintErrorClassificationCharacterization(t *testing.T) {
	for _, tc := range []struct {
		name, code, constraint, validationCode string
		createWant, updateWant                 error
	}{
		{"linked membership", "23503", "subjects_linked_membership_fk", "invalid_reference", nil, nil},
		{"person only", "23514", "subjects_linked_user_person_only", "requires_person", nil, nil},
		{"other foreign key", "23503", "other_constraint", "", subject.ErrNotFound, errPersistence},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mapped := classifyConstraintError(&pgconn.PgError{Code: tc.code, ConstraintName: tc.constraint})
			if tc.validationCode != "" {
				var validation *subject.ValidationError
				if !errors.As(mapped, &validation) || validation.Field != "linkedUserId" || validation.Code != tc.validationCode {
					t.Fatalf("mapped error=%v", mapped)
				}
				updateMapped := updateConstraintError(&pgconn.PgError{Code: tc.code, ConstraintName: tc.constraint})
				if !errors.As(updateMapped, &validation) || validation.Field != "linkedUserId" || validation.Code != tc.validationCode {
					t.Fatalf("update mapped error=%v", updateMapped)
				}
				return
			}
			if !errors.Is(mapped, tc.createWant) {
				t.Fatalf("create mapping=%v want=%v", mapped, tc.createWant)
			}
			if got := updateConstraintError(&pgconn.PgError{Code: tc.code, ConstraintName: tc.constraint}); got != nil {
				t.Fatalf("update mapping=%v, want fallback %v", got, tc.updateWant)
			}
		})
	}
}
func ptrSubject(s string) *string { return &s }
