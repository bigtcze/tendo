package item

import (
	"errors"

	"github.com/bigtcze/tendo/backend/internal/schedule"
)

func completionDecider(
	userID, householdID, itemID string,
	expectedVersion int64,
	key string,
	fingerprint [32]byte,
	request CompletionRequest,
	today schedule.Date,
) CompletionDecider {
	return func(current Item, existing *Completion) (CompletionPlan, error) {
		if existing != nil {
			if existing.Fingerprint != fingerprint {
				return CompletionPlan{}, ErrIdempotencyKeyReused
			}
			return CompletionPlan{Receipt: *existing, Item: current}, nil
		}
		completed, err := requestedCompletionDate(request.CompletedOn, today)
		if err != nil {
			return CompletionPlan{}, err
		}
		if current.Version != expectedVersion {
			return CompletionPlan{}, ErrVersionMismatch
		}
		if current.Archived {
			return CompletionPlan{}, ErrArchived
		}
		if current.Done {
			return CompletionPlan{}, ErrDone
		}
		next, err := schedule.NextCycle(current.AttentionOn, completed, policyOrDisabled(current.Recurrence))
		if errors.Is(err, schedule.ErrDateOverflow) {
			return CompletionPlan{}, &ValidationError{"recurrence", "date_overflow"}
		}
		if err != nil {
			return CompletionPlan{}, ErrUnavailable
		}
		return planCompletion(current, completed, next, userID, householdID, itemID, key, fingerprint), nil
	}
}

func requestedCompletionDate(submitted *string, today schedule.Date) (schedule.Date, error) {
	if submitted == nil {
		return today, nil
	}
	completed, err := ParseDate(*submitted)
	if err != nil {
		return schedule.Date{}, &ValidationError{"completedOn", "invalid_date"}
	}
	if completed.Compare(today) > 0 {
		return schedule.Date{}, &ValidationError{"completedOn", "future_date"}
	}
	return completed, nil
}

func planCompletion(current Item, completed schedule.Date, next *schedule.Date, userID, householdID, itemID, key string, fingerprint [32]byte) CompletionPlan {
	receipt := Completion{
		HouseholdID: householdID, ItemID: itemID, CompletedByUserID: userID,
		CompletedOn: completed, CycleAttentionOn: current.AttentionOn,
		Recurrence: current.Recurrence, PriorWorkflowState: current.WorkflowState,
		NextAttentionOn: next, ItemVersionBefore: current.Version,
		IdempotencyKey: key, Fingerprint: fingerprint,
	}
	updated := current
	if current.Recurrence != nil {
		updated.AttentionOn, updated.WorkflowState, updated.Done = next, StateOpen, false
	} else {
		updated.Done = true
	}
	updated.LastCompletedOn = &completed
	updated.Version++
	return CompletionPlan{Receipt: receipt, Item: updated}
}

func completionServiceError(err error) error {
	if err == nil || errors.Is(err, ErrIdempotencyKeyReused) || errors.Is(err, ErrVersionMismatch) ||
		errors.Is(err, ErrNotFound) || errors.Is(err, ErrArchived) || errors.Is(err, ErrDone) {
		return err
	}
	var validation *ValidationError
	if errors.As(err, &validation) {
		return err
	}
	return ErrUnavailable
}
