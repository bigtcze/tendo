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

// The account MCP policy routes (ADR 0023, slice B1b1). Like the connected-apps
// routes they are signed-in browser operations: the session cookie only, never
// an MCP bearer token, the canonical Origin on writes, and they work whether or
// not TENDO_MCP_ENABLED is set.
const (
	mcpAdminPolicyPath      = "/api/v1/admin/accounts/{userId}/mcp-policy"
	mcpSettingsPath         = "/api/v1/mcp/settings"
	mcpSettingsWritePerUser = 30
	mcpPolicyBodyLimit      = 4096
	// Slice B1b2: the installation policy and the audit list (ADR 0023).
	mcpInstallationPolicyPath = "/api/v1/admin/mcp-policy"
	mcpPolicyEventsPath       = "/api/v1/admin/mcp-policy-events"
)

// mcpPolicyService is the account policy surface the adapter needs, with the
// installation policy and the audit list (B1b2).
type mcpPolicyService interface {
	RequireInstanceAdmin(ctx context.Context, actorID string) error
	AccountPolicy(ctx context.Context, actorID, userID string) (identity.MCPAccountPolicy, error)
	UpdateAccountPolicy(ctx context.Context, actorID, userID string, change identity.MCPAccountPolicyChange, expected int64) (identity.MCPAccountPolicy, error)
	Settings(ctx context.Context, userID string) (identity.MCPSettings, error)
	UpdateSettings(ctx context.Context, userID string, change identity.MCPSettingsChange, expected int64) (identity.MCPSettings, error)
	InstallationPolicy(ctx context.Context, actorID string) (identity.MCPInstallationView, error)
	UpdateInstallationPolicy(ctx context.Context, actorID string, change identity.MCPInstallationChange, expected int64) (identity.MCPInstallationView, error)
	PolicyEvents(ctx context.Context, actorID string, limit int, cursor string) (identity.MCPPolicyEventList, error)
	// Slice B1b3: the administrator grant list and revocations.
	AccountGrants(ctx context.Context, actorID, userID string, limit int, cursor string) (identity.MCPGrantPage, error)
	RevokeAccountGrant(ctx context.Context, actorID, userID, grantID string) error
	RevokeAccountGrants(ctx context.Context, actorID, userID string) error
}

// MCPPolicyHandler serves the administrator's account policy and the account
// owner's own settings.
type MCPPolicyHandler struct {
	service  mcpPolicyService
	sessions *SessionHandler
	// settingsPerUser is its own bucket, never shared with other routes.
	settingsPerUser *limiter
	// adminRevokePerActor is the administrator revoke bucket (B1b3).
	adminRevokePerActor *limiter
}

// NewMCPPolicy builds the account policy adapter.
func NewMCPPolicy(service mcpPolicyService, sessions *SessionHandler) *MCPPolicyHandler {
	return &MCPPolicyHandler{
		service: service, sessions: sessions,
		settingsPerUser:     newLimiter(mcpSettingsWritePerUser),
		adminRevokePerActor: newLimiter(mcpAdminRevokePerActor),
	}
}

// Register mounts the routes behind RequireSession. None is in
// MCPCrossOriginRoutes: a PUT or DELETE needs the canonical Origin.
func (h *MCPPolicyHandler) Register(r chi.Router) {
	admin := func(next http.HandlerFunc) http.HandlerFunc {
		return h.sessions.RequireSession(h.requireInstanceAdmin(next)).ServeHTTP
	}
	r.Get(mcpAdminPolicyPath, admin(h.getAccountPolicy))
	r.Put(mcpAdminPolicyPath, admin(h.updateAccountPolicy))
	r.Get(mcpInstallationPolicyPath, admin(h.getInstallationPolicy))
	r.Put(mcpInstallationPolicyPath, admin(h.updateInstallationPolicy))
	r.Get(mcpPolicyEventsPath, admin(h.listPolicyEvents))
	h.registerAdminGrants(r, admin)
	r.Get(mcpSettingsPath, h.sessions.RequireSession(http.HandlerFunc(h.getSettings)).ServeHTTP)
	r.Put(mcpSettingsPath, h.sessions.RequireSession(http.HandlerFunc(h.updateSettings)).ServeHTTP)
}

// requireInstanceAdmin orders the 403 before If-Match, body validation and any
// lookup of the target account.
func (h *MCPPolicyHandler) requireInstanceAdmin(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, _ := PrincipalFromContext(r.Context())
		if err := h.service.RequireInstanceAdmin(r.Context(), principal.UserID); err != nil {
			h.writeError(w, r, err, "mcp_policy_admin_check")
			return
		}
		next(w, r)
	})
}

var mcpAccountPolicyFields = map[string]httpx.Field{
	"enabled":   {Kind: httpx.KindNullableBool, Required: true},
	"maxScopes": {Kind: httpx.KindNullableStringArray, Required: true},
}

var mcpSettingsFields = map[string]httpx.Field{
	"enabled": {Kind: httpx.KindBool, Required: true},
	"scopes":  {Kind: httpx.KindStringArray, Required: true},
}

func (h *MCPPolicyHandler) getAccountPolicy(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	policy, err := h.service.AccountPolicy(r.Context(), principal.UserID, chi.URLParam(r, "userId"))
	if err != nil {
		h.writeError(w, r, err, "mcp_policy_admin_read")
		return
	}
	writeAccountPolicy(w, policy)
}

func (h *MCPPolicyHandler) updateAccountPolicy(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	expected, ok := httpx.ParseIfMatch(w, r)
	if !ok {
		return
	}
	values, ok := httpx.ReadObject(w, r, mcpAccountPolicyFields, mcpPolicyBodyLimit)
	if !ok {
		return
	}
	var change identity.MCPAccountPolicyChange
	if enabled, set := values["enabled"].(bool); set {
		change.Enabled = &enabled
	}
	if scopes, set := values["maxScopes"].([]string); set {
		change.MaxScopes = scopes
	}
	policy, err := h.service.UpdateAccountPolicy(r.Context(), principal.UserID, chi.URLParam(r, "userId"), change, expected)
	if err != nil {
		h.writeError(w, r, err, "mcp_policy_admin_update")
		return
	}
	writeAccountPolicy(w, policy)
}

func (h *MCPPolicyHandler) getSettings(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	settings, err := h.service.Settings(r.Context(), principal.UserID)
	if err != nil {
		h.writeError(w, r, err, "mcp_settings_read")
		return
	}
	writeSettings(w, settings)
}

func (h *MCPPolicyHandler) updateSettings(w http.ResponseWriter, r *http.Request) {
	principal, _ := PrincipalFromContext(r.Context())
	// Per-user, before any database call: a write flood cannot cost lookups.
	if !h.settingsPerUser.admitAttempt(principal.UserID) {
		w.Header().Set("Retry-After", "60")
		problem(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	expected, ok := httpx.ParseIfMatch(w, r)
	if !ok {
		return
	}
	values, ok := httpx.ReadObject(w, r, mcpSettingsFields, mcpPolicyBodyLimit)
	if !ok {
		return
	}
	enabled, okEnabled := values["enabled"].(bool)
	scopes, okScopes := values["scopes"].([]string)
	if !okEnabled || !okScopes {
		problem(w, http.StatusBadRequest, "invalid_request")
		return
	}
	change := identity.MCPSettingsChange{Enabled: enabled, Scopes: scopes}
	settings, err := h.service.UpdateSettings(r.Context(), principal.UserID, change, expected)
	if err != nil {
		h.writeError(w, r, err, "mcp_settings_update")
		return
	}
	writeSettings(w, settings)
}

func (h *MCPPolicyHandler) writeError(w http.ResponseWriter, r *http.Request, err error, fault string) {
	var validation *identity.ValidationError
	switch {
	case errors.Is(err, identity.ErrInstanceAdminRequired):
		problem(w, http.StatusForbidden, "instance_admin_required")
	case errors.Is(err, identity.ErrNotFound):
		problem(w, http.StatusNotFound, "not_found")
	case errors.Is(err, identity.ErrMCPPolicyPreconditionFailed):
		problem(w, http.StatusPreconditionFailed, "precondition_failed")
	case errors.Is(err, identity.ErrUnauthenticated):
		h.sessions.clear(w)
		problem(w, http.StatusUnauthorized, "unauthenticated")
	case errors.As(err, &validation):
		httpx.ValidationProblemResponse(w, validation.Field, validation.Code)
	default:
		httpx.RecordFault(r, fault)
		problem(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeAccountPolicy(w http.ResponseWriter, policy identity.MCPAccountPolicy) {
	out := apimodel.McpAccountPolicy{
		UserId: policy.UserID, Enabled: policy.AdminEnabled,
		UserEnabled: policy.UserEnabled, UserScopes: policy.UserScopes, EffectiveScopes: policy.Effective,
	}
	if policy.AdminMaxScopes != nil {
		scopes := apimodel.McpScopeList(policy.AdminMaxScopes)
		out.MaxScopes = &scopes
	}
	w.Header().Set("ETag", httpx.ETag(policy.Version))
	writeJSON(w, http.StatusOK, out)
}

func writeSettings(w http.ResponseWriter, settings identity.MCPSettings) {
	w.Header().Set("ETag", httpx.ETag(settings.Version))
	writeJSON(w, http.StatusOK, apimodel.McpSettings{
		Enabled: settings.Enabled, Scopes: settings.Scopes,
		MaximumScopes: settings.Maximum, EffectiveScopes: settings.Effective,
	})
}
