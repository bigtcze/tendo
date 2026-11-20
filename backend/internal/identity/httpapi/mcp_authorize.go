package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
)

// The browser consent flow (ADR 0023, slice A3a). /oauth/authorize never
// redirects to the client: every outcome is a 303 to Tendo's own /connect page,
// and the client redirect happens only after the signed-in human approves.
const (
	mcpAuthorizePath             = "/oauth/authorize"
	mcpAuthorizationRequestPath  = "/api/v1/mcp/authorization-requests/{requestHandle}"
	mcpAuthorizationDecisionPath = mcpAuthorizationRequestPath + "/decision"
	mcpConnectPath               = "/connect"
	mcpConsentCookieSecure       = "__Secure-tendo_mcp_consent"
	mcpConsentCookiePlain        = "tendo_mcp_consent"
	mcpConsentMaxAgeSeconds      = 300
	mcpAuthorizeIPPerMinute      = 20
	mcpAuthorizeGlobalPerMinute  = 120
	mcpDecisionPerUserPerMinute  = 30
	mcpDecisionBodyLimit         = 4096
)

// authorizeRecognized are the /oauth/authorize parameters the adapter reads.
// Any other parameter is ignored.
var authorizeRecognized = []string{"response_type", "client_id", "redirect_uri", "code_challenge", "code_challenge_method", "scope", "resource", "state"}

// authorize is the browser entry point. It reads no cookie: a session cookie
// present in the request changes nothing here.
func (h *MCPHandler) authorize(w http.ResponseWriter, r *http.Request) {
	if !h.service.Enabled() {
		httpx.WriteNotFound(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !h.authorizeIP.admitAttempt(clientIP(r)) || !h.authorizeGlobal.admitAttempt("global") {
		redirectConnectError(w, "temporarily_unavailable")
		return
	}
	params, code := parseAuthorizeQuery(r.URL.RawQuery)
	if code != "" {
		redirectConnectError(w, code)
		return
	}
	pending, err := h.service.BeginAuthorization(r.Context(), params)
	if err != nil {
		code, known := authorizeErrorCode(err)
		if !known {
			httpx.RecordFault(r, "mcp_authorize")
			code = "server_error"
		}
		redirectConnectError(w, code)
		return
	}
	h.setConsentCookie(w, pending.Handle, pending.BrowserBinding)
	redirectConnect(w, url.Values{"request": {pending.Handle}})
}

// parseAuthorizeQuery turns the URL query into service parameters. A refusal
// is returned as its RFC 6749 error code.
func parseAuthorizeQuery(raw string) (identity.MCPAuthorizationParams, string) {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return identity.MCPAuthorizationParams{}, "invalid_request"
	}
	if code, repeated := repeatedParameter(values, authorizeRecognized); repeated {
		return identity.MCPAuthorizationParams{}, code
	}
	switch values.Get("response_type") {
	case "":
		return identity.MCPAuthorizationParams{}, "invalid_request"
	case "code":
	default:
		return identity.MCPAuthorizationParams{}, "unsupported_response_type"
	}
	return identity.MCPAuthorizationParams{
		ClientID:            values.Get("client_id"),
		RedirectURI:         values.Get("redirect_uri"),
		CodeChallenge:       values.Get("code_challenge"),
		CodeChallengeMethod: values.Get("code_challenge_method"),
		Scopes:              splitScopes(values.Get("scope")),
		Resource:            values.Get("resource"),
		State:               values.Get("state"),
	}, ""
}

// splitScopes returns nil for an empty or blank scope, which is absent: the
// service then applies both Tendo scopes.
func splitScopes(value string) []string {
	scopes := strings.Fields(value)
	if len(scopes) == 0 {
		return nil
	}
	return scopes
}

// repeatedParameter refuses a recognized parameter that appears more than once.
// A repeated resource is invalid_target (RFC 8707); every other one is
// invalid_request.
func repeatedParameter(values url.Values, recognized []string) (string, bool) {
	for _, name := range recognized {
		if len(values[name]) > 1 {
			if name == "resource" {
				return "invalid_target", true
			}
			return "invalid_request", true
		}
	}
	return "", false
}

// authorizeErrorCode maps a service refusal to its RFC 6749 code. An unknown
// error reports false so the caller records a fault and answers server_error.
func authorizeErrorCode(err error) (string, bool) {
	switch {
	case errors.Is(err, identity.ErrMCPInvalidRequest):
		return "invalid_request", true
	case errors.Is(err, identity.ErrMCPInvalidScope):
		return "invalid_scope", true
	case errors.Is(err, identity.ErrMCPInvalidTarget):
		return "invalid_target", true
	case errors.Is(err, identity.ErrMCPInvalidClient):
		return "invalid_client", true
	case errors.Is(err, identity.ErrMCPInvalidRedirectURI):
		return "invalid_redirect_uri", true
	}
	return "", false
}

// redirectConnect sends the browser to Tendo's consent page. The values sit in
// the fragment, so they never reach a server log or a Referer.
func redirectConnect(w http.ResponseWriter, fragment url.Values) {
	w.Header().Set("Location", mcpConnectPath+"#"+fragment.Encode())
	w.WriteHeader(http.StatusSeeOther)
}

func redirectConnectError(w http.ResponseWriter, code string) {
	redirectConnect(w, url.Values{"error": {code}})
}

// consentSecure follows the public URL: the __Secure- cookie needs HTTPS.
func (h *MCPHandler) consentSecure() bool { return strings.HasPrefix(h.issuer, "https://") }

func (h *MCPHandler) consentCookieName() string {
	if h.consentSecure() {
		return mcpConsentCookieSecure
	}
	return mcpConsentCookiePlain
}

// consentCookiePath scopes the binding to one pending consent, so parallel
// consents in one browser do not overwrite each other.
func consentCookiePath(handle string) string {
	return "/api/v1/mcp/authorization-requests/" + handle
}

func (h *MCPHandler) setConsentCookie(w http.ResponseWriter, handle, binding string) {
	http.SetCookie(w, &http.Cookie{
		Name: h.consentCookieName(), Value: binding, Path: consentCookiePath(handle),
		HttpOnly: true, Secure: h.consentSecure(), SameSite: http.SameSiteLaxMode, MaxAge: mcpConsentMaxAgeSeconds,
	})
}

// clearConsentCookie expires the binding cookie for a well-formed handle only.
// A malformed handle has no cookie Path that Tendo ever issued, so there is
// nothing to clear and no Set-Cookie is written.
func (h *MCPHandler) clearConsentCookie(w http.ResponseWriter, handle string) {
	if !validRequestHandle(handle) {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: h.consentCookieName(), Value: "", Path: consentCookiePath(handle),
		HttpOnly: true, Secure: h.consentSecure(), SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// validRequestHandle reports whether handle has the shape Tendo issues: 43
// base64url characters.
func validRequestHandle(handle string) bool {
	if len(handle) != 43 {
		return false
	}
	for i := 0; i < len(handle); i++ {
		c := handle[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// consentBinding returns the single binding cookie. A missing or duplicated
// cookie is treated as not found, without asking the service.
func (h *MCPHandler) consentBinding(r *http.Request) (string, bool) {
	name := h.consentCookieName()
	var value string
	count := 0
	for _, c := range r.Cookies() {
		if c.Name == name {
			value = c.Value
			count++
		}
	}
	return value, count == 1
}

// guarded answers the unknown-path 404 while MCP is disabled, before any
// session lookup, and otherwise requires a session.
func (h *MCPHandler) guarded(next http.HandlerFunc) http.Handler {
	authenticated := h.sessions.RequireSession(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.service.Enabled() {
			httpx.WriteNotFound(w)
			return
		}
		authenticated.ServeHTTP(w, r)
	})
}

// authorizationRequestJSON is the consent screen view. The client name is
// unverified: it comes from the client's own registration.
type authorizationRequestJSON struct {
	ClientName         string   `json:"clientName"`
	ClientNameVerified bool     `json:"clientNameVerified"`
	RedirectURI        string   `json:"redirectUri"`
	Scopes             []string `json:"scopes"`
	ExpiresAt          string   `json:"expiresAt"`
}

func (h *MCPHandler) getAuthorizationRequest(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "requestHandle")
	binding, ok := h.consentBinding(r)
	if !ok {
		problem(w, http.StatusNotFound, "authorization_request_not_found")
		return
	}
	view, err := h.service.GetAuthorizationRequest(r.Context(), handle, binding)
	switch {
	case errors.Is(err, identity.ErrMCPAuthorizationRequestNotFound):
		problem(w, http.StatusNotFound, "authorization_request_not_found")
	case err != nil:
		httpx.RecordFault(r, "mcp_authorization_request")
		problem(w, http.StatusServiceUnavailable, "unavailable")
	default:
		scopes := view.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		writeJSON(w, http.StatusOK, authorizationRequestJSON{
			ClientName: view.ClientName, ClientNameVerified: false, RedirectURI: view.RedirectURI,
			Scopes: scopes, ExpiresAt: view.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}
}

func (h *MCPHandler) decideAuthorizationRequest(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "requestHandle")
	principal, _ := PrincipalFromContext(r.Context())
	token, ok := h.sessions.sessionToken(r)
	if !ok {
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	// Per-user, before the body is read: a decision flood cannot cost body I/O.
	if !h.decisions.admitAttempt(principal.UserID) {
		w.Header().Set("Retry-After", mcpRegisterRetryAfterSeconds)
		problem(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	binding, ok := h.consentBinding(r)
	if !ok {
		h.writeDecisionNotFound(w, handle)
		return
	}
	values, ok := httpx.ReadObject(w, r, map[string]httpx.Field{
		"decision": {Kind: httpx.KindString, Required: true},
	}, mcpDecisionBodyLimit)
	if !ok {
		return
	}
	decision := stringField(values, "decision")
	if decision != "approve" && decision != "deny" {
		httpx.ProblemResponse(w, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := h.service.DecideAuthorization(r.Context(), identity.MCPDecision{
		Principal: principal, SessionToken: token, Handle: handle, BrowserBinding: binding, Approve: decision == "approve",
	})
	switch {
	case errors.Is(err, identity.ErrUnauthenticated):
		h.sessions.clear(w)
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	case errors.Is(err, identity.ErrMCPAuthorizationRequestNotFound):
		h.writeDecisionNotFound(w, handle)
		return
	case err != nil:
		httpx.RecordFault(r, "mcp_decision")
		problem(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	redirectTo, err := decisionRedirect(result, h.issuer)
	if err != nil {
		httpx.RecordFault(r, "mcp_decision")
		problem(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	h.clearConsentCookie(w, handle)
	writeJSON(w, http.StatusOK, map[string]string{"redirectTo": redirectTo})
}

func (h *MCPHandler) writeDecisionNotFound(w http.ResponseWriter, handle string) {
	h.clearConsentCookie(w, handle)
	problem(w, http.StatusNotFound, "authorization_request_not_found")
}

// decisionRedirect appends the authorization response to the registered
// redirect URI. The existing query is kept byte for byte; the issuer is
// included so the client can check the response came from this server (RFC 9207).
func decisionRedirect(result identity.MCPDecisionResult, issuer string) (string, error) {
	target, err := url.Parse(result.RedirectURI)
	if err != nil {
		return "", err
	}
	extra := url.Values{}
	if result.Code != "" {
		extra.Set("code", result.Code)
	} else {
		extra.Set("error", "access_denied")
	}
	if result.State != "" {
		extra.Set("state", result.State)
	}
	extra.Set("iss", issuer)
	if target.RawQuery == "" {
		target.RawQuery = extra.Encode()
	} else {
		target.RawQuery += "&" + extra.Encode()
	}
	return target.String(), nil
}
