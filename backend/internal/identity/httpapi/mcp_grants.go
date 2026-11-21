package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"github.com/go-chi/chi/v5"
)

// The connected-apps routes (ADR 0023, slice A4b). They are signed-in browser
// operations: the session cookie only, never an MCP bearer token. They work
// whether or not TENDO_MCP_ENABLED is set, so the handler deliberately does not
// see the service's Enabled() flag and never uses MCPHandler.guarded.
const (
	mcpGrantsPath         = "/api/v1/mcp/grants"
	mcpGrantPath          = "/api/v1/mcp/grants/{grantId}"
	mcpGrantRevokePerUser = 30
	mcpGrantNotFoundCode  = "mcp_grant_not_found"
)

// mcpGrantService is the connected-apps surface the adapter needs.
type mcpGrantService interface {
	ListGrants(ctx context.Context, userID string, limit int, cursor string) (identity.MCPGrantPage, error)
	RevokeGrantForUser(ctx context.Context, userID, grantID string) error
}

// MCPGrantsHandler serves the signed-in account's connected apps.
type MCPGrantsHandler struct {
	service mcpGrantService
	// sessions authenticates every request by the session cookie.
	sessions *SessionHandler
	// revokePerUser is its own bucket, never shared with consent or login.
	revokePerUser *limiter
}

// NewMCPGrants builds the connected-apps adapter over the MCP service.
func NewMCPGrants(service mcpGrantService, sessions *SessionHandler) *MCPGrantsHandler {
	return &MCPGrantsHandler{
		service:       service,
		sessions:      sessions,
		revokePerUser: newLimiter(mcpGrantRevokePerUser),
	}
}

// Register mounts both routes behind RequireSession. They are not in
// MCPCrossOriginRoutes: a DELETE needs the canonical Origin like any other
// state-changing request, and no CORS headers are sent.
func (h *MCPGrantsHandler) Register(r chi.Router) {
	r.Get(mcpGrantsPath, h.sessions.RequireSession(http.HandlerFunc(h.list)).ServeHTTP)
	r.Delete(mcpGrantPath, h.sessions.RequireSession(http.HandlerFunc(h.revoke)).ServeHTTP)
}

// mcpGrantJSON is one connected app: apimodel.McpGrant, with the enum fields
// typed as the contract declares them.
func mcpGrantJSON(item identity.MCPGrantItem) apimodel.McpGrant {
	scopes := make([]apimodel.McpGrantScopes, 0, len(item.Scopes))
	for _, scope := range item.Scopes {
		scopes = append(scopes, apimodel.McpGrantScopes(scope))
	}
	return apimodel.McpGrant{
		Id:                 item.ID,
		ClientName:         item.ClientName,
		ClientNameVerified: apimodel.McpGrantClientNameVerifiedFalse,
		Scopes:             scopes,
		CreatedAt:          item.CreatedAt.UTC(),
		ExpiresAt:          item.ExpiresAt.UTC(),
	}
}

func (h *MCPGrantsHandler) list(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	p, ok := httpx.ParseListParamsFor(w, r, identity.MCPGrantListMaxLimit, httpx.ListFilter(0))
	if !ok {
		return
	}
	page, err := h.service.ListGrants(r.Context(), principal.UserID, p.Limit, p.Cursor)
	if errors.Is(err, identity.ErrMCPInvalidCursor) {
		httpx.QueryProblemResponse(w, "cursor")
		return
	}
	if err != nil {
		httpx.RecordFault(r, "mcp_grant_list")
		problem(w, http.StatusServiceUnavailable, "unavailable")
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

func (h *MCPGrantsHandler) revoke(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	// Per-user, before any database call: a revoke flood cannot cost lookups.
	if !h.revokePerUser.admitAttempt(principal.UserID) {
		w.Header().Set("Retry-After", "60")
		problem(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	err := h.service.RevokeGrantForUser(r.Context(), principal.UserID, chi.URLParam(r, "grantId"))
	switch {
	case errors.Is(err, identity.ErrNotFound):
		problem(w, http.StatusNotFound, mcpGrantNotFoundCode)
	case errors.Is(err, identity.ErrUnauthenticated):
		h.sessions.clear(w)
		problem(w, http.StatusUnauthorized, "unauthenticated")
	case err != nil:
		httpx.RecordFault(r, "mcp_grant_revoke")
		problem(w, http.StatusServiceUnavailable, "unavailable")
	default:
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}
