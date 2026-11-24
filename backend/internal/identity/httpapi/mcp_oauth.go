package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

// The MCP OAuth well-known and registration paths (ADR 0023). They are exact:
// the root protected-resource document is not served.
const (
	mcpRegisterPath              = "/oauth/register"
	mcpProtectedResourcePath     = "/.well-known/oauth-protected-resource/mcp"
	mcpAuthorizationServerPath   = "/.well-known/oauth-authorization-server"
	mcpRegisterPerIPPerMinute    = 10
	mcpRegisterGlobalPerMinute   = 60
	mcpRegisterRetryAfterSeconds = "60"
	mcpPreflightMaxAge           = "600"
)

// mcpRegistrar is the MCP service surface the adapter needs. While Available
// reports false every gated endpoint answers the router 404 (slice B1b2).
type mcpRegistrar interface {
	Available(ctx context.Context) (bool, error)
	RegisterClient(ctx context.Context, reg identity.MCPClientRegistration) (identity.MCPRegisteredClient, error)
	BeginAuthorization(ctx context.Context, in identity.MCPAuthorizationParams) (identity.MCPPendingAuthorization, error)
	GetAuthorizationRequest(ctx context.Context, handle, browserBinding string) (identity.MCPAuthorizationRequestView, error)
	DecideAuthorization(ctx context.Context, in identity.MCPDecision) (identity.MCPDecisionResult, error)
	ExchangeCode(ctx context.Context, in identity.MCPCodeExchange) (identity.MCPTokens, error)
	Refresh(ctx context.Context, in identity.MCPRefresh) (identity.MCPTokens, error)
	RevokeToken(ctx context.Context, in identity.MCPTokenRevocation) error
	Resource() string
}

// MCPHandler serves dynamic client registration (RFC 7591) and the RFC 9728 and
// RFC 8414 metadata documents. It never reads or sets cookies and sends no
// credentials-bearing CORS headers.
type MCPHandler struct {
	service mcpRegistrar
	// sessions authenticates the consent decision and reads the raw session
	// token. The token endpoints never touch it.
	sessions *SessionHandler
	// issuer is the normalized public origin: TENDO_PUBLIC_URL without a
	// trailing slash. It is the authorization server issuer and the base of
	// every advertised endpoint.
	issuer string
	perIP  *limiter
	global *limiter
	// The consent and token buckets each have their own limiter instance and
	// key space, never shared with registration or login.
	authorizeIP     *limiter
	authorizeGlobal *limiter
	decisions       *limiter
	tokenPerClient  *limiter
	tokenGlobal     *limiter
	revokePerIP     *limiter
}

// NewMCP builds the MCP HTTP adapter. publicURL is the canonical public URL.
// sessions supplies RequireSession and the raw session token for the consent
// decision.
func NewMCP(service mcpRegistrar, sessions *SessionHandler, publicURL string) *MCPHandler {
	return &MCPHandler{
		service:  service,
		sessions: sessions,
		issuer:   strings.TrimRight(publicURL, "/"),
		perIP:    newLimiter(mcpRegisterPerIPPerMinute),
		// The global bucket has its own instance and key space; it is never
		// shared with the per-IP limiter or with login.
		global:          newLimiter(mcpRegisterGlobalPerMinute),
		authorizeIP:     newLimiter(mcpAuthorizeIPPerMinute),
		authorizeGlobal: newLimiter(mcpAuthorizeGlobalPerMinute),
		decisions:       newLimiter(mcpDecisionPerUserPerMinute),
		tokenPerClient:  newLimiter(mcpTokenPerClientPerMinute),
		tokenGlobal:     newLimiter(mcpTokenGlobalPerMinute),
		revokePerIP:     newLimiter(mcpRevokePerIPPerMinute),
	}
}

// MCPCrossOriginRoutes lists the endpoints that are exempt from the browser
// Origin check. Any Origin is ignored on them; the canonical-authority check
// still runs. Composition passes this list to the origin policy.
func MCPCrossOriginRoutes() []httpx.CrossOriginRoute {
	return []httpx.CrossOriginRoute{
		{Method: http.MethodPost, Path: mcpRegisterPath, Mode: httpx.CrossOriginAny},
		{Method: http.MethodGet, Path: mcpProtectedResourcePath, Mode: httpx.CrossOriginAny},
		{Method: http.MethodGet, Path: mcpAuthorizationServerPath, Mode: httpx.CrossOriginAny},
		{Method: http.MethodPost, Path: mcpTokenPath, Mode: httpx.CrossOriginAny},
		{Method: http.MethodPost, Path: mcpRevokePath, Mode: httpx.CrossOriginAny},
		// A native or CLI MCP client sends no Origin. A browser Origin must be
		// the public origin, so a foreign page cannot drive the endpoint.
		{Method: http.MethodPost, Path: mcpEndpointPath, Mode: httpx.CrossOriginNoneOrPublic},
	}
}

// Register mounts the MCP endpoints and their CORS preflights. Each OPTIONS
// route exists so a preflight is answered here instead of as a method error.
// /oauth/authorize is a browser navigation and has no preflight.
func (h *MCPHandler) Register(r chi.Router) {
	r.Post(mcpRegisterPath, h.register)
	r.Options(mcpRegisterPath, h.preflight(http.MethodPost))
	r.Get(mcpProtectedResourcePath, h.protectedResource)
	r.Options(mcpProtectedResourcePath, h.preflight(http.MethodGet))
	r.Get(mcpAuthorizationServerPath, h.authorizationServer)
	r.Options(mcpAuthorizationServerPath, h.preflight(http.MethodGet))
	r.Get(mcpAuthorizePath, h.authorize)
	r.Get(mcpAuthorizationRequestPath, h.guarded(h.getAuthorizationRequest).ServeHTTP)
	r.Post(mcpAuthorizationDecisionPath, h.guarded(h.decideAuthorizationRequest).ServeHTTP)
	r.Post(mcpTokenPath, h.token)
	r.Options(mcpTokenPath, h.preflight(http.MethodPost))
	r.Post(mcpRevokePath, h.revoke)
	r.Options(mcpRevokePath, h.preflight(http.MethodPost))
}

// gate runs the installation gate (ADR 0023, slice B1b2) before anything else
// on a gated route. It reports whether the route continues. Otherwise it has
// answered: the router 404 while the switch is off, or the route's own
// fail-closed answer when the switch cannot be read. The fault class is
// recorded, never the driver text.
func (h *MCPHandler) gate(w http.ResponseWriter, r *http.Request, fault func(http.ResponseWriter, *http.Request)) bool {
	on, err := h.service.Available(r.Context())
	if err != nil {
		httpx.RecordFault(r, "mcp_gate")
		fault(w, r)
		return false
	}
	if !on {
		httpx.WriteNotFound(w)
		return false
	}
	return true
}

// gateOAuthUnavailable is the OAuth 503 for the token, registration, revocation
// and Streamable HTTP routes.
func gateOAuthUnavailable(w http.ResponseWriter, _ *http.Request) {
	writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "The MCP service is temporarily unavailable.")
}

// gateProblemUnavailable is the problem 503 for the discovery documents,
// preflights and consent routes.
func gateProblemUnavailable(w http.ResponseWriter, _ *http.Request) {
	problem(w, http.StatusServiceUnavailable, "unavailable")
}

// gateAuthorizeServerError answers a fault of the installation gate on the
// browser entry point. It carries the same no-store and no-referrer headers as
// every other /oauth/authorize answer, so the fault is never cached or leaks
// the connect URL through a Referer.
func gateAuthorizeServerError(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	redirectConnectError(w, "server_error")
}

// preflight answers a CORS preflight. A disabled service answers the router
// 404, exactly as an unknown reserved path does.
func (h *MCPHandler) preflight(method string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.gate(w, r, gateProblemUnavailable) {
			return
		}
		setMCPCORS(w)
		w.Header().Set("Access-Control-Allow-Methods", method+", OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Max-Age", mcpPreflightMaxAge)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	}
}

// setMCPCORS adds the credential-free CORS headers. Allow-Credentials is never set.
func setMCPCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Expose-Headers", "Retry-After")
}

func (h *MCPHandler) protectedResource(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r, gateProblemUnavailable) {
		return
	}
	setMCPCORS(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 h.issuer + "/mcp",
		"authorization_servers":    []string{h.issuer},
		"scopes_supported":         []string{identity.MCPScopeRead, identity.MCPScopeWrite},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Tendo",
	})
}

func (h *MCPHandler) authorizationServer(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r, gateProblemUnavailable) {
		return
	}
	setMCPCORS(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         h.issuer,
		"authorization_endpoint":                         h.issuer + "/oauth/authorize",
		"token_endpoint":                                 h.issuer + "/oauth/token",
		"registration_endpoint":                          h.issuer + mcpRegisterPath,
		"revocation_endpoint":                            h.issuer + "/oauth/revoke",
		"scopes_supported":                               []string{identity.MCPScopeRead, identity.MCPScopeWrite},
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"code_challenge_methods_supported":               []string{"S256"},
		"authorization_response_iss_parameter_supported": true,
	})
}

// registrationResponse is the RFC 7591 §3.2.1 response. The field names are the
// RFC-defined snake_case wire names, an exception to API.md camelCase.
type registrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

func (h *MCPHandler) register(w http.ResponseWriter, r *http.Request) {
	if !h.gate(w, r, gateOAuthUnavailable) {
		return
	}
	setMCPCORS(w)
	if !isJSONMediaType(r.Header.Get("Content-Type")) {
		writeOAuthError(w, http.StatusUnsupportedMediaType, "invalid_client_metadata", "Content-Type must be application/json.")
		return
	}
	// Rate limits run before the body is read so a flood cannot cost body I/O.
	if !h.perIP.admitAttempt(clientIP(r)) || !h.global.admitAttempt("global") {
		w.Header().Set("Retry-After", mcpRegisterRetryAfterSeconds)
		writeOAuthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "Too many registrations. Retry later.")
		return
	}
	body, ok := readRegistrationBody(r)
	if !ok {
		writeOAuthError(w, http.StatusRequestEntityTooLarge, "invalid_client_metadata", "Registration body is too large.")
		return
	}
	reg, err := identity.DecodeClientRegistration(body)
	if err != nil {
		writeRegistrationError(w, err)
		return
	}
	client, err := h.service.RegisterClient(r.Context(), reg)
	if errors.Is(err, identity.ErrMCPRegistrationFull) {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "Client registration is temporarily unavailable.")
		return
	}
	if err != nil {
		if errors.Is(err, identity.ErrMCPInvalidRedirectURI) || errors.Is(err, identity.ErrMCPInvalidClientMetadata) {
			writeRegistrationError(w, err)
			return
		}
		httpx.RecordFault(r, "mcp_register")
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "Client registration is temporarily unavailable.")
		return
	}
	writeJSON(w, http.StatusCreated, registrationResponse{
		ClientID:                client.ID,
		ClientIDIssuedAt:        client.IssuedAt.Unix(),
		ClientName:              client.Name,
		RedirectURIs:            client.RedirectURIs,
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		TokenEndpointAuthMethod: "none",
		Scope:                   identity.MCPScopeRead + " " + identity.MCPScopeWrite,
	})
}

// readRegistrationBody reads at most the registration limit. It reports false
// when the body is larger.
func readRegistrationBody(r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, identity.MCPMaxRegistrationBytes+1))
	if err != nil || len(body) > identity.MCPMaxRegistrationBytes {
		return nil, false
	}
	return body, true
}

// isJSONMediaType accepts application/json with optional parameters such as
// charset, and nothing else.
func isJSONMediaType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}

func writeRegistrationError(w http.ResponseWriter, err error) {
	if errors.Is(err, identity.ErrMCPInvalidRedirectURI) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "A redirect URI is not permitted.")
		return
	}
	writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "Client metadata is not acceptable.")
}

// writeOAuthError writes the RFC 6749 §5.2 / RFC 7591 §3.2.2 error shape. Only
// fixed descriptions are used, never driver or decoder text.
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": description})
}
