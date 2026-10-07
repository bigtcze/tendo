package main

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/household"
	itemapp "github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/subject"
)

// itemHouseholds adapts a household lookup to the item module's Households
// function. A missing household or membership becomes item.ErrNotFound; any
// other failure stays an infrastructure error and is never reported as not
// found.
func itemHouseholds(get func(ctx context.Context, userID, householdID string) (household.Household, error)) itemapp.Households {
	return func(ctx context.Context, userID, householdID string) (string, error) {
		h, err := get(ctx, userID, householdID)
		switch {
		case err == nil:
			return h.Timezone, nil
		case errors.Is(err, household.ErrNotFound):
			return "", itemapp.ErrNotFound
		default:
			return "", itemapp.ErrUnavailable
		}
	}
}

// itemSubjects adapts a subject lookup to the item module's Subjects function.
// An absent subject becomes item.ErrNotFound; any other failure is an
// infrastructure error.
func itemSubjects(get func(ctx context.Context, userID, householdID, subjectID string) (subject.Subject, error)) itemapp.Subjects {
	return func(ctx context.Context, userID, householdID, subjectID string) (bool, error) {
		s, err := get(ctx, userID, householdID, subjectID)
		switch {
		case err == nil:
			return s.Archived, nil
		case errors.Is(err, subject.ErrNotFound):
			return false, itemapp.ErrNotFound
		default:
			return false, itemapp.ErrUnavailable
		}
	}
}
