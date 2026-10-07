package main

import (
	"context"
	"errors"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
	itemapp "github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/subject"
)

func TestItemHouseholdsReturnsTimezoneAndMapsErrors(t *testing.T) {
	var gotUser, gotHousehold string
	lookup := func(h household.Household, result error) func(context.Context, string, string) (household.Household, error) {
		return func(_ context.Context, userID, householdID string) (household.Household, error) {
			gotUser, gotHousehold = userID, householdID
			return h, result
		}
	}
	zone, err := itemHouseholds(lookup(household.Household{Timezone: "Europe/Prague"}, nil))(context.Background(), "user-1", "household-1")
	if err != nil || zone != "Europe/Prague" || gotUser != "user-1" || gotHousehold != "household-1" {
		t.Fatalf("zone=%q err=%v user=%q household=%q", zone, err, gotUser, gotHousehold)
	}
	for _, tc := range []struct {
		name string
		in   error
		want error
	}{
		{"not found", household.ErrNotFound, itemapp.ErrNotFound},
		{"wrapped not found", errors.Join(errors.New("ctx"), household.ErrNotFound), itemapp.ErrNotFound},
		{"infrastructure failure", errors.New("pq: connection refused"), itemapp.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			zone, err := itemHouseholds(lookup(household.Household{Timezone: "Europe/Prague"}, tc.in))(context.Background(), "user-1", "household-1")
			if !errors.Is(err, tc.want) || zone != "" {
				t.Fatalf("zone=%q err=%v want %v", zone, err, tc.want)
			}
			if tc.want == itemapp.ErrUnavailable && errors.Is(err, itemapp.ErrNotFound) {
				t.Fatal("infrastructure failure must not be reported as not found")
			}
		})
	}
}

func TestItemSubjectsReturnsArchivedAndMapsErrors(t *testing.T) {
	var got [3]string
	lookup := func(s subject.Subject, result error) func(context.Context, string, string, string) (subject.Subject, error) {
		return func(_ context.Context, userID, householdID, subjectID string) (subject.Subject, error) {
			got = [3]string{userID, householdID, subjectID}
			return s, result
		}
	}
	for _, archived := range []bool{false, true} {
		isArchived, err := itemSubjects(lookup(subject.Subject{Archived: archived}, nil))(context.Background(), "user-1", "household-1", "subject-1")
		if err != nil || isArchived != archived || got != [3]string{"user-1", "household-1", "subject-1"} {
			t.Fatalf("archived=%v got=%v err=%v args=%v", archived, isArchived, err, got)
		}
	}
	for _, tc := range []struct {
		name string
		in   error
		want error
	}{
		{"not found", subject.ErrNotFound, itemapp.ErrNotFound},
		{"wrapped not found", errors.Join(errors.New("ctx"), subject.ErrNotFound), itemapp.ErrNotFound},
		{"subject unavailable", subject.ErrUnavailable, itemapp.ErrUnavailable},
		{"infrastructure failure", errors.New("pq: connection refused"), itemapp.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archived, err := itemSubjects(lookup(subject.Subject{Archived: true}, tc.in))(context.Background(), "u", "h", "s")
			if !errors.Is(err, tc.want) || archived {
				t.Fatalf("archived=%v err=%v want %v", archived, err, tc.want)
			}
			if tc.want == itemapp.ErrUnavailable && errors.Is(err, itemapp.ErrNotFound) {
				t.Fatal("infrastructure failure must not be reported as not found")
			}
		})
	}
}
