package httpapi

import (
	"errors"
	"net/http"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"github.com/go-chi/chi/v5"
)

// The administrator grant routes (ADR 0023, slice B1b3). They are signed-in
// browser operations like the other admin policy routes: the session cookie
// only, the canonical Origin on the DELETEs, the 403 before any lookup, and
// they work whether or not TENDO_MCP_ENABLED or the installation switch is on.
// Every 404 is not_found, the admin policy code: an unknown account and an
// unknown, foreign, revoked or expired grant read the same.
const (
	mcpAdminGrantsPath = "/api/v1/admin/accounts/{userId}/mcp-grants"
	mcpAdminGrantPath  = "/api/v1/admin/accounts/{userId}/mcp-grants/{grantId}"
	// mcpAdminRevokePerActor bounds one administrator's revocations; both
	// DELETEs share this bucket and no other route uses it.
	mcpAdminRevokePerActor = 30
)

// registerAdminGrants mounts the three routes behind the admin wrapper.
func (h *MCPPolicyHandler) registerAdminGrants(r chi.Router, admin func(http.HandlerFunc) http.HandlerFunc) {
	r.Get(mcpAdminGrantsPath, admin(h.listAccountGrants))
	r.Delete(mcpAdminGrantsPath, admin(h.revokeAccountGrants))
	r.Delete(mcpAdminGrantPath, admin(h.revokeAccountGrant))
}

func (h *MCPPolicyHandler) listAccountGrants(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	p, ok := httpx.ParseListParamsFor(w, r, identity.MCPGrantListMaxLimit, httpx.ListFilter(0))
	if !ok {
		return
	}
	page, err := h.service.AccountGrants(r.Context(), principal.UserID, chi.URLParam(r, "userId"), p.Limit, p.Cursor)
	if errors.Is(err, identity.ErrMCPInvalidCursor) {
		httpx.QueryProblemResponse(w, "cursor")
		return
	}
	if err != nil {
		h.writeError(w, r, err, "mcp_admin_grant_list")
		return
	}
	out := apimodel.McpGrantList{Items: make([]apimodel.McpGrant, 0, len(page.Items))}
	for _, item := range page.Items {
		out.Items = append(out.Items, mcpGrantJSON(item))
	}
	if page.NextCursor != "" {
		next := page.NextCursor
		out.NextCursor = &next
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *MCPPolicyHandler) revokeAccountGrant(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	if !h.admitAdminRevoke(w, principal.UserID) {
		return
	}
	err := h.service.RevokeAccountGrant(r.Context(), principal.UserID, chi.URLParam(r, "userId"), chi.URLParam(r, "grantId"))
	h.writeRevoked(w, r, err, "mcp_admin_grant_revoke")
}

func (h *MCPPolicyHandler) revokeAccountGrants(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	if !h.admitAdminRevoke(w, principal.UserID) {
		return
	}
	err := h.service.RevokeAccountGrants(r.Context(), principal.UserID, chi.URLParam(r, "userId"))
	h.writeRevoked(w, r, err, "mcp_admin_grant_revoke_all")
}

// admitAdminRevoke takes one attempt from the administrator's revoke bucket.
// It runs after the administrator check, so a non-administrator always reads
// 403, and before the revocation transaction.
func (h *MCPPolicyHandler) admitAdminRevoke(w http.ResponseWriter, actorID string) bool {
	if h.adminRevokePerActor.admitAttempt(actorID) {
		return true
	}
	w.Header().Set("Retry-After", "60")
	problem(w, http.StatusTooManyRequests, "rate_limited")
	return false
}

func (h *MCPPolicyHandler) writeRevoked(w http.ResponseWriter, r *http.Request, err error, fault string) {
	if err != nil {
		h.writeError(w, r, err, fault)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}
