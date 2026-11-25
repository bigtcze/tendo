package httpapi

import (
	"errors"
	"net/http"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
)

// Slice B1b2 of #457 (ADR 0023): the installation policy and the policy audit
// list. Both are instance-administrator routes behind the session cookie only.
// They work whether or not TENDO_MCP_ENABLED is set or the installation switch
// is off, so an administrator can always turn MCP back on.

var mcpInstallationFields = map[string]httpx.Field{
	"enabled":               {Kind: httpx.KindBool, Required: true},
	"defaultAccountEnabled": {Kind: httpx.KindBool, Required: true},
	"defaultMaxScopes":      {Kind: httpx.KindStringArray, Required: true},
}

func (h *MCPPolicyHandler) getInstallationPolicy(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	view, err := h.service.InstallationPolicy(r.Context(), principal.UserID)
	if err != nil {
		h.writeError(w, r, err, "mcp_installation_read")
		return
	}
	writeInstallationPolicy(w, view)
}

func (h *MCPPolicyHandler) updateInstallationPolicy(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	expected, ok := httpx.ParseIfMatch(w, r)
	if !ok {
		return
	}
	values, ok := httpx.ReadObject(w, r, mcpInstallationFields, mcpPolicyBodyLimit)
	if !ok {
		return
	}
	enabled, okEnabled := values["enabled"].(bool)
	defaultOn, okDefault := values["defaultAccountEnabled"].(bool)
	scopes, okScopes := values["defaultMaxScopes"].([]string)
	if !okEnabled || !okDefault || !okScopes {
		problem(w, http.StatusBadRequest, "invalid_request")
		return
	}
	change := identity.MCPInstallationChange{Enabled: enabled, DefaultAccountEnabled: defaultOn, DefaultMaxScopes: scopes}
	view, err := h.service.UpdateInstallationPolicy(r.Context(), principal.UserID, change, expected)
	if err != nil {
		h.writeError(w, r, err, "mcp_installation_update")
		return
	}
	writeInstallationPolicy(w, view)
}

// listPolicyEvents is the newest-first audit list. Its cursor is opaque and a
// malformed one is refused before any database read.
func (h *MCPPolicyHandler) listPolicyEvents(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	p, ok := httpx.ParseListParamsFor(w, r, identity.MCPPolicyEventListMaxLimit, httpx.ListFilter(0))
	if !ok {
		return
	}
	page, err := h.service.PolicyEvents(r.Context(), principal.UserID, p.Limit, p.Cursor)
	if errors.Is(err, identity.ErrMCPInvalidCursor) {
		httpx.QueryProblemResponse(w, "cursor")
		return
	}
	if err != nil {
		h.writeError(w, r, err, "mcp_policy_events_list")
		return
	}
	out := apimodel.McpPolicyEventList{Items: make([]apimodel.McpPolicyEvent, 0, len(page.Items))}
	for _, event := range page.Items {
		out.Items = append(out.Items, mcpPolicyEventJSON(event))
	}
	if page.NextCursor != "" {
		next := page.NextCursor
		out.NextCursor = &next
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, out)
}

func writeInstallationPolicy(w http.ResponseWriter, view identity.MCPInstallationView) {
	w.Header().Set("ETag", httpx.ETag(view.Version))
	writeJSON(w, http.StatusOK, apimodel.McpInstallationPolicy{
		Enabled: view.Enabled, OperatorEnabled: view.OperatorEnabled,
		DefaultAccountEnabled: view.DefaultAccountEnabled,
		DefaultMaxScopes:      apimodel.McpScopeList(view.DefaultMaxScopes),
	})
}

func mcpPolicyEventJSON(event identity.MCPPolicyEvent) apimodel.McpPolicyEvent {
	out := apimodel.McpPolicyEvent{
		Id: event.ID, Policy: apimodel.McpPolicyEventPolicy(event.Kind),
		ActorUserId: event.ActorUserID, Enabled: event.Enabled,
		DefaultAccountEnabled: event.DefaultAccountEnabled,
		OccurredAt:            event.OccurredAt.UTC(),
	}
	if event.TargetUserID != "" {
		target := event.TargetUserID
		out.TargetUserId = &target
	}
	if event.ActorLogin != "" {
		login := event.ActorLogin
		out.ActorLogin = &login
	}
	if event.TargetLogin != "" {
		login := event.TargetLogin
		out.TargetLogin = &login
	}
	if event.Scopes != nil {
		scopes := apimodel.McpScopeList(event.Scopes)
		out.Scopes = &scopes
	}
	return out
}
