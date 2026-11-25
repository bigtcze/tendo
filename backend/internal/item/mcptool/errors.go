package mcptool

import (
	"errors"

	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/item"
)

// toolError maps an item read service error to a fixed tool error. Not found covers
// a malformed, unknown or foreign household, item or completion, and a caller
// who is not a member, identically. A refused query parameter names the tool
// argument from args (service parameter -> argument); an unmapped parameter or
// any other error is reported as unavailable without detail.
func toolError(err error, args map[string]string) error {
	var query *item.InvalidQueryError
	switch {
	case errors.Is(err, item.ErrNotFound):
		return identityhttp.ErrMCPToolNotFound
	case errors.As(err, &query):
		if query.Parameter == "cursor" {
			return identityhttp.ErrMCPToolBadCursor
		}
		if name, ok := args[query.Parameter]; ok {
			return identityhttp.MCPInvalidArgument(name)
		}
	}
	return identityhttp.ErrMCPToolUnavailable
}
