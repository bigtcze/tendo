package item_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/item/itemtest"
)

const candidateID = "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62"

func TestResponsibleMemberValidationAndPatchSemantics(t *testing.T) {
	repo := itemtest.New()
	var lookups []string
	members := func(_ context.Context, userID, household string) error {
		lookups = append(lookups, userID+"/"+household)
		if userID == candidateID || userID == "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b65" {
			return nil
		}
		if userID == "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63" {
			return item.ErrNotFound
		}
		return item.ErrUnavailable
	}
	svc := item.NewService(repo,
		func(context.Context, string, string) (string, error) { return "UTC", nil },
		func(context.Context, string, string, string) (bool, error) { return false, nil },
		members,
		func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) })

	upper := "0198A2F0-7C1E-7A53-9B0E-5D3F2C1A4B62"
	created, err := svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: subjectID, Title: "Assigned", ResponsibleUserID: &upper})
	if err != nil || created.ResponsibleUserID == nil || *created.ResponsibleUserID != candidateID {
		t.Fatalf("create canonicalized assignment=%+v err=%v", created.ResponsibleUserID, err)
	}
	id := created.ID
	otherMember := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b65"
	assigned, err := svc.Update(context.Background(), userID, householdID, id, created.Version, item.Patch{ResponsibleUserID: item.Some(otherMember)})
	if err != nil || assigned.Version != created.Version+1 || assigned.ResponsibleUserID == nil || *assigned.ResponsibleUserID != otherMember {
		t.Fatalf("assignment update=%+v err=%v", assigned, err)
	}
	preserved, err := svc.Update(context.Background(), userID, householdID, id, assigned.Version, item.Patch{Title: ptr("Renamed")})
	if err != nil || preserved.ResponsibleUserID == nil || *preserved.ResponsibleUserID != otherMember {
		t.Fatalf("omitted patch changed assignment: %+v err=%v", preserved.ResponsibleUserID, err)
	}
	cleared, err := svc.Update(context.Background(), userID, householdID, id, preserved.Version, item.Patch{ResponsibleUserID: item.Null[string]()})
	if err != nil || cleared.ResponsibleUserID != nil || cleared.Version != preserved.Version+1 {
		t.Fatalf("clear=%+v err=%v", cleared, err)
	}

	for _, bad := range []string{" bad", "not-a-uuid", "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63"} {
		_, err := svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: subjectID, Title: "Bad", ResponsibleUserID: &bad})
		validation(t, err, "responsibleUserId", "invalid_reference")
	}
	bad := "0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b64"
	_, err = svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: subjectID, Title: "Outage", ResponsibleUserID: &bad})
	if !errors.Is(err, item.ErrUnavailable) {
		t.Fatalf("member lookup outage=%v", err)
	}
	if len(lookups) != 4 {
		t.Fatalf("member lookups=%v", lookups)
	}
}

func TestInvalidSubjectWinsBeforeResponsibleMemberLookup(t *testing.T) {
	called := false
	svc := item.NewService(itemtest.New(),
		func(context.Context, string, string) (string, error) { return "UTC", nil },
		func(context.Context, string, string, string) (bool, error) { return false, item.ErrNotFound },
		func(context.Context, string, string) error { called = true; return errors.New("must not run") },
		func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) })
	responsible := candidateID
	_, err := svc.Create(context.Background(), userID, householdID, item.NewItem{SubjectID: missingID, Title: "Invalid subject", ResponsibleUserID: &responsible})
	validation(t, err, "subjectId", "invalid_reference")
	if called {
		t.Fatal("responsible lookup ran before subject validation")
	}
}
