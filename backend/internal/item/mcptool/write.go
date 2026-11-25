package mcptool

import (
	"context"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Write tool names clients call.
const (
	CreateItemName     = "create_item"
	UpdateItemName     = "update_item"
	CompleteItemName   = "complete_item"
	UndoCompletionName = "undo_completion"
)

// Writer is the item service surface the write tools need. Its signatures are
// those of item.Service.
type Writer interface {
	Create(ctx context.Context, userID, householdID, key string, n item.NewItem) (item.Item, error)
	Update(ctx context.Context, userID, householdID, itemID string, expectedVersion int64, p item.Patch) (item.Item, error)
	Complete(ctx context.Context, userID, householdID, itemID string, expectedVersion int64, key string, request item.CompletionRequest) (item.Completion, bool, error)
	UndoCompletion(ctx context.Context, userID, householdID, itemID, completionID string, expectedVersion int64) (item.Completion, error)
	Get(ctx context.Context, userID, householdID, itemID string) (item.Item, error)
}

// WriteTools returns the item write tools, each requiring tendo:write.
func WriteTools(service Writer) []identityhttp.MCPTool {
	return []identityhttp.MCPTool{CreateItem(service), UpdateItem(service), CompleteItem(service), UndoCompletion(service)}
}

// CreateItem is create_item: add an item about one subject of a household. A
// retry with the same idempotencyKey returns the item it created.
func CreateItem(service Writer) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name:        CreateItemName,
		Description: "Create an item about one subject of a household. Retry with the same idempotencyKey to get the same item back.",
		Annotations: writeAnnotations(false),
		InputSchema: identityhttp.MCPInputSchema[CreateItemInput](map[string]func(*jsonschema.Schema){
			"householdId":           identityhttp.MCPUUID,
			"subjectId":             identityhttp.MCPUUID,
			"title":                 plainString(minLength(1), identityhttp.MCPMaxLength(item.MaxTitleRunes)),
			"idempotencyKey":        identityhttp.MCPIdempotencyKeyArg,
			"responsibleUserId":     nullableString(identityhttp.MCPUUID),
			"notes":                 nullableString(minLength(1), identityhttp.MCPMaxLength(item.MaxNotesRunes)),
			"attentionOn":           nullableString(dateFormat),
			"historicalCompletedOn": nullableString(dateFormat),
			"recurrence":            recurrenceArg,
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in CreateItemInput) (*mcp.CallToolResult, Item, error) {
		userID, key, err := principal(req, in.IdempotencyKey)
		if err != nil {
			return nil, Item{}, err
		}
		created, err := service.Create(ctx, userID, in.HouseholdID, key, newItemFromInput(in))
		if err != nil {
			return nil, Item{}, writeError(err, createItemArgs)
		}
		return nil, itemOutput(created), nil
	}
	return writeTool(tool, handler)
}

// UpdateItem is update_item: change the named fields of an item, guarded by the
// version the caller read. Absent fields stay as they are; null clears a
// nullable field.
func UpdateItem(service Writer) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name: UpdateItemName,
		Description: "Change fields of one item. Pass the version you read; a stale version is refused. " +
			"Omit a field to keep it, pass null to clear a nullable field (responsibleUserId, notes, attentionOn, recurrence).",
		Annotations: writeAnnotations(true),
		InputSchema: identityhttp.MCPInputSchema[UpdateItemInput](map[string]func(*jsonschema.Schema){
			"householdId":       identityhttp.MCPUUID,
			"itemId":            identityhttp.MCPUUID,
			"version":           minimumOne,
			"title":             plainString(minLength(1), identityhttp.MCPMaxLength(item.MaxTitleRunes)),
			"subjectId":         plainString(identityhttp.MCPUUID),
			"workflowState":     plainString(identityhttp.MCPEnum(workflowStates...)),
			"archived":          plainBool,
			"responsibleUserId": nullableString(identityhttp.MCPUUID),
			"notes":             nullableString(minLength(1), identityhttp.MCPMaxLength(item.MaxNotesRunes)),
			"attentionOn":       nullableString(dateFormat),
			"recurrence":        recurrenceArg,
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in UpdateItemInput) (*mcp.CallToolResult, Item, error) {
		userID, err := caller(req)
		if err != nil {
			return nil, Item{}, err
		}
		updated, err := service.Update(ctx, userID, in.HouseholdID, in.ItemID, in.Version, patchFromInput(in))
		if err != nil {
			return nil, Item{}, writeError(err, updateItemArgs)
		}
		return nil, itemOutput(updated), nil
	}
	return writeTool(tool, handler)
}

// CompleteItem is complete_item: mark an item done, or record a completion of a
// repeating item and schedule its next cycle. A retry with the same
// idempotencyKey returns the completion it recorded.
func CompleteItem(service Writer) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name:        CompleteItemName,
		Description: "Complete an item, today or on a past date. Retry with the same idempotencyKey to get the same completion back.",
		Annotations: writeAnnotations(false),
		InputSchema: identityhttp.MCPInputSchema[CompleteItemInput](map[string]func(*jsonschema.Schema){
			"householdId":    identityhttp.MCPUUID,
			"itemId":         identityhttp.MCPUUID,
			"version":        minimumOne,
			"idempotencyKey": identityhttp.MCPIdempotencyKeyArg,
			"completedOn":    plainString(dateFormat),
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in CompleteItemInput) (*mcp.CallToolResult, CompleteItemOutput, error) {
		userID, key, err := principal(req, in.IdempotencyKey)
		if err != nil {
			return nil, CompleteItemOutput{}, err
		}
		done, replayed, err := service.Complete(ctx, userID, in.HouseholdID, in.ItemID, in.Version, key,
			item.CompletionRequest{CompletedOn: in.CompletedOn})
		if err != nil {
			return nil, CompleteItemOutput{}, writeError(err, completeItemArgs)
		}
		return nil, completeOutput(done, replayed), nil
	}
	return writeTool(tool, handler)
}

// UndoCompletion is undo_completion: undo the latest completion of an item,
// guarded by the item version the caller read. Undoing a completion that is
// already undone returns it unchanged. The result carries the item as one read
// right after the undo; a failed read leaves Item null and never fails the
// committed undo.
func UndoCompletion(service Writer) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name:        UndoCompletionName,
		Description: "Undo the latest completion of an item. Pass the item version you read; a stale version is refused.",
		Annotations: writeAnnotations(true),
		InputSchema: identityhttp.MCPInputSchema[UndoCompletionInput](map[string]func(*jsonschema.Schema){
			"householdId":  identityhttp.MCPUUID,
			"itemId":       identityhttp.MCPUUID,
			"completionId": identityhttp.MCPUUID,
			"version":      minimumOne,
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in UndoCompletionInput) (*mcp.CallToolResult, UndoCompletionOutput, error) {
		userID, err := caller(req)
		if err != nil {
			return nil, UndoCompletionOutput{}, err
		}
		undone, err := service.UndoCompletion(ctx, userID, in.HouseholdID, in.ItemID, in.CompletionID, in.Version)
		if err != nil {
			return nil, UndoCompletionOutput{}, writeError(err, nil)
		}
		out := UndoCompletionOutput{Completion: completionOutput(undone)}
		if current, readErr := service.Get(ctx, userID, in.HouseholdID, in.ItemID); readErr == nil {
			shown := itemOutput(current)
			out.Item = &shown
		}
		return nil, out, nil
	}
	return writeTool(tool, handler)
}

// writeTool pairs a tool with its typed handler under tendo:write.
func writeTool[In, Out any](tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) identityhttp.MCPTool {
	return identityhttp.MCPTool{
		Tool:    tool,
		Scope:   identity.MCPScopeWrite,
		Install: func(server *mcp.Server) { mcp.AddTool(server, tool, handler) },
	}
}

// writeAnnotations declares a write tool: never read-only, never idempotent in
// the MCP sense (a retry needs its key), and destructive only when it can undo
// or overwrite what a caller saw.
func writeAnnotations(destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive, IdempotentHint: false}
}

// caller returns the verified user, or the fixed no-principal error. A token
// without tendo:write fails here, before any service call.
func caller(req *mcp.CallToolRequest) (string, error) {
	userID, ok := identityhttp.MCPUserID(req, identity.MCPScopeWrite)
	if !ok {
		return "", identityhttp.ErrMCPToolNoPrincipal
	}
	return userID, nil
}

// principal returns the caller and the service idempotency key derived from the
// client key. The client key is never forwarded; a key the derivation refuses
// is a bad key only when the caller is otherwise known.
func principal(req *mcp.CallToolRequest, clientKey string) (string, string, error) {
	userID, err := caller(req)
	if err != nil {
		return "", "", err
	}
	key, ok := identityhttp.MCPIdempotencyKey(req, identity.MCPScopeWrite, clientKey)
	if !ok {
		return "", "", identityhttp.ErrMCPToolBadKey
	}
	return userID, key, nil
}
