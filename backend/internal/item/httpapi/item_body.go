package httpapi

import (
	"errors"

	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/schedule"
)

const bodyLimit = 64 << 10

// Strict body schemas. httpx.ReadObject enforces kinds, required keys and
// null permission before the converters below run, so their type assertions
// only see values of the declared kind.
var (
	createFields = map[string]httpx.Field{
		"title":                 {Kind: httpx.KindString, Required: true},
		"subjectId":             {Kind: httpx.KindString, Required: true},
		"responsibleUserId":     {Kind: httpx.KindNullableString},
		"notes":                 {Kind: httpx.KindNullableString},
		"attentionOn":           {Kind: httpx.KindNullableString},
		"historicalCompletedOn": {Kind: httpx.KindString},
		"recurrence":            {Kind: httpx.KindNullableObject, Fields: recurrenceFields},
	}

	recurrenceFields = map[string]httpx.Field{
		"intervalValue": {Kind: httpx.KindInteger, Required: true},
		"intervalUnit":  {Kind: httpx.KindString, Required: true},
		"mode":          {Kind: httpx.KindString, Required: true},
	}
	updateFields = map[string]httpx.Field{
		"title":             {Kind: httpx.KindString},
		"subjectId":         {Kind: httpx.KindString},
		"responsibleUserId": {Kind: httpx.KindNullableString},
		"notes":             {Kind: httpx.KindNullableString},
		"attentionOn":       {Kind: httpx.KindNullableString},
		"recurrence":        {Kind: httpx.KindNullableObject, Fields: recurrenceFields},
		"workflowState":     {Kind: httpx.KindString},
		"archived":          {Kind: httpx.KindBool},
	}
)

// newItemFromBody converts a decoded create body. Omitted and null optional
// fields both become nil, which keeps create idempotency fingerprints equal.
func newItemFromBody(values map[string]any) (item.NewItem, error) {
	n := item.NewItem{
		SubjectID:             values["subjectId"].(string),
		Title:                 values["title"].(string),
		ResponsibleUserID:     optionalString(values, "responsibleUserId"),
		Notes:                 optionalString(values, "notes"),
		AttentionOn:           optionalString(values, "attentionOn"),
		HistoricalCompletedOn: optionalString(values, "historicalCompletedOn"),
	}
	if v, present := values["recurrence"]; present && v != nil {
		policy, err := recurrence(v)
		if err != nil {
			return item.NewItem{}, err
		}
		n.Recurrence = policy
	}
	return n, nil
}

// patchFromBody converts a decoded PATCH body. Omitted fields stay unchanged;
// explicit null clears a nullable field.
func patchFromBody(values map[string]any) (item.Patch, error) {
	patch := item.Patch{
		Title:         optionalString(values, "title"),
		SubjectID:     optionalString(values, "subjectId"),
		WorkflowState: optionalString(values, "workflowState"),
	}
	if v, present := values["responsibleUserId"]; present {
		patch.ResponsibleUserID = nullable(v)
	}
	if v, present := values["notes"]; present {
		patch.Notes = nullable(v)
	}
	if v, present := values["attentionOn"]; present {
		patch.AttentionOn = nullable(v)
	}
	if v, present := values["recurrence"]; present {
		if v == nil {
			patch.Recurrence = item.Null[schedule.Policy]()
		} else {
			policy, err := recurrence(v)
			if err != nil {
				return item.Patch{}, err
			}
			patch.Recurrence = item.Some(*policy)
		}
	}
	if v, present := values["archived"]; present {
		b := v.(bool)
		patch.Archived = &b
	}
	return patch, nil
}

// optionalString returns nil for an omitted or null string field.
func optionalString(values map[string]any, name string) *string {
	v, present := values[name]
	if !present || v == nil {
		return nil
	}
	s := v.(string)
	return &s
}

// nullable converts a decoded string-or-null value.
func nullable(v any) item.Nullable[string] {
	if v == nil {
		return item.Null[string]()
	}
	return item.Some(v.(string))
}

// recurrence converts a decoded recurrence object. An interval outside 1..999
// becomes 0 so the service reports invalid_interval without int narrowing.
func recurrence(v any) (*schedule.Policy, error) {
	fields := v.(map[string]any)
	value, ok := fields["intervalValue"].(int64)
	if !ok {
		return nil, errors.New("invalid interval")
	}
	unit, ok := fields["intervalUnit"].(string)
	if !ok {
		return nil, errors.New("invalid unit")
	}
	mode, ok := fields["mode"].(string)
	if !ok {
		return nil, errors.New("invalid mode")
	}
	interval := 0
	if value >= 1 && value <= 999 {
		interval = int(value)
	}
	return &schedule.Policy{
		Enabled:  true,
		Interval: schedule.Interval{Value: interval, Unit: schedule.Unit(unit)},
		Mode:     schedule.Mode(mode),
	}, nil
}
