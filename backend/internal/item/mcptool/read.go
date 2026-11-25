package mcptool

import (
	"context"
	"strconv"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/identity"
	identityhttp "github.com/bigtcze/tendo/backend/internal/identity/httpapi"
	"github.com/bigtcze/tendo/backend/internal/item"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool names clients call.
const (
	ListItemsName       = "list_items"
	GetItemName         = "get_item"
	ListCompletionsName = "list_completions"
)

// Reader is the item service surface the read tools need.
type Reader interface {
	Get(ctx context.Context, userID, householdID, itemID string) (item.Item, error)
	List(ctx context.Context, userID, householdID string, q item.ListQuery) (item.Page, error)
	ListCompletions(ctx context.Context, userID, householdID, itemID string, limit int, cursor, order string) (item.CompletionPage, error)
}

// ReadTools returns the item read tools, each requiring tendo:read.
func ReadTools(service Reader) []identityhttp.MCPTool {
	return []identityhttp.MCPTool{ListItems(service), GetItem(service), ListCompletions(service)}
}

// ListItemsInput is the typed input of list_items.
type ListItemsInput struct {
	HouseholdID       string `json:"householdId" jsonschema:"the household, from list_households"`
	Q                 string `json:"q,omitempty" jsonschema:"search text matched against item titles and notes"`
	SubjectID         string `json:"subjectId,omitempty" jsonschema:"only items about this subject"`
	ResponsibleUserID string `json:"responsibleUserId,omitempty" jsonschema:"only items this member is responsible for, or unassigned"`
	WorkflowState     string `json:"workflowState,omitempty"`
	Attention         string `json:"attention,omitempty" jsonschema:"needs_attention or upcoming, as of today in the household"`
	Recurring         *bool  `json:"recurring,omitempty" jsonschema:"only repeating (true) or one-off (false) items"`
	Archived          bool   `json:"archived,omitempty" jsonschema:"list archived items instead of active ones"`
	Done              bool   `json:"done,omitempty" jsonschema:"list completed one-off items instead of unfinished ones"`
	Sort              string `json:"sort,omitempty" jsonschema:"created (default) or attention_on (soonest first, items needing attention now first)"`
	Limit             int    `json:"limit,omitempty" jsonschema:"page size, 1 to 100, default 50"`
	Cursor            string `json:"cursor,omitempty" jsonschema:"opaque nextCursor from the previous page of the same call"`
}

// ListItemsOutput is one page of items. An empty nextCursor is the last page.
type ListItemsOutput struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"nextCursor"`
}

// GetItemInput is the typed input of get_item.
type GetItemInput struct {
	HouseholdID string `json:"householdId"`
	ItemID      string `json:"itemId"`
}

// ListCompletionsInput is the typed input of list_completions.
type ListCompletionsInput struct {
	HouseholdID string `json:"householdId"`
	ItemID      string `json:"itemId"`
	Order       string `json:"order,omitempty" jsonschema:"oldest (default) or newest first"`
	Limit       int    `json:"limit,omitempty" jsonschema:"page size, 1 to 100, default 50"`
	Cursor      string `json:"cursor,omitempty" jsonschema:"opaque nextCursor from the previous page of the same call"`
}

// ListCompletionsOutput is one page of an item's completion history, including
// undone completions. An empty nextCursor is the last page.
type ListCompletionsOutput struct {
	Completions []Completion `json:"completions"`
	NextCursor  string       `json:"nextCursor"`
}

// listItemsArgs maps the service's query parameters to argument names.
var listItemsArgs = map[string]string{
	"q": "q", "subjectId": "subjectId", "responsibleUserId": "responsibleUserId",
	"workflowState": "workflowState", "attention": "attention", "sort": "sort", "limit": "limit",
}

var completionArgs = map[string]string{"order": "order", "limit": "limit"}

var (
	workflowStates = []string{string(item.StateOpen), string(item.StateInProgress), string(item.StateWaiting), string(item.StatePaused)}
	maxCursor      = identityhttp.MCPMaxLength(identityhttp.MaxMCPCursorLength)
	pageLimit      = identityhttp.MCPRange(1, item.MaxLimit)
)

// ListItems is list_items: search and filter one household's items.
func ListItems(service Reader) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name: ListItemsName,
		Description: "List or search the items of one household, one page at a time. Filters combine; " +
			"a subject or member from another household matches nothing.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema: identityhttp.MCPInputSchema[ListItemsInput](map[string]func(*jsonschema.Schema){
			"householdId": identityhttp.MCPUUID,
			"q":           identityhttp.MCPMaxLength(item.MaxSearchRunes),
			"subjectId":   identityhttp.MCPUUID,
			"responsibleUserId": func(s *jsonschema.Schema) {
				s.Pattern = `^(unassigned|` + strings.Trim(identityhttp.MCPUUIDPattern, "^$") + `)$`
			},
			"workflowState": identityhttp.MCPEnum(workflowStates...),
			"attention":     identityhttp.MCPEnum("needs_attention", "upcoming"),
			"sort":          identityhttp.MCPEnum(string(item.SortCreated), string(item.SortAttention)),
			"limit":         pageLimit,
			"cursor":        maxCursor,
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in ListItemsInput) (*mcp.CallToolResult, ListItemsOutput, error) {
		userID, ok := identityhttp.MCPUserID(req, identity.MCPScopeRead)
		if !ok {
			return nil, ListItemsOutput{}, identityhttp.ErrMCPToolNoPrincipal
		}
		binding := listItemsBinding(in)
		cursor, ok := identityhttp.OpenMCPCursor(in.Cursor, ListItemsName, userID, binding...)
		if !ok {
			return nil, ListItemsOutput{}, identityhttp.ErrMCPToolBadCursor
		}
		page, err := service.List(ctx, userID, in.HouseholdID, item.ListQuery{
			Sort: item.ListSort(in.Sort), Archived: in.Archived, Done: in.Done,
			SubjectID: in.SubjectID, ResponsibleUserID: in.ResponsibleUserID,
			WorkflowState: in.WorkflowState, Attention: in.Attention, Recurring: in.Recurring,
			Q: in.Q, Limit: in.Limit, Cursor: cursor,
		})
		if err != nil {
			return nil, ListItemsOutput{}, toolError(err, listItemsArgs)
		}
		out := ListItemsOutput{Items: make([]Item, 0, len(page.Items))}
		for _, found := range page.Items {
			out.Items = append(out.Items, itemOutput(found))
		}
		out.NextCursor = identityhttp.BindMCPCursor(deref(page.NextCursor), ListItemsName, userID, binding...)
		return nil, out, nil
	}
	return readTool(tool, handler)
}

// listItemsBinding is every argument that selects the collection. A cursor
// continues only the same household, search, filters and sort; the page size
// may change between pages.
func listItemsBinding(in ListItemsInput) []string {
	recurring := ""
	if in.Recurring != nil {
		recurring = strconv.FormatBool(*in.Recurring)
	}
	return []string{
		strings.ToLower(in.HouseholdID), in.Q, strings.ToLower(in.SubjectID), strings.ToLower(in.ResponsibleUserID),
		in.WorkflowState, in.Attention, recurring, strconv.FormatBool(in.Archived), strconv.FormatBool(in.Done), in.Sort,
	}
}

// GetItem is get_item: one item of one household.
func GetItem(service Reader) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name:        GetItemName,
		Description: "Get one item of a household by its id.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema: identityhttp.MCPInputSchema[GetItemInput](map[string]func(*jsonschema.Schema){
			"householdId": identityhttp.MCPUUID,
			"itemId":      identityhttp.MCPUUID,
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in GetItemInput) (*mcp.CallToolResult, Item, error) {
		userID, ok := identityhttp.MCPUserID(req, identity.MCPScopeRead)
		if !ok {
			return nil, Item{}, identityhttp.ErrMCPToolNoPrincipal
		}
		found, err := service.Get(ctx, userID, in.HouseholdID, in.ItemID)
		if err != nil {
			return nil, Item{}, toolError(err, nil)
		}
		return nil, itemOutput(found), nil
	}
	return readTool(tool, handler)
}

// ListCompletions is list_completions: the completion history of one item.
func ListCompletions(service Reader) identityhttp.MCPTool {
	tool := &mcp.Tool{
		Name:        ListCompletionsName,
		Description: "List the completion history of one item, one page at a time, including completions that were undone.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		InputSchema: identityhttp.MCPInputSchema[ListCompletionsInput](map[string]func(*jsonschema.Schema){
			"householdId": identityhttp.MCPUUID,
			"itemId":      identityhttp.MCPUUID,
			"order":       identityhttp.MCPEnum(item.CompletionOrderOldest, item.CompletionOrderNewest),
			"limit":       pageLimit,
			"cursor":      maxCursor,
		}),
	}
	handler := func(ctx context.Context, req *mcp.CallToolRequest, in ListCompletionsInput) (*mcp.CallToolResult, ListCompletionsOutput, error) {
		userID, ok := identityhttp.MCPUserID(req, identity.MCPScopeRead)
		if !ok {
			return nil, ListCompletionsOutput{}, identityhttp.ErrMCPToolNoPrincipal
		}
		binding := []string{strings.ToLower(in.HouseholdID), strings.ToLower(in.ItemID), in.Order}
		cursor, ok := identityhttp.OpenMCPCursor(in.Cursor, ListCompletionsName, userID, binding...)
		if !ok {
			return nil, ListCompletionsOutput{}, identityhttp.ErrMCPToolBadCursor
		}
		page, err := service.ListCompletions(ctx, userID, in.HouseholdID, in.ItemID, in.Limit, cursor, in.Order)
		if err != nil {
			return nil, ListCompletionsOutput{}, toolError(err, completionArgs)
		}
		out := ListCompletionsOutput{Completions: make([]Completion, 0, len(page.Items))}
		for _, c := range page.Items {
			out.Completions = append(out.Completions, completionOutput(c))
		}
		out.NextCursor = identityhttp.BindMCPCursor(deref(page.NextCursor), ListCompletionsName, userID, binding...)
		return nil, out, nil
	}
	return readTool(tool, handler)
}

// readTool pairs a tool with its typed handler under tendo:read.
func readTool[In, Out any](tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) identityhttp.MCPTool {
	return identityhttp.MCPTool{
		Tool:    tool,
		Scope:   identity.MCPScopeRead,
		Install: func(server *mcp.Server) { mcp.AddTool(server, tool, handler) },
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
