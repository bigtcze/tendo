package httpapi

import "errors"

// Fixed tool error results shared by the household-scoped MCP tools. They never
// carry driver or service text. ErrMCPToolNotFound is the one answer for a
// household, item or subject that does not exist, belongs to another household
// or is not visible to the caller, so the cases cannot be told apart.
var (
	ErrMCPToolNoPrincipal = errors.New("the request has no verified principal")
	ErrMCPToolNotFound    = errors.New("the household or record was not found")
	ErrMCPToolUnavailable = errors.New("the service is temporarily unavailable")
	ErrMCPToolBadCursor   = errors.New("the argument cursor is not valid")
	// Write tool refusals (#457 slice B3).
	ErrMCPToolVersionChanged = errors.New("the record has changed; read it again and use its current version")
	ErrMCPToolKeyReused      = errors.New("the idempotencyKey was already used for a different action; use a new key")
	ErrMCPToolBadKey         = errors.New("the argument idempotencyKey is not valid")
	ErrMCPToolNoChange       = errors.New("the call changes nothing; pass at least one field to change")
)

// MCPInvalidArgument is the tool error for an argument the service refused.
// name must be a fixed argument name from the tool's own schema, never input.
func MCPInvalidArgument(name string) error {
	return errors.New("the argument " + name + " is not valid")
}
