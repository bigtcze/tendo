package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

type sessionService interface {
	Login(ctx context.Context, login, password string) (identity.Session, error)
	Lookup(ctx context.Context, token string) (identity.SessionInfo, error)
	Logout(ctx context.Context, token string) error
	SetDefaultHousehold(ctx context.Context, userID, householdID string) error
}

type SessionHandler struct {
	service sessionService
	secure  bool
	limiter *limiter
}

// NewSession builds the session handler. The cookie policy follows publicURL:
// an https URL yields the __Host- prefixed Secure cookie.
func NewSession(s sessionService, publicURL string) *SessionHandler {
	return &SessionHandler{service: s, secure: strings.HasPrefix(publicURL, "https://"), limiter: newLimiter(10)}
}
func (h *SessionHandler) Register(r chi.Router) {
	r.Post("/api/v1/session", h.create)
	r.Get("/api/v1/session", h.get)
	r.Patch("/api/v1/session", h.update)
	r.Delete("/api/v1/session", h.delete)
}

func (h *SessionHandler) cookieName() string {
	if h.secure {
		return "__Host-tendo_session"
	}
	return "tendo_session"
}

func (h *SessionHandler) setCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: h.cookieName(), Value: token, Path: "/", HttpOnly: true,
		Secure: h.secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
}

func (h *SessionHandler) clear(w http.ResponseWriter) { h.setCookie(w, "", -1) }

// sessionToken returns the single session cookie value; missing or duplicated
// cookies are rejected.
func (h *SessionHandler) sessionToken(r *http.Request) (string, bool) {
	var token string
	count := 0
	for _, c := range r.Cookies() {
		if c.Name == h.cookieName() {
			token = c.Value
			count++
		}
	}
	return token, count == 1
}

func (h *SessionHandler) create(w http.ResponseWriter, r *http.Request) {
	if !h.limiter.admitAttempt(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return
	}
	values, ok := httpx.ReadObject(w, r, map[string]httpx.Field{
		"login":    {Kind: httpx.KindString, Required: true},
		"password": {Kind: httpx.KindString, Required: true},
	}, 4096)
	if !ok {
		return
	}
	session, err := h.service.Login(r.Context(), stringField(values, "login"), stringField(values, "password"))
	if errors.Is(err, security.ErrPasswordWorkLimit) {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return
	}
	if errors.Is(err, identity.ErrInvalidCredentials) {
		recordCredentialFault(r, err)
		problem(w, 401, "invalid_credentials")
		return
	}
	if err != nil {
		problem(w, 503, "unavailable")
		return
	}
	h.setCookie(w, session.Token, int(identity.SessionLifetime/time.Second))
	w.Header().Set("Location", "/api/v1/session")
	writeJSON(w, 201, sessionResponse(session.Principal, session.ExpiresAt, session.SignInMethod))
}

func (h *SessionHandler) get(w http.ResponseWriter, r *http.Request) {
	info, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	w.Header().Set("ETag", `"session-`+info.ID+`"`)
	writeJSON(w, 200, sessionResponse(info.Principal, info.ExpiresAt, info.SignInMethod))
}

// update selects the account-wide default household. The value is a navigation
// hint, never an authorization grant: the persisting statement also proves the
// caller's membership, and every protected operation re-authorizes on its own.
// There is no ETag and no If-Match; the last write wins.
func (h *SessionHandler) update(w http.ResponseWriter, r *http.Request) {
	info, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	values, ok := httpx.ReadObject(w, r, map[string]httpx.Field{
		"defaultHouseholdId": {Kind: httpx.KindString, Required: true},
	}, 4096)
	if !ok {
		return
	}
	householdID := strings.ToLower(stringField(values, "defaultHouseholdId"))
	switch err := h.service.SetDefaultHousehold(r.Context(), info.Principal.UserID, householdID); {
	case errors.Is(err, identity.ErrUnauthenticated):
		h.clear(w)
		problem(w, 401, "unauthenticated")
	case errors.Is(err, identity.ErrInvalidReference):
		httpx.ValidationProblemResponse(w, "defaultHouseholdId", "invalid_reference")
	case err != nil:
		problem(w, 503, "unavailable")
	default:
		info.Principal.DefaultHouseholdID = householdID
		writeJSON(w, 200, sessionResponse(info.Principal, info.ExpiresAt, info.SignInMethod))
	}
}

// authenticate resolves the session cookie. Only a definite 401 clears the
// cookie; a persistence outage answers 503 and leaves the cookie intact.
func (h *SessionHandler) authenticate(w http.ResponseWriter, r *http.Request) (identity.SessionInfo, bool) {
	w.Header().Set("Cache-Control", "no-store")
	token, ok := h.sessionToken(r)
	if !ok {
		problem(w, 401, "unauthenticated")
		return identity.SessionInfo{}, false
	}
	info, err := h.service.Lookup(r.Context(), token)
	if errors.Is(err, identity.ErrUnauthenticated) {
		h.clear(w)
		problem(w, 401, "unauthenticated")
		return identity.SessionInfo{}, false
	}
	if err != nil {
		problem(w, 503, "unavailable")
		return identity.SessionInfo{}, false
	}
	return info, true
}

func (h *SessionHandler) delete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if token, ok := h.sessionToken(r); ok {
		if err := h.service.Logout(r.Context(), token); err != nil {
			problem(w, 503, "unavailable")
			return
		}
	}
	h.clear(w)
	w.WriteHeader(204)
}

type principalKey struct{}
type sessionInfoKey struct{}

// RequireSession authenticates the request and exposes the principal through
// PrincipalFromContext. It is the seam later household-scoped routes use.
func (h *SessionHandler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := h.authenticate(w, r)
		if !ok {
			return
		}
		ctx := context.WithValue(r.Context(), principalKey{}, info.Principal)
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, sessionInfoKey{}, info)))
	})
}

func PrincipalFromContext(ctx context.Context) (identity.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(identity.Principal)
	return p, ok
}

// SessionFromContext exposes the full authenticated session, including the
// recorded sign-in kind, to handlers that must branch on it.
func SessionFromContext(ctx context.Context) (identity.SessionInfo, bool) {
	info, ok := ctx.Value(sessionInfoKey{}).(identity.SessionInfo)
	return info, ok
}

type sessionJSON struct {
	UserID             string `json:"userId"`
	Login              string `json:"login"`
	DefaultHouseholdID string `json:"defaultHouseholdId,omitempty"`
	ExpiresAt          string `json:"expiresAt"`
	// InstanceAdmin is always emitted, including false, so clients never infer
	// the capability from a missing field.
	InstanceAdmin bool `json:"instanceAdmin"`
	// SignInMethod is always emitted so a client can tell a local session from
	// an OIDC one and offer the local password change only where it applies.
	SignInMethod string `json:"signInMethod"`
}

func sessionResponse(p identity.Principal, expires time.Time, method identity.SignInMethod) sessionJSON {
	return sessionJSON{UserID: p.UserID, Login: p.Login, DefaultHouseholdID: p.DefaultHouseholdID, ExpiresAt: expires.UTC().Format(time.RFC3339), InstanceAdmin: p.InstanceAdmin, SignInMethod: string(method)}
}

// recordCredentialFault marks a corrupt stored password hash on the request's
// access log entry while the response stays the uniform invalid_credentials.
func recordCredentialFault(r *http.Request, err error) {
	if errors.Is(err, security.ErrInvalidPasswordHash) {
		httpx.RecordFault(r, "password_hash_integrity_fault")
	}
}
