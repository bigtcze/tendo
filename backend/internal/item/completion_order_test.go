package item_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

func TestCompleteValidationOrderCharacterization(t *testing.T) {
	e := completionEnv("UTC", "2026-03-01", item.Item{ID: missingID, Archived: true})
	for _, tc := range []struct {
		name                string
		date                string
		version             int64
		wantField, wantCode string
	}{
		{name: "invalid date before stale version", date: "2026-02-30", version: 9, wantField: "completedOn", wantCode: "invalid_date"},
		{name: "future date before stale version", date: "2026-03-02", version: 9, wantField: "completedOn", wantCode: "future_date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := e.svc.Complete(context.Background(), userID, householdID, missingID, tc.version, "order-"+map[bool]string{true: "invalid-date", false: "future-date"}[tc.wantCode == "invalid_date"], item.CompletionRequest{CompletedOn: &tc.date})
			validation(t, err, tc.wantField, tc.wantCode)
		})
	}
	if _, _, err := e.svc.Complete(context.Background(), userID, householdID, missingID, 9, "stale-archived", item.CompletionRequest{}); !errors.Is(err, item.ErrVersionMismatch) {
		t.Fatalf("version check should precede archived state: %v", err)
	}
}

func TestCompleteIdempotencyConflictPrecedesValidationCharacterization(t *testing.T) {
	e := completionEnv("UTC", "2026-03-01", item.Item{ID: missingID, Recurrence: &schedule.Policy{Enabled: true, Interval: schedule.Interval{Value: 1, Unit: schedule.UnitDay}, Mode: schedule.ModeFixed}})
	day := "2026-03-01"
	first, replayed, err := e.svc.Complete(context.Background(), userID, householdID, missingID, 1, "same-key", item.CompletionRequest{CompletedOn: &day})
	if err != nil || replayed {
		t.Fatalf("first=%+v replay=%v err=%v", first, replayed, err)
	}
	badDate := "not-a-date"
	replay, replayed, err := e.svc.Complete(context.Background(), userID, householdID, missingID, 999, "same-key", item.CompletionRequest{CompletedOn: &day})
	if err != nil || !replayed || replay.ID != first.ID {
		t.Fatalf("same-body replay=%+v replay=%v err=%v", replay, replayed, err)
	}
	if _, _, err := e.svc.Complete(context.Background(), userID, householdID, missingID, 999, "same-key", item.CompletionRequest{CompletedOn: &badDate}); !errors.Is(err, item.ErrIdempotencyKeyReused) {
		t.Fatalf("different body should conflict before date validation: %v", err)
	}
}
