package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx/apimodel"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

// pendingCookieName mirrors the flow cookie rule: a __Host- prefixed Secure
// cookie is used only when the public URL is https.
func (h *OIDCHandler) pendingCookieName() string {
	if h.secure() {
		return "__Host-tendo_oidc_pending"
	}
	return "tendo_oidc_pending"
}

func (h *OIDCHandler) setPendingCookie(w http.ResponseWriter, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: h.pendingCookieName(), Value: token, Path: "/", HttpOnly: true, Secure: h.secure(), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func (h *OIDCHandler) clearPendingCookie(w http.ResponseWriter) { h.setPendingCookie(w, "", -1) }

// pendingToken returns the single pending cookie value. Duplicate cookies are
// treated as absent, exactly like the flow cookie.
func (h *OIDCHandler) pendingToken(r *http.Request) string {
	token := ""
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == h.pendingCookieName() {
			token = cookie.Value
			count++
		}
	}
	if count != 1 {
		return ""
	}
	return token
}

// pendingGuard applies the shared preconditions of a continuation endpoint:
// OIDC enabled, the endpoint's limiter, canonical Origin when the method is
// state-changing, and the "already signed in" conflict for a valid session
// cookie. OIDC-disabled is answered before any limiter budget is consumed.
func (h *OIDCHandler) pendingGuard(w http.ResponseWriter, r *http.Request, limiter *limiter, requireOrigin, rejectSignedIn bool) bool {
	w.Header().Set("Cache-Control", "no-store")
	if h.service == nil {
		problem(w, 404, "oidc_disabled")
		return false
	}
	if !limiter.admitAttempt(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return false
	}
	if requireOrigin && !h.originAllowed(r.Header.Get("Origin")) {
		problem(w, 403, "origin_not_allowed")
		return false
	}
	if rejectSignedIn && h.sessions != nil {
		if token, present := h.sessions.sessionToken(r); present {
			// Only a *valid* session conflicts. A stale cookie is ignored here
			// so the continuation still works; session authentication reports
			// its own state on the routes that require it.
			if _, err := h.sessions.service.Lookup(r.Context(), token); err == nil {
				problem(w, 409, "already_signed_in")
				return false
			}
		}
	}
	return true
}

func pendingSession(p identity.Principal, expires time.Time) apimodel.Session {
	session := apimodel.Session{UserId: p.UserID, Login: p.Login, ExpiresAt: expires.UTC()}
	if p.DefaultHouseholdID != "" {
		household := p.DefaultHouseholdID
		session.DefaultHouseholdId = &household
	}
	return session
}

func writeOIDCPendingStatus(w http.ResponseWriter, status identity.OIDCPendingStatus) {
	writeJSON(w, 200, apimodel.OIDCPendingStatus{
		ExpiresAt: status.ExpiresAt.UTC(), EmailVerified: status.EmailVerified, Invitation: status.Invitation,
	})
}

func (h *OIDCHandler) pending(w http.ResponseWriter, r *http.Request) {
	if !h.pendingGuard(w, r, h.pendingReadLimiter, false, true) {
		return
	}
	status, err := h.service.Pending(r.Context(), h.pendingToken(r))
	if err != nil {
		h.writeOIDCPendingError(w, r, err)
		return
	}
	writeOIDCPendingStatus(w, status)
}

func (h *OIDCHandler) createPendingAccount(w http.ResponseWriter, r *http.Request) {
	if !h.pendingGuard(w, r, h.pendingLimiter, true, true) {
		return
	}
	// The request carries no fields. An absent body or the empty object are
	// both accepted; anything else is rejected by the shared strict decoder.
	if r.ContentLength != 0 {
		if _, ok := httpx.ReadObjectAllowEmpty(w, r, map[string]httpx.Field{}, 4096); !ok {
			return
		}
	}
	result, err := h.service.CreatePendingAccount(r.Context(), h.pendingToken(r))
	if err != nil {
		h.writeOIDCPendingError(w, r, err)
		return
	}
	h.clearPendingCookie(w)
	h.sessions.setCookie(w, result.Session.Token, int(identity.SessionLifetime/time.Second))
	w.Header().Set("Location", "/api/v1/session")
	writeJSON(w, 201, apimodel.OIDCPendingResult{
		Session: pendingSession(result.Session.Principal, result.Session.ExpiresAt), Invitation: apimodel.OIDCInvitationOutcome(result.Invitation),
	})
}

type oidcPendingLinkRequest struct{ Login, Password string }

func readOIDCPendingLink(w http.ResponseWriter, r *http.Request) (oidcPendingLinkRequest, bool) {
	values, ok := httpx.ReadObject(w, r, map[string]httpx.Field{
		"login":    {Kind: httpx.KindString, Required: true},
		"password": {Kind: httpx.KindString, Required: true},
	}, 4096)
	if !ok {
		return oidcPendingLinkRequest{}, false
	}
	return oidcPendingLinkRequest{Login: stringField(values, "login"), Password: stringField(values, "password")}, true
}

func (h *OIDCHandler) linkPendingAccount(w http.ResponseWriter, r *http.Request) {
	if !h.pendingGuard(w, r, h.pendingLimiter, true, true) {
		return
	}
	request, ok := readOIDCPendingLink(w, r)
	if !ok {
		return
	}
	result, err := h.service.LinkPendingAccount(r.Context(), h.pendingToken(r), request.Login, request.Password)
	if err != nil {
		h.writeOIDCPendingError(w, r, err)
		return
	}
	h.clearPendingCookie(w)
	h.sessions.setCookie(w, result.Session.Token, int(identity.SessionLifetime/time.Second))
	w.Header().Set("Location", "/api/v1/session")
	writeJSON(w, 201, apimodel.OIDCPendingLinkResult{
		Session:    pendingSession(result.Session.Principal, result.Session.ExpiresAt),
		Invitation: apimodel.OIDCInvitationOutcome(result.Invitation), EmailSaved: result.EmailSaved,
	})
}

func (h *OIDCHandler) cancelPending(w http.ResponseWriter, r *http.Request) {
	// Cancelling is harmless and idempotent, so a signed-in browser may do it.
	if !h.pendingGuard(w, r, h.pendingLimiter, true, false) {
		return
	}
	if err := h.service.CancelPending(r.Context(), h.pendingToken(r)); err != nil {
		problem(w, 503, "unavailable")
		return
	}
	h.clearPendingCookie(w)
	w.WriteHeader(204)
}

func (h *OIDCHandler) discardAccount(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.service == nil || h.sessions == nil {
		problem(w, 404, "oidc_disabled")
		return
	}
	if !h.pendingLimiter.admitAttempt(clientIP(r)) {
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
		return
	}
	if !h.originAllowed(r.Header.Get("Origin")) {
		problem(w, 403, "origin_not_allowed")
		return
	}
	info, ok := h.sessions.authenticate(w, r)
	if !ok {
		return
	}
	handle, status, err := h.service.DiscardAccount(r.Context(), info.Principal.UserID)
	if err != nil {
		h.writeOIDCPendingError(w, r, err)
		return
	}
	h.sessions.clear(w)
	h.setPendingCookie(w, handle, int(identity.OIDCPendingLifetime/time.Second))
	writeOIDCPendingStatus(w, status)
}

func (h *OIDCHandler) writeOIDCPendingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrOIDCPendingNotFound):
		problem(w, 404, "pending_not_found")
	case errors.Is(err, identity.ErrOIDCConflict):
		// Row 6/7a conflicts consume the continuation; clear the cookie so the
		// browser starts the decision again from the provider.
		h.clearPendingCookie(w)
		problem(w, 409, "identity_conflict")
	case errors.Is(err, identity.ErrOIDCAccountNotDiscardable):
		problem(w, 409, "account_not_discardable")
	case errors.Is(err, identity.ErrOIDCPendingExhausted):
		// The row was deleted at the attempt limit, so the cookie must go too.
		h.clearPendingCookie(w)
		problem(w, 401, "invalid_credentials")
	case errors.Is(err, identity.ErrInvalidCredentials):
		recordCredentialFault(r, err)
		problem(w, 401, "invalid_credentials")
	case errors.Is(err, security.ErrPasswordWorkLimit):
		w.Header().Set("Retry-After", "60")
		problem(w, 429, "rate_limited")
	default:
		problem(w, 503, "unavailable")
	}
}
