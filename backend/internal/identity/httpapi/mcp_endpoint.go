package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP Streamable HTTP endpoint (ADR 0023, Issue #456 A4a). It is one
// stateless JSON-over-POST route. Every request is authenticated against the
// database; nothing about a grant or a token is cached.
const (
	mcpEndpointPath          = "/mcp"
	mcpEndpointBodyLimit     = 64 << 10
	mcpEndpointIPPerMinute   = 300
	mcpEndpointGrantPerMin   = 120
	mcpEndpointUserPerMin    = 240
	mcpEndpointRetryAfter    = "60"
	mcpEndpointInvalidToken  = "The access token is missing, malformed, expired or revoked."
	mcpEndpointBatchError    = `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"batch requests are not supported"}}`
	mcpEndpointVersionError  = `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"MCP-Protocol-Version header required"}}`
	mcpProtocolVersionHeader = "MCP-Protocol-Version"
	// mcpEndpointDefaultVerifyTimeout bounds token verification when no database
	// timeout is configured.
	mcpEndpointDefaultVerifyTimeout = 5 * time.Second
)

// mcpTokenVerifier is the MCP service surface the endpoint needs.
type mcpTokenVerifier interface {
	Enabled() bool
	VerifyAccessToken(ctx context.Context, token string) (identity.MCPAccess, error)
}

// mcpAccessKey carries the verified MCPAccess from the front checks to the SDK
// token verifier. The SDK verifier never sees or parses the raw token.
type mcpAccessKey struct{}

// MCPEndpoint serves /mcp. It answers the unknown-path 404 while MCP is
// disabled, and never reads cookies or query or form tokens.
type MCPEndpoint struct {
	service   mcpTokenVerifier
	issuer    string
	dbTimeout time.Duration
	// Each bucket is its own limiter instance and key space.
	perIP    *limiter
	perGrant *limiter
	perUser  *limiter
	// allowed is the union of the scopes the registered tools require. A token
	// holding none of them cannot use any tool.
	allowed []string
	// guard is the SDK bearer middleware in front of the SDK handler.
	guard http.Handler
}

// NewMCPEndpoint builds the endpoint and its tools. The tool server is built
// once, here, before the SDK handler exists, so the tool list is fixed for the
// process. A malformed tool fails construction. dbTimeout bounds token
// verification; production passes cfg.DBTimeout, and a zero or negative value
// falls back to mcpEndpointDefaultVerifyTimeout (5s) so a zero-value
// configuration cannot make every verification fail immediately.
func NewMCPEndpoint(service mcpTokenVerifier, publicURL string, dbTimeout time.Duration, tools []MCPTool) (*MCPEndpoint, error) {
	server, allowed, err := buildMCPServer(tools)
	if err != nil {
		return nil, err
	}
	if dbTimeout <= 0 {
		// Production always has a positive TENDO_DB_TIMEOUT; this default only
		// keeps a zero-value configuration from making every verification fail.
		dbTimeout = mcpEndpointDefaultVerifyTimeout
	}
	sdk := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		DisableLocalhostProtection:   true,
		PropagateRequestCancellation: true,
		MaxRequestBodyBytes:          mcpEndpointBodyLimit,
		Logger:                       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return &MCPEndpoint{
		service:   service,
		issuer:    strings.TrimRight(publicURL, "/"),
		dbTimeout: dbTimeout,
		perIP:     newLimiter(mcpEndpointIPPerMinute),
		perGrant:  newLimiter(mcpEndpointGrantPerMin),
		perUser:   newLimiter(mcpEndpointUserPerMin),
		allowed:   allowed,
		guard: auth.RequireBearerToken(sdkTokenVerifier, &auth.RequireBearerTokenOptions{
			AllowMissingExpiration: true,
		})(stripCredentials(sdk)),
	}, nil
}

// stripCredentials removes the credential headers after bearer verification and
// before the SDK handler. The SDK copies the request headers into
// RequestExtra.Header, so without this a tool could read the bearer token, a
// Cookie or a Proxy-Authorization value. Tools use the principal helper only.
func stripCredentials(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del("Authorization")
		r.Header.Del("Proxy-Authorization")
		r.Header.Del("Cookie")
		next.ServeHTTP(w, r)
	})
}

// Register mounts the endpoint for every method. The handler answers the
// non-POST methods itself, so the router never does.
func (e *MCPEndpoint) Register(r chi.Router) {
	r.Handle(mcpEndpointPath, e)
}

// ServeHTTP runs the front checks in order: per-IP limit, bearer
// authentication, the per-grant and per-user limits, the scope admission, the
// body checks, and finally the SDK.
func (e *MCPEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !e.service.Enabled() {
		httpx.WriteNotFound(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		httpx.ProblemResponse(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if !e.perIP.admitAttempt(clientIP(r)) {
		writeMCPRateLimited(w)
		return
	}
	access, ok := e.authenticate(w, r)
	if !ok {
		return
	}
	if !e.perGrant.admitAttempt(access.GrantID) || !e.perUser.admitAttempt(access.UserID) {
		writeMCPRateLimited(w)
		return
	}
	if !e.admitScopes(access.Scopes) {
		e.writeInsufficientScope(w)
		return
	}
	body, ok := readMCPBody(w, r)
	if !ok {
		return
	}
	r.Header.Del("Mcp-Session-Id")
	r.Body = io.NopCloser(bytes.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), mcpAccessKey{}, access))
	e.guard.ServeHTTP(&noStoreWriter{ResponseWriter: w}, r)
}

// noStoreWriter forces Cache-Control: no-store at the first WriteHeader or
// Write. The SDK sets its own Cache-Control on SSE and JSON answers, so the
// header is set again just before the status goes out. Unwrap keeps
// http.ResponseController flushing working for SSE.
type noStoreWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *noStoreWriter) WriteHeader(status int) {
	if !w.wrote {
		w.wrote = true
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *noStoreWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *noStoreWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// authenticate reads exactly one bearer token and verifies it. On failure it
// writes the response and reports false.
func (e *MCPEndpoint) authenticate(w http.ResponseWriter, r *http.Request) (identity.MCPAccess, bool) {
	token, present, ok := bearerFromHeader(r.Header.Values("Authorization"))
	if !present {
		e.writeChallenge(w, "", "The request must present a bearer access token.")
		return identity.MCPAccess{}, false
	}
	if !ok {
		e.writeChallenge(w, "invalid_token", mcpEndpointInvalidToken)
		return identity.MCPAccess{}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), e.dbTimeout)
	defer cancel()
	access, err := e.service.VerifyAccessToken(ctx, token)
	switch {
	case errors.Is(err, identity.ErrMCPDisabled):
		httpx.WriteNotFound(w)
	case errors.Is(err, identity.ErrMCPInvalidToken):
		e.writeChallenge(w, "invalid_token", mcpEndpointInvalidToken)
	case err != nil:
		httpx.RecordFault(r, "mcp_verify")
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "Token service is temporarily unavailable.")
	default:
		return access, true
	}
	return identity.MCPAccess{}, false
}

// bearerFromHeader returns the single token of exactly one Authorization value
// with the Bearer scheme, matched case-insensitively. present is false when no
// Authorization header exists at all; ok is false for any malformed, duplicated
// or non-Bearer value.
func bearerFromHeader(values []string) (token string, present, ok bool) {
	if len(values) == 0 {
		return "", false, false
	}
	if len(values) != 1 {
		return "", true, false
	}
	fields := strings.Fields(values[0])
	if len(fields) != 2 || !strings.EqualFold(fields[0], "bearer") {
		return "", true, false
	}
	return fields[1], true, true
}

// admitScopes reports whether the token holds at least one scope that a
// registered tool requires.
func (e *MCPEndpoint) admitScopes(scopes []string) bool {
	for _, held := range scopes {
		for _, required := range e.allowed {
			if held == required {
				return true
			}
		}
	}
	return false
}

// writeChallenge writes the 401 with the protected-resource challenge. errCode is
// empty for a request that carries no credential at all.
func (e *MCPEndpoint) writeChallenge(w http.ResponseWriter, errCode, description string) {
	challenge := "Bearer "
	body := "invalid_request"
	if errCode != "" {
		challenge += `error="` + errCode + `", `
		body = errCode
	}
	w.Header().Set("WWW-Authenticate", challenge+e.resourceParams()+`, scope="tendo:read tendo:write"`)
	writeOAuthError(w, http.StatusUnauthorized, body, description)
}

func (e *MCPEndpoint) writeInsufficientScope(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="tendo:read", `+e.resourceParams())
	writeOAuthError(w, http.StatusForbidden, "insufficient_scope", "The token does not grant a scope this endpoint requires.")
}

// resourceParams is the resource_metadata parameter shared by every challenge.
func (e *MCPEndpoint) resourceParams() string {
	return `resource_metadata="` + e.issuer + mcpProtectedResourcePath + `"`
}

func writeMCPRateLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", mcpEndpointRetryAfter)
	httpx.ProblemResponse(w, http.StatusTooManyRequests, "rate_limited")
}

// readMCPBody reads at most the body limit, refuses a JSON-RPC batch (a first
// non-whitespace byte of '[') and refuses a single message that does not carry
// MCP-Protocol-Version unless it is initialize. The refusals happen here, not in
// the SDK, so the answer does not depend on the SDK's version fallback. A read
// that exceeds the limit is 413; any other read error is a plain 400 with no
// detail.
func readMCPBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, mcpEndpointBodyLimit)
	body, err := io.ReadAll(r.Body)
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		httpx.ProblemResponse(w, http.StatusRequestEntityTooLarge, "payload_too_large")
		return nil, false
	case err != nil:
		httpx.ProblemResponse(w, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	if len(trimmed) > 0 && trimmed[0] == '[' {
		writeMCPJSONRPCError(w, mcpEndpointBatchError)
		return nil, false
	}
	if r.Header.Get(mcpProtocolVersionHeader) == "" && !isInitializeMessage(body) {
		writeMCPJSONRPCError(w, mcpEndpointVersionError)
		return nil, false
	}
	return body, true
}

// writeMCPJSONRPCError writes a 400 JSON-RPC error body with a fixed payload.
func writeMCPJSONRPCError(w http.ResponseWriter, payload string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte(payload))
}

// isInitializeMessage reports whether the body is a single JSON-RPC message
// whose method is initialize. Only initialize may omit the protocol header: it
// is the request that negotiates the version. Every other message, including
// notifications/initialized, carries the header (MCP 2025-06-18, basic/
// transports, "Protocol Version Header"). A body that does not parse is not
// initialize, so it is refused by the header rule and never reaches the SDK
// without a version. It decodes with the SDK's own JSON-RPC decoder, so the
// method it sees is the method the SDK dispatches (encoding/json would match
// "Method" or "METHOD" case-insensitively and disagree).
func isInitializeMessage(body []byte) bool {
	msg, err := jsonrpc.DecodeMessage(body)
	if err != nil {
		return false
	}
	req, ok := msg.(*jsonrpc.Request)
	return ok && req.Method == "initialize"
}

// sdkTokenVerifier builds the SDK TokenInfo from the MCPAccess the front checks
// already verified. It never parses the raw token.
func sdkTokenVerifier(_ context.Context, _ string, r *http.Request) (*auth.TokenInfo, error) {
	access, ok := r.Context().Value(mcpAccessKey{}).(identity.MCPAccess)
	if !ok {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{
		Scopes: access.Scopes,
		UserID: access.UserID,
		Extra:  map[string]any{"grant_id": access.GrantID, "client_id": access.ClientID},
	}, nil
}
