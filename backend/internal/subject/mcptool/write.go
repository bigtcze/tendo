package mcptool

import (
	"context"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/subject"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CreateSubjectName is the write tool name clients call.
const CreateSubjectName = "create_subject"

// notDestructive is the destructive hint of create_subject: it only adds.
var notDestructive = false

// Writer is the subject service surface the write tools need. Its signature is
// that of subject.Service.Create.
type Writer interface {
	Create(ctx context.Context, userID, householdID, key, name string, t subject.Type, linkedUserID *string) (subject.Subject, error)
}

// WriteTools returns the subject write tools, each requiring tendo:write.
func WriteTools(service Writer) []identityhttp.MCPTool {
	return []identityhttp.MCPTool{CreateSubject(service)}
}

// CreateSubjectInput is the typed input of create_subject. Linking a household
// member is not part of this tool.
type CreateSubjectInput struct {
	HouseholdID    string `json:"householdId"`
	Name           string `json:"name" jsonschema:"1 to 100 characters"`
	Type           string `json:"type" jsonschema:"person, home, vehicle, pet or custom"`
	IdempotencyKey string `json:"idempotencyKey" jsonschema:"a new unique value for each new subject"`
}

// CreateSubject is create_subject: add a subject to a household. A retry with
// the same idempotencyKey returns the subject it created.
func CreateSubject(service Writer) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name:        CreateSubjectName,
		Description: "Create a subject (a person, home, vehicle, pet or other thing items are about) in a household. Retry with the same idempotencyKey to get the same subject back.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &notDestructive, IdempotentHint: false},
		InputSchema: identityhttp.MCPInputSchema[CreateSubjectInput](map[string]func(*jsonschema.Schema){
			"householdId":    identityhttp.MCPUUID,
			"name":           identityhttp.MCPMaxLength(maxNameRunes),
			"type":           identityhttp.MCPEnum(subjectTypes...),
			"idempotencyKey": identityhttp.MCPIdempotencyKeyArg,
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in CreateSubjectInput) (*mcp.CallToolResult, Subject, error) {
		userID, ok := identityhttp.MCPUserID(req, identity.MCPScopeWrite)
		if !ok {
			return nil, Subject{}, identityhttp.ErrMCPToolNoPrincipal
		}
		key, ok := identityhttp.MCPIdempotencyKey(req, identity.MCPScopeWrite, in.IdempotencyKey)
		if !ok {
			return nil, Subject{}, identityhttp.ErrMCPToolBadKey
		}
		created, err := service.Create(ctx, userID, in.HouseholdID, key, in.Name, subject.Type(in.Type), nil)
		if err != nil {
			return nil, Subject{}, writeError(err)
		}
		return nil, subjectOutput(created), nil
	}
	return writeTool(tool, handler)
}

var subjectTypes = []string{
	string(subject.TypePerson), string(subject.TypeHome), string(subject.TypeVehicle),
	string(subject.TypePet), string(subject.TypeCustom),
}

// maxNameRunes is the subject name limit of subject.ValidateName.
const maxNameRunes = 100

// writeTool pairs a tool with its typed handler under tendo:write.
func writeTool[In, Out any](tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) identityhttp.MCPTool {
	return identityhttp.MCPTool{
		Tool:    tool,
		Scope:   identity.MCPScopeWrite,
		Install: func(server *mcp.Server) { mcp.AddTool(server, tool, handler) },
	}
}
