package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

type sessionService interface {
	Login(ctx context.Context, login, password string) (identity.Session, error)
	Lookup(ctx context.Context, token string) (identity.SessionInfo, error)
	Logout(ctx context.Context, token string) error
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
	r.Delete("/api/v1/session", h.delete)
}

func (h *SessionHandler) cookieName() string {
	if h.secure {
		return "__Host-tendo_session"
	}
	return "tendo_session"
}

func (h *SessionHandler) setCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: h.cookieName(), Value: token, Path: "/", HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
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
	values, ok := readStrictObject(w, r, 4096, "login", "password")
	if !ok {
		return
	}
	session, err := h.service.Login(r.Context(), values["login"], values["password"])
	if errors.Is(err, security.ErrPasswordWorkLimit) {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return
	}
	if errors.Is(err, identity.ErrInvalidCredentials) {
		problem(w, 401, "invalid_credentials")
		return
	}
	if err != nil {
		problem(w, 503, "unavailable")
		return
	}
	h.setCookie(w, session.Token, int(identity.SessionLifetime/time.Second))
	w.Header().Set("Location", "/api/v1/session")
	writeJSON(w, 201, sessionResponse(session.Principal, session.ExpiresAt))
}

func (h *SessionHandler) get(w http.ResponseWriter, r *http.Request) {
	info, ok := h.authenticate(w, r)
	if !ok {
		return
	}
	w.Header().Set("ETag", `"session-`+info.ID+`"`)
	writeJSON(w, 200, sessionResponse(info.Principal, info.ExpiresAt))
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

// RequireSession authenticates the request and exposes the principal through
// PrincipalFromContext. It is the seam later household-scoped routes use.
func (h *SessionHandler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := h.authenticate(w, r)
		if !ok {
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, info.Principal)))
	})
}

func PrincipalFromContext(ctx context.Context) (identity.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(identity.Principal)
	return p, ok
}

type sessionJSON struct {
	UserID             string `json:"userId"`
	Login              string `json:"login"`
	DefaultHouseholdID string `json:"defaultHouseholdId,omitempty"`
	ExpiresAt          string `json:"expiresAt"`
}

func sessionResponse(p identity.Principal, expires time.Time) sessionJSON {
	return sessionJSON{UserID: p.UserID, Login: p.Login, DefaultHouseholdID: p.DefaultHouseholdID, ExpiresAt: expires.UTC().Format(time.RFC3339)}
}
