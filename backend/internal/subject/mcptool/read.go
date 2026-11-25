// Package mcptool holds the subject MCP tools. They are thin adapters over the
// subject application service: they read the principal only through
// identityhttp.MCPUserID, pass the caller's household to the service on every
// call and map service errors to fixed tool error results.
package mcptool

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool names clients call.
const (
	ListSubjectsName = "list_subjects"
	GetSubjectName   = "get_subject"
)

// Reader is the subject service surface the read tools need.
type Reader interface {
	Get(ctx context.Context, userID, householdID, subjectID string) (subject.Subject, error)
	List(ctx context.Context, userID, householdID string, q subject.ListQuery) (subject.Page, error)
}

// ReadTools returns the subject read tools, each requiring tendo:read.
func ReadTools(service Reader) []identityhttp.MCPTool {
	return []identityhttp.MCPTool{ListSubjects(service), GetSubject(service)}
}

// Subject is one subject: a person, home, vehicle, pet or other thing items
// are about.
type Subject struct {
	ID           string  `json:"id"`
	Type         string  `json:"type" jsonschema:"person, home, vehicle, pet or custom"`
	Name         string  `json:"name"`
	Archived     bool    `json:"archived"`
	LinkedUserID *string `json:"linkedUserId" jsonschema:"the member this person represents, if any"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
	Version      int64   `json:"version"`
}

// ListSubjectsInput is the typed input of list_subjects.
type ListSubjectsInput struct {
	HouseholdID string `json:"householdId" jsonschema:"the household, from list_households"`
	Archived    bool   `json:"archived,omitempty" jsonschema:"list archived subjects instead of active ones"`
	Limit       int    `json:"limit,omitempty" jsonschema:"page size, 1 to 100, default 50"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"opaque nextCursor from the previous page of the same call"`
}

// ListSubjectsOutput is one page of subjects. An empty nextCursor is the last page.
type ListSubjectsOutput struct {
	Subjects   []Subject `json:"subjects"`
	NextCursor string    `json:"nextCursor"`
}

// GetSubjectInput is the typed input of get_subject.
type GetSubjectInput struct {
	HouseholdID string `json:"householdId"`
	SubjectID   string `json:"subjectId"`
}

// ListSubjects is list_subjects: the subjects of one household.
func ListSubjects(service Reader) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name:        ListSubjectsName,
		Description: "List the subjects (people and things items are about) of one household, one page at a time.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema: identityhttp.MCPInputSchema[ListSubjectsInput](map[string]func(*jsonschema.Schema){
			"householdId": identityhttp.MCPUUID,
			"limit":       identityhttp.MCPRange(1, subject.MaxLimit),
			"cursor":      identityhttp.MCPMaxLength(identityhttp.MaxMCPCursorLength),
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in ListSubjectsInput) (*mcp.CallToolResult, ListSubjectsOutput, error) {
		userID, ok := identityhttp.MCPUserID(req, identity.MCPScopeRead)
		if !ok {
			return nil, ListSubjectsOutput{}, identityhttp.ErrMCPToolNoPrincipal
		}
		binding := []string{strings.ToLower(in.HouseholdID), strconv.FormatBool(in.Archived)}
		cursor, ok := identityhttp.OpenMCPCursor(in.Cursor, ListSubjectsName, userID, binding...)
		if !ok {
			return nil, ListSubjectsOutput{}, identityhttp.ErrMCPToolBadCursor
		}
		page, err := service.List(ctx, userID, in.HouseholdID, subject.ListQuery{Archived: in.Archived, Limit: in.Limit, Cursor: cursor})
		if err != nil {
			return nil, ListSubjectsOutput{}, toolError(err)
		}
		out := ListSubjectsOutput{Subjects: make([]Subject, 0, len(page.Items))}
		for _, s := range page.Items {
			out.Subjects = append(out.Subjects, subjectOutput(s))
		}
		next := ""
		if page.NextCursor != nil {
			next = *page.NextCursor
		}
		out.NextCursor = identityhttp.BindMCPCursor(next, ListSubjectsName, userID, binding...)
		return nil, out, nil
	}
	return readTool(tool, handler)
}

// GetSubject is get_subject: one subject of one household.
func GetSubject(service Reader) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name:        GetSubjectName,
		Description: "Get one subject of a household by its id.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema: identityhttp.MCPInputSchema[GetSubjectInput](map[string]func(*jsonschema.Schema){
			"householdId": identityhttp.MCPUUID,
			"subjectId":   identityhttp.MCPUUID,
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in GetSubjectInput) (*mcp.CallToolResult, Subject, error) {
		userID, ok := identityhttp.MCPUserID(req, identity.MCPScopeRead)
		if !ok {
			return nil, Subject{}, identityhttp.ErrMCPToolNoPrincipal
		}
		found, err := service.Get(ctx, userID, in.HouseholdID, in.SubjectID)
		if err != nil {
			return nil, Subject{}, toolError(err)
		}
		return nil, subjectOutput(found), nil
	}
	return readTool(tool, handler)
}

func subjectOutput(s subject.Subject) Subject {
	return Subject{
		ID: s.ID, Type: string(s.Type), Name: s.Name, Archived: s.Archived, LinkedUserID: s.LinkedUserID,
		CreatedAt: s.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: s.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Version: s.Version,
	}
}

// toolError maps a subject service error to a fixed tool error. Not found
// covers a malformed, unknown or foreign household or subject and a caller who
// is not a member, identically.
func toolError(err error) error {
	var query *subject.InvalidQueryError
	switch {
	case errors.Is(err, subject.ErrNotFound):
		return identityhttp.ErrMCPToolNotFound
	case errors.As(err, &query) && query.Parameter == "cursor":
		return identityhttp.ErrMCPToolBadCursor
	case errors.As(err, &query) && query.Parameter == "limit":
		return identityhttp.MCPInvalidArgument("limit")
	}
	return identityhttp.ErrMCPToolUnavailable
}

func readTool[In, Out any](tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) identityhttp.MCPTool {
	return identityhttp.MCPTool{
		Tool:    tool,
		Scope:   identity.MCPScopeRead,
		Install: func(server *mcp.Server) { mcp.AddTool(server, tool, handler) },
	}
}
