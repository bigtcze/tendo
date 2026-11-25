package main

import (
	"regexp"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	itemapp "github.com/bigtcze/tendo/backend/internal/item"
	subjectapp "github.com/bigtcze/tendo/backend/internal/subject"
)

// productionMCPTools is the complete served tool set and each tool's scope
// (#457 slice B2). Adding, removing or rescoping a tool must change this map.
var productionMCPTools = map[string]string{
	"list_households":  identityapp.MCPScopeRead,
	"list_items":       identityapp.MCPScopeRead,
	"get_item":         identityapp.MCPScopeRead,
	"list_completions": identityapp.MCPScopeRead,
	"list_subjects":    identityapp.MCPScopeRead,
	"get_subject":      identityapp.MCPScopeRead,
}

// excludedMCPToolName matches any tool name that would reach an area #457 keeps
// out of MCP: instance administration, members, invitations, roles, accounts,
// policy, grants, sessions and passwords.
var excludedMCPToolName = regexp.MustCompile(`admin|member|invit|role|account|polic|grant|session|password|user|token`)

func TestProductionMCPToolSetIsPinned(t *testing.T) {
	tools := mcpTools(household.NewService(nil), itemapp.NewService(nil, nil, nil, nil, nil), subjectapp.NewService(nil, nil, nil))
	if len(tools) != len(productionMCPTools) {
		t.Fatalf("composition serves %d tools, the pinned set has %d", len(tools), len(productionMCPTools))
	}
	for _, tool := range tools {
		name := tool.Tool.Name
		want, ok := productionMCPTools[name]
		if !ok {
			t.Fatalf("tool %q is served but not pinned", name)
		}
		if tool.Scope != want {
			t.Fatalf("tool %q scope=%q, pinned %q", name, tool.Scope, want)
		}
		if excludedMCPToolName.MatchString(name) {
			t.Fatalf("tool %q reaches an area excluded from MCP", name)
		}
		readOnly := tool.Tool.Annotations != nil && tool.Tool.Annotations.ReadOnlyHint
		if readOnly != (tool.Scope == identityapp.MCPScopeRead) {
			t.Fatalf("tool %q: readOnlyHint=%v does not match scope %q", name, readOnly, tool.Scope)
		}
	}
}
