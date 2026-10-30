package item

import (
	"context"
	"github.com/bigtcze/tendo/backend/internal/platform/ids"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/schedule"
)

func validateCollectionFilters(subjectID, responsibleID, workflow, attention string, recurring *bool) (CollectionFilters, error) {
	f := CollectionFilters{}
	if subjectID != "" {
		if !ids.IsCanonicalUUID(subjectID) {
			return f, &InvalidQueryError{"subjectId"}
		}
		f.SubjectID = strings.ToLower(subjectID)
	}
	if responsibleID != "" {
		if responsibleID == "unassigned" {
			f.ResponsibleUserID = Null[string]()
		} else {
			if !ids.IsCanonicalUUID(responsibleID) {
				return f, &InvalidQueryError{"responsibleUserId"}
			}
			f.ResponsibleUserID = Some(strings.ToLower(responsibleID))
		}
	}
	if workflow != "" {
		v, ok := ParseWorkflowState(workflow)
		if !ok {
			return f, &InvalidQueryError{"workflowState"}
		}
		f.WorkflowState = v
	}
	if attention != "" {
		switch schedule.Attention(attention) {
		case schedule.NeedsAttention, schedule.Upcoming:
			f.Attention = schedule.Attention(attention)
		default:
			return f, &InvalidQueryError{"attention"}
		}
	}
	f.Recurring = recurring
	return f, nil
}

func (s *Service) Summary(ctx context.Context, userID, householdID string, q SummaryQuery) (Summary, error) {
	householdID, today, err := s.check(ctx, userID, householdID)
	if err != nil {
		return Summary{}, err
	}
	filters, err := validateCollectionFilters(q.SubjectID, q.ResponsibleUserID, q.WorkflowState, q.Attention, q.Recurring)
	if err != nil {
		return Summary{}, err
	}
	if q.Q != "" {
		filters.Q, err = ValidateSearch(q.Q)
		if err != nil {
			return Summary{}, err
		}
	}
	if q.AttentionDate != "" {
		d, e := ParseDateForField(q.AttentionDate, "attentionDate")
		if e != nil || d.Compare(today) != 0 {
			return Summary{}, &InvalidQueryError{"attentionDate"}
		}
	}
	opctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	counts, err := s.repository.Summary(opctx, householdID, SummaryOptions{Filters: filters, Today: today})
	if err != nil {
		return Summary{}, ErrUnavailable
	}
	return Summary{EvaluatedOn: today, SummaryCounts: counts}, nil
}
