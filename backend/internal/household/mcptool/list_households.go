// Package mcptool holds the household MCP tools. It depends on the household
// service and on the neutral MCP tool type in the identity HTTP adapter; the
// identity layer never imports this package.
package mcptool

import (
	"context"
	"errors"

	"github.com/bigtcze/tendo/backend/internal/household"
	"github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListHouseholdsName is the tool name clients call.
const ListHouseholdsName = "list_households"

// maxCursorLength bounds the opaque household cursor before the household
// service decodes it. A valid cursor is 2 + 48 characters.
const (
	maxCursorLength = 64
	defaultLimit    = 50
)

// ListHouseholdsInput is the typed input. The cursor is optional; an empty
// cursor starts at the first page.
type ListHouseholdsInput struct {
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque cursor from a previous nextCursor, omit for the first page"`
}

// Household is one household visible to the caller.
type Household struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// ListHouseholdsOutput is the typed output.
type ListHouseholdsOutput struct {
	Households []Household `json:"households"`
	NextCursor string      `json:"nextCursor"`
}

// Fixed tool error messages. They never carry driver or service text.
const (
	errNoPrincipal = "the request has no verified principal"
	errUnavailable = "households are temporarily unavailable"
	errInvalid     = "the cursor is not valid"
	errNotFound    = "no households are visible to this token"
)

// Lister is the household service surface the tool needs.
type Lister interface {
	ListForMember(ctx context.Context, userID, cursor string, limit int) ([]household.ListedHousehold, string, error)
}

// ListHouseholds returns the MCP tool definition and its scope-checked handler.
// The tool requires tendo:read and is read-only.
func ListHouseholds(service Lister) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name:        ListHouseholdsName,
		Description: "List the households the signed-in user belongs to, one page at a time.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}
	return identityhttp.MCPTool{
		Tool:  tool,
		Scope: identity.MCPScopeRead,
		Install: func(server *mcp.Server) {
			mcp.AddTool(server, tool, listHandler(service))
		},
	}
}

func listHandler(service Lister) mcp.ToolHandlerFor[ListHouseholdsInput, ListHouseholdsOutput] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in ListHouseholdsInput) (*mcp.CallToolResult, ListHouseholdsOutput, error) {
		userID, ok := identityhttp.MCPUserID(req, identity.MCPScopeRead)
		if !ok {
			return nil, ListHouseholdsOutput{}, errors.New(errNoPrincipal)
		}
		if len(in.Cursor) > maxCursorLength {
			return nil, ListHouseholdsOutput{}, errors.New(errInvalid)
		}
		rows, next, err := service.ListForMember(ctx, userID, in.Cursor, defaultLimit)
		switch {
		case errors.Is(err, household.ErrNotFound):
			return nil, ListHouseholdsOutput{}, errors.New(errNotFound)
		case isValidationError(err):
			return nil, ListHouseholdsOutput{}, errors.New(errInvalid)
		case err != nil:
			return nil, ListHouseholdsOutput{}, errors.New(errUnavailable)
		}
		out := ListHouseholdsOutput{Households: make([]Household, 0, len(rows)), NextCursor: next}
		for _, row := range rows {
			out.Households = append(out.Households, Household{ID: row.ID, Name: row.Name, Role: row.ViewerRole})
		}
		return nil, out, nil
	}
}

func isValidationError(err error) bool {
	var v *household.ValidationError
	return errors.As(err, &v)
}
