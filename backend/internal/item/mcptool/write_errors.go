package mcptool

import (
	"errors"

	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/item"
)

// Fixed write tool errors for item states. They name no item, service or
// driver detail.
var (
	ErrMCPToolArchived   = errors.New("the item is archived")
	ErrMCPToolDone       = errors.New("the item is already done")
	ErrMCPToolNotLatest  = errors.New("only the latest completion can be undone")
	ErrMCPToolSuperseded = errors.New("the item changed after this completion; it can no longer be undone")
	// ErrMCPToolDateOverflow means the next occurrence would fall beyond the
	// supported dates. It is not an argument error: the caller cannot fix it by
	// changing one argument's format.
	ErrMCPToolDateOverflow = errors.New("the next occurrence would be beyond the supported dates")
)

// Argument maps translate a service field to the tool argument it came from, so
// a refused field is named by its argument only. Fields absent from a map are
// unavailable, never named.
var (
	createItemArgs = map[string]string{
		"title": "title", "notes": "notes", "attentionOn": "attentionOn",
		"historicalCompletedOn": "historicalCompletedOn", "recurrence": "recurrence",
	}
	updateItemArgs = map[string]string{
		"title": "title", "notes": "notes", "attentionOn": "attentionOn",
		"recurrence": "recurrence", "workflowState": "workflowState",
	}
	completeItemArgs = map[string]string{"completedOn": "completedOn"}
)

// writeError maps an item write service error to a fixed tool error. A subject
// or responsible-member reference that is not valid is reported exactly as not
// found, so the caller cannot tell a foreign reference from a missing one.
func writeError(err error, args map[string]string) error {
	var validation *item.ValidationError
	if errors.As(err, &validation) {
		return validationError(validation, args)
	}
	return sentinelError(err)
}

// sentinelError maps the service's sentinel errors to fixed texts.
func sentinelError(err error) error {
	switch {
	case errors.Is(err, item.ErrNotFound), errors.Is(err, item.ErrInvalidReference),
		errors.Is(err, item.ErrInvalidResponsibleReference):
		return identityhttp.ErrMCPToolNotFound
	case errors.Is(err, item.ErrVersionMismatch):
		return identityhttp.ErrMCPToolVersionChanged
	case errors.Is(err, item.ErrIdempotencyKeyReused):
		return identityhttp.ErrMCPToolKeyReused
	case errors.Is(err, item.ErrEmptyPatch):
		return identityhttp.ErrMCPToolNoChange
	case errors.Is(err, item.ErrArchived):
		return ErrMCPToolArchived
	case errors.Is(err, item.ErrDone):
		return ErrMCPToolDone
	case errors.Is(err, item.ErrCompletionNotLatest):
		return ErrMCPToolNotLatest
	case errors.Is(err, item.ErrCompletionSuperseded):
		return ErrMCPToolSuperseded
	}
	return identityhttp.ErrMCPToolUnavailable
}

// validationError maps a field validation failure of the service.
func validationError(v *item.ValidationError, args map[string]string) error {
	if isReferenceField(v) {
		return identityhttp.ErrMCPToolNotFound
	}
	if v.Field == "" && v.Code == "invalid_idempotency_key" {
		return identityhttp.ErrMCPToolBadKey
	}
	if v.Code == "date_overflow" {
		return ErrMCPToolDateOverflow
	}
	if name, ok := args[v.Field]; ok {
		return identityhttp.MCPInvalidArgument(name)
	}
	return identityhttp.ErrMCPToolUnavailable
}

// isReferenceField reports whether v is a subject or responsible-member
// reference the caller cannot see, which is reported as not found.
func isReferenceField(v *item.ValidationError) bool {
	return v.Code == "invalid_reference" && (v.Field == "subjectId" || v.Field == "responsibleUserId")
}
