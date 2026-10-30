package httpapi

import (
	"net/url"

	"github.com/bigtcze/tendo/backend/internal/item"
)

// queryReader reads single-valued query parameters in call order and keeps the
// first malformed parameter, so handlers report the first offender exactly as
// the documented evaluation order requires. After a failure every later read
// returns the zero value and does not overwrite the recorded offender.
type queryReader struct {
	values  url.Values
	invalid string
}

func newQueryReader(values url.Values) *queryReader { return &queryReader{values: values} }

// scalar returns an absent parameter as "" and rejects a repeated or empty one.
func (q *queryReader) scalar(name string) string {
	if q.invalid != "" {
		return ""
	}
	v, exists := q.values[name]
	if !exists {
		return ""
	}
	if len(v) != 1 || v[0] == "" {
		q.invalid = name
		return ""
	}
	return v[0]
}

// boolean accepts exactly "true" or "false"; absent is nil.
func (q *queryReader) boolean(name string) *bool {
	text := q.scalar(name)
	if text == "" {
		return nil
	}
	if text != "true" && text != "false" {
		q.invalid = name
		return nil
	}
	b := text == "true"
	return &b
}

// forbid rejects parameters that an endpoint does not accept.
func (q *queryReader) forbid(names ...string) {
	for _, name := range names {
		if _, exists := q.values[name]; exists && q.invalid == "" {
			q.invalid = name
		}
	}
}

// collectionFilters are the filters shared by the item list and summary, read
// in their documented order. Semantic validation stays in the item service.
type collectionFilters struct {
	subjectID, responsibleUserID, workflowState, attention string
	recurring                                              *bool
}

func (q *queryReader) collectionFilters() collectionFilters {
	return collectionFilters{
		subjectID:         q.scalar("subjectId"),
		responsibleUserID: q.scalar("responsibleUserId"),
		workflowState:     q.scalar("workflowState"),
		attention:         q.scalar("attention"),
		recurring:         q.boolean("recurring"),
	}
}

func (f collectionFilters) summaryQuery(attentionDate, search string) item.SummaryQuery {
	return item.SummaryQuery{
		SubjectID:         f.subjectID,
		ResponsibleUserID: f.responsibleUserID,
		WorkflowState:     f.workflowState,
		Attention:         f.attention,
		Recurring:         f.recurring,
		AttentionDate:     attentionDate,
		Q:                 search,
	}
}
