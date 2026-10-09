package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

type oidcService interface {
	Status() (bool, string)
	Linked(context.Context, string) (bool, error)
	Start(context.Context, identity.OIDCStartParams) (identity.OIDCStartResult, error)
	Callback(context.Context, url.Values, string, string) (identity.OIDCCallbackResult, error)
}

type OIDCHandler struct {
	service   oidcService
	sessions  *SessionHandler
	publicURL string
	limiter   *limiter
}

func NewOIDC(service oidcService, sessions *SessionHandler, publicURL string) *OIDCHandler {
	return &OIDCHandler{service: service, sessions: sessions, publicURL: publicURL, limiter: newLimiter(10)}
}

func (h *OIDCHandler) Register(r chi.Router) {
	r.Get("/api/v1/auth/oidc", h.status)
	r.Get("/api/v1/auth/oidc/identity", h.identity)
	r.Post("/api/v1/auth/oidc/start", h.start)
	r.Get("/api/v1/auth/oidc/callback", h.callback)
}

func (h *OIDCHandler) secure() bool { return strings.HasPrefix(h.publicURL, "https://") }
func (h *OIDCHandler) flowCookieName() string {
	if h.secure() {
		return "__Host-tendo_oidc"
	}
	return "tendo_oidc"
}
func (h *OIDCHandler) setFlowCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: h.flowCookieName(), Value: token, Path: "/", HttpOnly: true, Secure: h.secure(), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
func (h *OIDCHandler) status(w http.ResponseWriter, _ *http.Request) {
	if h.service == nil {
		writeJSON(w, 200, map[string]any{"enabled": false})
		return
	}
	_, name := h.service.Status()
	writeJSON(w, 200, map[string]any{"enabled": true, "displayName": name})
}
func (h *OIDCHandler) identity(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		problem(w, 404, "oidc_disabled")
		return
	}
	p, ok := PrincipalFromContext(r.Context())
	if !ok && h.sessions != nil {
		info, authenticated := h.sessions.authenticate(w, r)
		if !authenticated {
			return
		}
		p, ok = info.Principal, true
	}
	if !ok {
		problem(w, 401, "unauthenticated")
		return
	}
	linked, err := h.service.Linked(r.Context(), p.UserID)
	if err != nil {
		problem(w, 503, "unavailable")
		return
	}
	writeJSON(w, 200, map[string]bool{"linked": linked})
}
func (h *OIDCHandler) start(w http.ResponseWriter, r *http.Request) {
	if !h.limiter.admitAttempt(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return
	}
	fields := map[string]httpx.Field{"purpose": {Kind: httpx.KindString, Required: true}, "currentPassword": {Kind: httpx.KindString}}
	values, ok := httpx.ReadObject(w, r, fields, 4096)
	if !ok {
		return
	}
	purpose := identity.OIDCPurpose(values["purpose"].(string))
	password, hasPassword := values["currentPassword"].(string)
	if purpose == identity.OIDCPurposeLink && !hasPassword {
		writeOIDCValidation(w, "currentPassword")
		return
	}
	if purpose != identity.OIDCPurposeLogin && purpose != identity.OIDCPurposeLink || purpose == identity.OIDCPurposeLogin && hasPassword {
		writeOIDCValidation(w, "purpose")
		return
	}
	if h.service == nil {
		problem(w, 404, "oidc_disabled")
		return
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != h.publicURL {
		problem(w, 403, "origin_not_allowed")
		return
	}
	var session identity.SessionInfo
	var principal identity.Principal
	if h.sessions != nil {
		if _, present := h.sessions.sessionToken(r); present {
			var authenticated bool
			session, authenticated = h.sessions.authenticate(w, r)
			if !authenticated {
				return
			}
			principal = session.Principal
		} else if purpose == identity.OIDCPurposeLink {
			problem(w, 401, "invalid_credentials")
			return
		}
	}
	if purpose == identity.OIDCPurposeLink && session.ID == "" {
		problem(w, 401, "invalid_credentials")
		return
	}
	session.Principal = principal
	previousBrowserToken, validPreviousBrowserCookie := h.singleFlowCookie(r)
	if !validPreviousBrowserCookie {
		previousBrowserToken = ""
	}
	result, err := h.service.Start(r.Context(), identity.OIDCStartParams{Purpose: purpose, Principal: principal, Session: session, CurrentPassword: password, PreviousBrowserToken: previousBrowserToken})
	switch {
	case errors.Is(err, identity.ErrOIDCInvalidPurpose):
		writeOIDCValidation(w, "purpose")
	case errors.Is(err, identity.ErrOIDCAlreadySignedIn):
		problem(w, 409, "already_signed_in")
	case errors.Is(err, identity.ErrUnauthenticated), errors.Is(err, identity.ErrInvalidCredentials):
		problem(w, 401, "invalid_credentials")
	case errors.Is(err, security.ErrPasswordWorkLimit):
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
	case errors.Is(err, identity.ErrOIDCUnavailable):
		problem(w, 503, "oidc_unavailable")
	case err != nil:
		problem(w, 503, "unavailable")
	default:
		h.setFlowCookie(w, result.BrowserToken, 600)
		writeJSON(w, 200, map[string]string{"authorizationUrl": result.AuthorizationURL})
	}
}
func (h *OIDCHandler) singleFlowCookie(r *http.Request) (string, bool) {
	var token string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == h.flowCookieName() {
			token = cookie.Value
			count++
		}
	}
	return token, count == 1
}

func (h *OIDCHandler) callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if h.service == nil {
		h.setFlowCookie(w, "", -1)
		h.redirect(w, "/login#oidcError=unavailable")
		return
	}
	browser, validBrowserCookie := h.singleFlowCookie(r)
	if !validBrowserCookie {
		browser = ""
	}
	sessionToken := ""
	if h.sessions != nil {
		var validSessionCookie bool
		sessionToken, validSessionCookie = h.sessions.sessionToken(r)
		if !validSessionCookie {
			sessionToken = ""
		}
	}
	result, err := h.service.Callback(r.Context(), r.URL.Query(), browser, sessionToken)
	if err != nil {
		result = identity.OIDCCallbackResult{Destination: "/login#oidcError=unavailable"}
	}
	h.setFlowCookie(w, "", -1)
	if result.SessionToken != "" {
		h.sessions.setCookie(w, result.SessionToken, int(identity.SessionLifetime/time.Second))
	}
	h.redirect(w, result.Destination)
}
func (h *OIDCHandler) redirect(w http.ResponseWriter, destination string) {
	w.Header().Set("Location", destination)
	w.WriteHeader(http.StatusSeeOther)
}

func writeOIDCValidation(w http.ResponseWriter, field string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(422)
	_ = json.NewEncoder(w).Encode(oidcValidationProblem{Type: "about:blank", Title: "Validation Failed", Status: 422, Code: "invalid_value", Field: field})
}

type oidcValidationProblem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code"`
	Field  string `json:"field"`
}
