package mcptool

import (
	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

// CompleteItemOutput is the result of complete_item. ItemVersion is the item
// version the completion produced; Replayed is true when the key returned an
// earlier completion unchanged.
type CompleteItemOutput struct {
	Completion  Completion `json:"completion"`
	ItemVersion int64      `json:"itemVersion" jsonschema:"the item version this completion produced"`
	Replayed    bool       `json:"replayed" jsonschema:"true when the key returned an earlier completion unchanged"`
}

// UndoCompletionOutput is the result of undo_completion. Item is the item as one
// read right after the undo, so its version is the version of that content and
// a caller can update from it without losing a concurrent change. It is nil when
// that read failed; the undo itself is committed either way.
type UndoCompletionOutput struct {
	Completion Completion `json:"completion"`
	Item       *Item      `json:"item" jsonschema:"the item as read right after the undo; null if it could not be read"`
}

// newItemFromInput converts the create input to the service draft.
func newItemFromInput(in CreateItemInput) item.NewItem {
	n := item.NewItem{
		SubjectID: in.SubjectID, Title: in.Title, ResponsibleUserID: in.ResponsibleUserID,
		Notes: in.Notes, AttentionOn: in.AttentionOn, HistoricalCompletedOn: in.HistoricalCompletedOn,
	}
	if in.Recurrence != nil {
		policy := policyOf(*in.Recurrence)
		n.Recurrence = &policy
	}
	return n
}

// patchFromInput converts the update input to the service patch. Absent fields
// are not set; explicit nulls set the nullable fields to clear.
func patchFromInput(in UpdateItemInput) item.Patch {
	p := item.Patch{Title: in.Title, SubjectID: in.SubjectID, WorkflowState: in.WorkflowState, Archived: in.Archived}
	p.ResponsibleUserID = nullableToService(in.ResponsibleUserID, func(v string) string { return v })
	p.Notes = nullableToService(in.Notes, func(v string) string { return v })
	p.AttentionOn = nullableToService(in.AttentionOn, func(v string) string { return v })
	p.Recurrence = nullableToService(in.Recurrence, policyOf)
	return p
}

// nullableToService converts a tool nullable to the service nullable, mapping
// the value with conv. Absent stays absent.
func nullableToService[T, S any](n Nullable[T], conv func(T) S) item.Nullable[S] {
	if !n.Set {
		return item.Nullable[S]{}
	}
	if n.Value == nil {
		return item.Null[S]()
	}
	return item.Some(conv(*n.Value))
}

// policyOf converts a repeat rule argument to the schedule policy. The
// argument schema has already bounded the values.
func policyOf(r RecurrenceArg) schedule.Policy {
	return schedule.Policy{
		Enabled:  true,
		Interval: schedule.Interval{Value: int(r.IntervalValue), Unit: schedule.Unit(r.IntervalUnit)},
		Mode:     schedule.Mode(r.Mode),
	}
}

// completeOutput builds the complete_item result. The version the completion
// produced is ItemVersionBefore+1 on both an applied completion and a replay,
// because the stored receipt keeps the version it was made from.
func completeOutput(c item.Completion, replayed bool) CompleteItemOutput {
	return CompleteItemOutput{Completion: completionOutput(c), ItemVersion: c.ItemVersionBefore + 1, Replayed: replayed}
}
