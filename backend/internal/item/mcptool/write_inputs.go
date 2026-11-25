package mcptool

// CreateItemInput is the typed input of create_item. IdempotencyKey is the
// client key; the tool derives the service key from it and never forwards it.
type CreateItemInput struct {
	HouseholdID           string         `json:"householdId" jsonschema:"the household, from list_households"`
	SubjectID             string         `json:"subjectId" jsonschema:"the subject the item is about, from list_subjects"`
	Title                 string         `json:"title" jsonschema:"1 to 200 characters"`
	IdempotencyKey        string         `json:"idempotencyKey" jsonschema:"a new unique value for each new item"`
	ResponsibleUserID     *string        `json:"responsibleUserId,omitempty" jsonschema:"the household member responsible, or omitted"`
	Notes                 *string        `json:"notes,omitempty" jsonschema:"up to 4000 characters, or omitted"`
	AttentionOn           *string        `json:"attentionOn,omitempty" jsonschema:"the date it next needs attention, YYYY-MM-DD, or omitted"`
	HistoricalCompletedOn *string        `json:"historicalCompletedOn,omitempty" jsonschema:"a past completion date, YYYY-MM-DD; requires recurrence"`
	Recurrence            *RecurrenceArg `json:"recurrence,omitempty" jsonschema:"the repeat rule, or omitted for a one-off item"`
}

// UpdateItemInput is the typed input of update_item. Version is the stored
// version the caller read. Nullable fields distinguish absent (leave as is),
// null (clear) and a value (set).
type UpdateItemInput struct {
	HouseholdID       string                  `json:"householdId"`
	ItemID            string                  `json:"itemId"`
	Version           int64                   `json:"version" jsonschema:"the version you read; the update is refused if it changed"`
	Title             *string                 `json:"title,omitempty"`
	SubjectID         *string                 `json:"subjectId,omitempty"`
	WorkflowState     *string                 `json:"workflowState,omitempty"`
	Archived          *bool                   `json:"archived,omitempty"`
	ResponsibleUserID Nullable[string]        `json:"responsibleUserId,omitempty" jsonschema:"a member, or null to unassign"`
	Notes             Nullable[string]        `json:"notes,omitempty" jsonschema:"text, or null to clear"`
	AttentionOn       Nullable[string]        `json:"attentionOn,omitempty" jsonschema:"YYYY-MM-DD, or null to clear"`
	Recurrence        Nullable[RecurrenceArg] `json:"recurrence,omitempty" jsonschema:"a repeat rule, or null to make it one-off"`
}

// CompleteItemInput is the typed input of complete_item.
type CompleteItemInput struct {
	HouseholdID    string  `json:"householdId"`
	ItemID         string  `json:"itemId"`
	Version        int64   `json:"version" jsonschema:"the version you read; the completion is refused if it changed"`
	IdempotencyKey string  `json:"idempotencyKey" jsonschema:"a new unique value for each completion; reuse it only to retry the same one"`
	CompletedOn    *string `json:"completedOn,omitempty" jsonschema:"YYYY-MM-DD, not in the future; defaults to today"`
}

// UndoCompletionInput is the typed input of undo_completion.
type UndoCompletionInput struct {
	HouseholdID  string `json:"householdId"`
	ItemID       string `json:"itemId"`
	CompletionID string `json:"completionId"`
	Version      int64  `json:"version" jsonschema:"the item version you read; the undo is refused if it changed"`
}
