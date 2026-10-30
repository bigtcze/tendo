package httpapi

import (
	"errors"
	"net/http"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

// writeServiceError maps item service errors to item resource problems. Any
// unclassified error is reported as 503 without internal detail.
func writeServiceError(w http.ResponseWriter, err error) {
	var validation *item.ValidationError
	var query *item.InvalidQueryError
	switch {
	case errors.Is(err, item.ErrIdempotencyKeyReused):
		httpx.ProblemResponse(w, http.StatusUnprocessableEntity, "idempotency_key_reused")
	case errors.Is(err, item.ErrNotFound):
		httpx.ProblemResponse(w, http.StatusNotFound, "not_found")
	case errors.Is(err, item.ErrEmptyPatch):
		httpx.ProblemResponse(w, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, item.ErrVersionMismatch):
		httpx.ProblemResponse(w, http.StatusPreconditionFailed, "precondition_failed")
	case errors.As(err, &validation):
		httpx.ValidationProblemResponse(w, validation.Field, validation.Code)
	case errors.As(err, &query):
		httpx.QueryProblemResponse(w, query.Parameter)
	default:
		httpx.ProblemResponse(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func recurrenceJSON(policy *schedule.Policy) *ItemRecurrence {
	if policy == nil {
		return nil
	}
	return &ItemRecurrence{
		IntervalValue: policy.Interval.Value,
		IntervalUnit:  RecurrenceIntervalUnit(policy.Interval.Unit),
		Mode:          RecurrenceMode(policy.Mode),
	}
}

func toJSON(i item.Item) Item {
	done := i.Done
	out := Item{
		Id:                i.ID,
		SubjectId:         i.SubjectID,
		ResponsibleUserId: i.ResponsibleUserID,
		Title:             i.Title,
		Notes:             i.Notes,
		Recurrence:        recurrenceJSON(i.Recurrence),
		WorkflowState:     WorkflowState(i.WorkflowState),
		Attention:         ItemAttention(i.Attention),
		Archived:          i.Archived,
		Done:              &done,
		CreatedAt:         i.CreatedAt.UTC(),
		UpdatedAt:         i.UpdatedAt.UTC(),
	}
	if i.AttentionOn != nil {
		s := i.AttentionOn.String()
		out.AttentionOn = &s
	}
	// lastCompletedOn is required-nullable: always emitted, null when absent.
	var last *string
	if i.LastCompletedOn != nil {
		s := i.LastCompletedOn.String()
		last = &s
	}
	out.LastCompletedOn = &last
	return out
}

func listJSON(page item.Page) ItemList {
	items := make([]Item, 0, len(page.Items))
	for _, found := range page.Items {
		items = append(items, toJSON(found))
	}
	return ItemList{Items: items, NextCursor: page.NextCursor}
}

func summaryJSON(s item.Summary) ItemSummary {
	return ItemSummary{
		EvaluatedOn:    s.EvaluatedOn.String(),
		Total:          s.Total,
		NeedsAttention: s.NeedsAttention,
		InProgress:     s.InProgress,
		Waiting:        s.Waiting,
		Upcoming:       s.Upcoming,
		Paused:         s.Paused,
		Done:           s.Done,
		Archived:       s.Archived,
	}
}
