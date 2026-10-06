package main

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/household"
	subjectapp "github.com/bigtcze/tendo/backend/internal/subject"
)

// subjectMembership adapts a household membership lookup to the subject
// module's Authorizer. A missing household or membership becomes
// subject.ErrNotFound; any other failure stays an infrastructure error and is
// never reported as not found.
func subjectMembership(get func(ctx context.Context, userID, householdID string) (household.Household, error)) subjectapp.Authorizer {
	return func(ctx context.Context, userID, householdID string) error {
		_, err := get(ctx, userID, householdID)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, household.ErrNotFound):
			return subjectapp.ErrNotFound
		default:
			return subjectapp.ErrUnavailable
		}
	}
}
