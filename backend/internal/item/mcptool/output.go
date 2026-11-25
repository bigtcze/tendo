// Package mcptool holds the item MCP tools. They are thin adapters over the
// item application service: they read the principal only through
// identityhttp.MCPUserID, pass the caller's household to the service on every
// call and map service errors to fixed tool error results.
package mcptool

import (
	"time"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

// Recurrence is an item's repeat rule.
type Recurrence struct {
	IntervalValue int    `json:"intervalValue"`
	IntervalUnit  string `json:"intervalUnit" jsonschema:"day, week, month or year"`
	Mode          string `json:"mode" jsonschema:"fixed or after_completion"`
}

// Item is one item as the tools return it. Dates are YYYY-MM-DD in the
// household's time zone; timestamps are RFC 3339 in UTC.
type Item struct {
	ID                string      `json:"id"`
	SubjectID         string      `json:"subjectId"`
	ResponsibleUserID *string     `json:"responsibleUserId"`
	Title             string      `json:"title"`
	Notes             *string     `json:"notes"`
	AttentionOn       *string     `json:"attentionOn" jsonschema:"the date the item next needs attention; null means it needs attention now"`
	Recurrence        *Recurrence `json:"recurrence"`
	WorkflowState     string      `json:"workflowState" jsonschema:"open, in_progress, waiting or paused"`
	Attention         string      `json:"attention" jsonschema:"needs_attention or upcoming, as of today in the household"`
	Archived          bool        `json:"archived"`
	Done              bool        `json:"done"`
	LastCompletedOn   *string     `json:"lastCompletedOn"`
	CreatedAt         string      `json:"createdAt"`
	UpdatedAt         string      `json:"updatedAt"`
	Version           int64       `json:"version" jsonschema:"the stored version; changes whenever the item changes"`
}

// Completion is one entry of an item's completion history.
type Completion struct {
	ID                string      `json:"id"`
	ItemID            string      `json:"itemId"`
	CompletedByUserID string      `json:"completedByUserId"`
	CompletedOn       string      `json:"completedOn"`
	CycleAttentionOn  *string     `json:"cycleAttentionOn" jsonschema:"the attention date of the cycle this completion closed"`
	NextAttentionOn   *string     `json:"nextAttentionOn" jsonschema:"the attention date this completion scheduled"`
	Recurrence        *Recurrence `json:"recurrence"`
	CreatedAt         string      `json:"createdAt"`
	UndoneAt          *string     `json:"undoneAt"`
	UndoneByUserID    *string     `json:"undoneByUserId"`
}

func itemOutput(i item.Item) Item {
	return Item{
		ID: i.ID, SubjectID: i.SubjectID, ResponsibleUserID: i.ResponsibleUserID,
		Title: i.Title, Notes: i.Notes, AttentionOn: dateOutput(i.AttentionOn),
		Recurrence: recurrenceOutput(i.Recurrence), WorkflowState: string(i.WorkflowState),
		Attention: string(i.Attention), Archived: i.Archived, Done: i.Done,
		LastCompletedOn: dateOutput(i.LastCompletedOn),
		CreatedAt:       timeOutput(i.CreatedAt), UpdatedAt: timeOutput(i.UpdatedAt), Version: i.Version,
	}
}

func completionOutput(c item.Completion) Completion {
	out := Completion{
		ID: c.ID, ItemID: c.ItemID, CompletedByUserID: c.CompletedByUserID,
		CompletedOn: c.CompletedOn.String(), CycleAttentionOn: dateOutput(c.CycleAttentionOn),
		NextAttentionOn: dateOutput(c.NextAttentionOn), Recurrence: recurrenceOutput(c.Recurrence),
		CreatedAt: timeOutput(c.CreatedAt), UndoneByUserID: c.UndoneByUserID,
	}
	if c.UndoneAt != nil {
		at := timeOutput(*c.UndoneAt)
		out.UndoneAt = &at
	}
	return out
}

func recurrenceOutput(p *schedule.Policy) *Recurrence {
	if p == nil {
		return nil
	}
	return &Recurrence{IntervalValue: p.Interval.Value, IntervalUnit: string(p.Interval.Unit), Mode: string(p.Mode)}
}

func dateOutput(d *schedule.Date) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func timeOutput(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
