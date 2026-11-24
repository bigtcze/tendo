package httpapi

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
)

// The token and revocation endpoints (RFC 6749 section 3.2, RFC 7009). Both are
// public-client, form-encoded POSTs. They never read or set cookies, so a
// browser session cannot ride along and the CORS policy stays credential-free.
const (
	mcpTokenPath               = "/oauth/token"
	mcpRevokePath              = "/oauth/revoke"
	mcpTokenBodyLimit          = 8 << 10
	mcpTokenPerClientPerMinute = 60
	mcpTokenGlobalPerMinute    = 600
	mcpRevokePerIPPerMinute    = 30
	mcpTokenRetryAfterSeconds  = "60"
)

var (
	tokenRecognized  = []string{"grant_type", "code", "redirect_uri", "client_id", "code_verifier", "refresh_token", "resource", "scope"}
	revokeRecognized = []string{"token", "client_id", "token_type_hint"}
)

// mcpTokenResponse is the RFC 6749 section 5.1 success body.
type mcpTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// readFormBody checks the media type and reads the body with a hard bound. It
// writes the RFC error and reports false on any refusal. Parameters come from
// the body only: the URL query is never consulted here.
func readFormBody(w http.ResponseWriter, r *http.Request, recognized []string) (url.Values, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Content-Type must be application/x-www-form-urlencoded.")
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, mcpTokenBodyLimit+1))
	if err != nil || len(body) > mcpTokenBodyLimit {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Request body is not acceptable.")
		return nil, false
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Request body is not acceptable.")
		return nil, false
	}
	if code, repeated := repeatedParameter(values, recognized); repeated {
		writeOAuthError(w, http.StatusBadRequest, code, "A request parameter is repeated.")
		return nil, false
	}
	return values, true
}

// tokenRateLimited applies the token endpoint buckets after the body is parsed.
// The global bucket is charged first: once it is full, a caller-chosen client_id
// cannot add keys to the per-client map, because the per-client bucket is never
// reached. A request refused by the per-client bucket still spent a global slot.
func (h *MCPHandler) tokenRateLimited(w http.ResponseWriter, r *http.Request, clientID string) bool {
	if h.tokenGlobal.admitAttempt("global") && h.tokenPerClient.admitAttempt(clientIP(r)+"|"+clientID) {
		return false
	}
	w.Header().Set("Retry-After", mcpTokenRetryAfterSeconds)
	writeOAuthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "Too many token requests. Retry later.")
	return true
}

func (h *MCPHandler) token(w http.ResponseWriter, r *http.Request) {
	if !h.service.Enabled() {
		httpx.WriteNotFound(w)
		return
	}
	setMCPCORS(w)
	values, ok := readFormBody(w, r, tokenRecognized)
	if !ok {
		return
	}
	if h.tokenRateLimited(w, r, values.Get("client_id")) {
		return
	}
	// The resource must be the one configured resource when present, else the
	// configured resource applies. A repeated resource was refused above.
	if values.Has("resource") && values.Get("resource") != h.service.Resource() {
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "The resource is not served.")
		return
	}
	switch values.Get("grant_type") {
	case "":
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "grant_type is required.")
	case "authorization_code":
		h.exchangeCode(w, r, values)
	case "refresh_token":
		h.refreshToken(w, r, values)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "The grant type is not supported.")
	}
}

func (h *MCPHandler) exchangeCode(w http.ResponseWriter, r *http.Request, values url.Values) {
	for _, name := range []string{"code", "redirect_uri", "client_id", "code_verifier"} {
		if values.Get(name) == "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "A required parameter is missing.")
			return
		}
	}
	tokens, err := h.service.ExchangeCode(r.Context(), identity.MCPCodeExchange{
		Code: values.Get("code"), CodeVerifier: values.Get("code_verifier"), ClientID: values.Get("client_id"),
		RedirectURI: values.Get("redirect_uri"), Resource: h.service.Resource(),
	})
	h.writeTokenResult(w, r, "mcp_token", tokens, err)
}

func (h *MCPHandler) refreshToken(w http.ResponseWriter, r *http.Request, values url.Values) {
	for _, name := range []string{"refresh_token", "client_id"} {
		if values.Get(name) == "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "A required parameter is missing.")
			return
		}
	}
	tokens, err := h.service.Refresh(r.Context(), identity.MCPRefresh{
		RefreshToken: values.Get("refresh_token"), ClientID: values.Get("client_id"), Resource: h.service.Resource(),
		Scopes: strings.Fields(values.Get("scope")),
	})
	h.writeTokenResult(w, r, "mcp_token", tokens, err)
}

// writeTokenResult maps the service outcome to the RFC shape. An unknown client
// is invalid_grant (declared deviation: no client oracle).
func (h *MCPHandler) writeTokenResult(w http.ResponseWriter, r *http.Request, fault string, tokens identity.MCPTokens, err error) {
	switch {
	case errors.Is(err, identity.ErrMCPDisabled):
		// The installation policy switched MCP off after routing admitted the
		// request: the same 404 as the operator switch.
		httpx.WriteNotFound(w)
		return
	case errors.Is(err, identity.ErrMCPInvalidGrant):
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "The grant is invalid or expired.")
		return
	case errors.Is(err, identity.ErrMCPInvalidScope):
		writeOAuthError(w, http.StatusBadRequest, "invalid_scope", "The requested scope is not available.")
		return
	case errors.Is(err, identity.ErrMCPInvalidTarget):
		writeOAuthError(w, http.StatusBadRequest, "invalid_target", "The resource is not served.")
		return
	case err != nil:
		httpx.RecordFault(r, fault)
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "Token service is temporarily unavailable.")
		return
	}
	expiresIn := int64(time.Until(tokens.AccessExpiresAt).Seconds())
	if expiresIn < 1 {
		expiresIn = 1
	}
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, mcpTokenResponse{
		AccessToken: tokens.AccessToken, TokenType: "Bearer", ExpiresIn: expiresIn,
		RefreshToken: tokens.RefreshToken, Scope: strings.Join(tokens.Scopes, " "),
	})
}

func (h *MCPHandler) revoke(w http.ResponseWriter, r *http.Request) {
	if !h.service.Enabled() {
		httpx.WriteNotFound(w)
		return
	}
	setMCPCORS(w)
	values, ok := readFormBody(w, r, revokeRecognized)
	if !ok {
		return
	}
	if !h.revokePerIP.admitAttempt(clientIP(r)) {
		w.Header().Set("Retry-After", mcpTokenRetryAfterSeconds)
		writeOAuthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "Too many revocation requests. Retry later.")
		return
	}
	if values.Get("token") == "" || values.Get("client_id") == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token and client_id are required.")
		return
	}
	// token_type_hint is accepted and ignored: the token is looked up by its
	// digest either way.
	if err := h.service.RevokeToken(r.Context(), identity.MCPTokenRevocation{
		Token: values.Get("token"), ClientID: values.Get("client_id"),
	}); err != nil {
		httpx.RecordFault(r, "mcp_revoke")
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "Revocation is temporarily unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}
