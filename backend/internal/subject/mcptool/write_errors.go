package mcptool

import (
	"errors"

	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/subject"
)

// writeError maps a subject write service error to a fixed tool error. A name
// or type the service refuses names its argument; anything else is unavailable.
// Member linking is not reachable from this tool, so its errors are not mapped.
func writeError(err error) error {
	var validation *subject.ValidationError
	switch {
	case errors.Is(err, subject.ErrNotFound):
		return identityhttp.ErrMCPToolNotFound
	case errors.Is(err, subject.ErrIdempotencyKeyReused):
		return identityhttp.ErrMCPToolKeyReused
	case errors.As(err, &validation) && validation.Field == "name":
		return identityhttp.MCPInvalidArgument("name")
	case errors.As(err, &validation) && validation.Field == "type":
		return identityhttp.MCPInvalidArgument("type")
	}
	return identityhttp.ErrMCPToolUnavailable
}
