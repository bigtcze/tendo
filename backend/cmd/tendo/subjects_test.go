package main

import (
	"context"
	"errors"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
	subjectapp "github.com/bigtcze/tendo/backend/internal/subject"
)

func TestSubjectMembershipMapsHouseholdErrors(t *testing.T) {
	var gotUser, gotHousehold string
	lookup := func(result error) func(context.Context, string, string) (household.Household, error) {
		return func(_ context.Context, userID, householdID string) (household.Household, error) {
			gotUser, gotHousehold = userID, householdID
			return household.Household{}, result
		}
	}
	for _, tc := range []struct {
		name string
		in   error
		want error
	}{
		{"member", nil, nil},
		{"not found", household.ErrNotFound, subjectapp.ErrNotFound},
		{"wrapped not found", errors.Join(errors.New("ctx"), household.ErrNotFound), subjectapp.ErrNotFound},
		{"infrastructure failure", errors.New("pq: connection refused"), subjectapp.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := subjectMembership(lookup(tc.in))(context.Background(), "user-1", "household-1")
			if tc.want == nil {
				if err != nil {
					t.Fatalf("err=%v", err)
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if tc.want == subjectapp.ErrUnavailable && errors.Is(err, subjectapp.ErrNotFound) {
				t.Fatal("infrastructure failure must not be reported as not found")
			}
			if gotUser != "user-1" || gotHousehold != "household-1" {
				t.Fatalf("lookup args user=%q household=%q", gotUser, gotHousehold)
			}
		})
	}
}
