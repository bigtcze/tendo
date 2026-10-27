package httpapi

import (
	"errors"
	"net/http"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
)

type oidcStartRequest struct {
	purpose  identity.OIDCPurpose
	password string
}

func readOIDCStartRequest(w http.ResponseWriter, r *http.Request) (oidcStartRequest, bool) {
	fields := map[string]httpx.Field{
		"purpose":         {Kind: httpx.KindString, Required: true},
		"currentPassword": {Kind: httpx.KindString},
	}
	values, ok := httpx.ReadObject(w, r, fields, 4096)
	if !ok {
		return oidcStartRequest{}, false
	}
	purpose := identity.OIDCPurpose(values["purpose"].(string))
	password, hasPassword := values["currentPassword"].(string)
	if purpose == identity.OIDCPurposeLink && !hasPassword {
		writeOIDCValidation(w, "currentPassword")
		return oidcStartRequest{}, false
	}
	invalidPurpose := purpose != identity.OIDCPurposeLogin && purpose != identity.OIDCPurposeLink
	passwordOnLogin := purpose == identity.OIDCPurposeLogin && hasPassword
	if invalidPurpose || passwordOnLogin {
		writeOIDCValidation(w, "purpose")
		return oidcStartRequest{}, false
	}
	return oidcStartRequest{purpose: purpose, password: password}, true
}
func (h *OIDCHandler) resolveStartSession(
	w http.ResponseWriter,
	r *http.Request,
	purpose identity.OIDCPurpose,
) (identity.SessionInfo, identity.Principal, bool) {
	var session identity.SessionInfo
	var principal identity.Principal
	if h.sessions != nil {
		if _, present := h.sessions.sessionToken(r); present {
			var authenticated bool
			session, authenticated = h.sessions.authenticate(w, r)
			if !authenticated {
				return identity.SessionInfo{}, identity.Principal{}, false
			}
			principal = session.Principal
		} else if purpose == identity.OIDCPurposeLink {
			problem(w, 401, "invalid_credentials")
			return identity.SessionInfo{}, identity.Principal{}, false
		}
	}
	if purpose == identity.OIDCPurposeLink && session.ID == "" {
		problem(w, 401, "invalid_credentials")
		return identity.SessionInfo{}, identity.Principal{}, false
	}
	session.Principal = principal
	return session, principal, true
}
func writeOIDCStartError(w http.ResponseWriter, err error) {
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
	default:
		problem(w, 503, "unavailable")
	}
}
