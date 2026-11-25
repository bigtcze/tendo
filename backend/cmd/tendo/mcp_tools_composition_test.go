package main

import (
	"reflect"
	"regexp"
	"slices"
	"testing"

	"github.com/bigtcze/tendo/backend/internal/household"
	householdmcp "github.com/bigtcze/tendo/backend/internal/household/mcptool"
	identityapp "github.com/bigtcze/tendo/backend/internal/identity"
	itemapp "github.com/bigtcze/tendo/backend/internal/item"
	itemmcp "github.com/bigtcze/tendo/backend/internal/item/mcptool"
	subjectapp "github.com/bigtcze/tendo/backend/internal/subject"
	subjectmcp "github.com/bigtcze/tendo/backend/internal/subject/mcptool"
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
// out of MCP: instance administration and settings, members and people,
// invitations, roles, accounts, policy, grants, scopes, sessions, credentials
// and audit. Names are a second line; the pinned ports below are the first.
var excludedMCPToolName = regexp.MustCompile(`(?i)admin|instance|setting|audit|member|people|invit|role|permission|account|user|polic|grant|scope|session|password|credential|token|oauth|oidc|mcp`)

// pinnedMCPPorts is the complete method set of each port the tools reach the
// application through. The ports are what keeps member, invitation, role,
// account and policy operations out of MCP: a tool can call nothing else (the
// architecture test forbids other imports). Widening a port must change this.
var pinnedMCPPorts = map[reflect.Type][]string{
	reflect.TypeFor[householdmcp.Lister](): {"ListForMember"},
	reflect.TypeFor[itemmcp.Reader]():      {"Get", "List", "ListCompletions"},
	reflect.TypeFor[subjectmcp.Reader]():   {"Get", "List"},
}

func TestMCPToolPortsArePinned(t *testing.T) {
	for port, want := range pinnedMCPPorts {
		got := make([]string, 0, port.NumMethod())
		for i := range port.NumMethod() {
			got = append(got, port.Method(i).Name)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%v methods=%v, pinned %v", port, got, want)
		}
	}
}

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
