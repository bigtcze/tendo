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
		next, err := schedule.NextCycle(attentionCycle(current), completed, policyOrDisabled(current.Recurrence))
		if errors.Is(err, schedule.ErrDateOverflow) {
			return CompletionPlan{}, &ValidationError{"recurrence", "date_overflow"}
		}
		if err != nil {
			return CompletionPlan{}, ErrUnavailable
		}
		return planCompletion(current, completed, next, userID, householdID, itemID, key, fingerprint), nil
	}
}

// attentionCycle rebuilds the current cycle from the stored date and anchor.
// Items seeded without an anchor (or legacy data) fall back to the attention day,
// which is always a valid anchor for that date.
func attentionCycle(current Item) *schedule.Cycle {
	if current.AttentionOn == nil {
		return nil
	}
	if current.AttentionAnchorDay != nil {
		if cycle, err := schedule.NewCycle(*current.AttentionOn, *current.AttentionAnchorDay); err == nil {
			return &cycle
		}
	}
	cycle, err := schedule.NewCycle(*current.AttentionOn, current.AttentionOn.Day())
	if err != nil {
		return nil
	}
	return &cycle
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

func planCompletion(current Item, completed schedule.Date, next *schedule.Cycle, userID, householdID, itemID, key string, fingerprint [32]byte) CompletionPlan {
	receipt := Completion{
		HouseholdID: householdID, ItemID: itemID, CompletedByUserID: userID,
		CompletedOn: completed, CycleAttentionOn: current.AttentionOn,
		CycleAnchorDay: current.AttentionAnchorDay,
		Recurrence:     current.Recurrence, PriorWorkflowState: current.WorkflowState,
		NextAttentionOn: nextDate(next), NextAnchorDay: nextAnchor(next),
		ItemVersionBefore: current.Version,
		IdempotencyKey:    key, Fingerprint: fingerprint,
	}
	updated := current
	if current.Recurrence != nil {
		updated.AttentionOn, updated.AttentionAnchorDay = nextDate(next), nextAnchor(next)
		updated.WorkflowState, updated.Done = StateOpen, false
	} else {
		updated.Done = true
	}
	updated.LastCompletedOn = &completed
	updated.Version++
	return CompletionPlan{Receipt: receipt, Item: updated}
}

func nextDate(next *schedule.Cycle) *schedule.Date {
	if next == nil {
		return nil
	}
	on := next.On
	return &on
}

func nextAnchor(next *schedule.Cycle) *int {
	if next == nil {
		return nil
	}
	anchor := next.AnchorDay
	return &anchor
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
