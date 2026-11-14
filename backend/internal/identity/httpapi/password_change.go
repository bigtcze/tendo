package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/bigtcze/tendo/backend/internal/identity"
	"github.com/bigtcze/tendo/backend/internal/platform/httpx"
	"github.com/bigtcze/tendo/backend/internal/platform/security"
	"github.com/go-chi/chi/v5"
)

const passwordChangeBodyLimit = 4096

const (
	passwordChangeClientLimit = 10
	passwordChangeUserLimit   = 5
	passwordChangeUserWindow  = time.Hour
	passwordChangeUserHourCap = 20
)

type passwordChangeService interface {
	ChangePassword(context.Context, identity.ChangePasswordInput) (identity.Session, error)
}

// PasswordChangeHandler serves the self-service local password change. The
// route is cookie-authenticated but the handler additionally requires a local
// sign-in kind, so an OIDC session can never replace a local password.
type PasswordChangeHandler struct {
	service       passwordChangeService
	sessions      *SessionHandler
	clientLimit   *limiter
	userLimit     *limiter
	userHourLimit *limiter
}

func NewPasswordChange(service passwordChangeService, sessions *SessionHandler) *PasswordChangeHandler {
	return &PasswordChangeHandler{
		service:       service,
		sessions:      sessions,
		clientLimit:   newLimiter(passwordChangeClientLimit),
		userLimit:     newLimiter(passwordChangeUserLimit),
		userHourLimit: newWindowLimiter(passwordChangeUserHourCap, passwordChangeUserWindow),
	}
}

func (h *PasswordChangeHandler) Register(r chi.Router) {
	r.With(h.limitClient, h.sessions.RequireSession).Post("/api/v1/auth/password", h.change)
}

// limitClient spends the per-address budget before authentication, so an
// unauthenticated flood cannot amplify into session lookups.
func (h *PasswordChangeHandler) limitClient(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.clientLimit.admitAttempt(clientIP(r)) {
			rateLimited(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *PasswordChangeHandler) change(w http.ResponseWriter, r *http.Request) {
	info, ok := SessionFromContext(r.Context())
	if !ok {
		problem(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	// The session kind is checked before any body work: an OIDC session is
	// refused even when the account also has a local password.
	if info.SignInMethod != identity.SignInPassword {
		problem(w, http.StatusForbidden, "local_session_required")
		return
	}
	userID := info.Principal.UserID
	if !h.userLimit.admitAttempt(userID) || !h.userHourLimit.admitAttempt(userID) {
		rateLimited(w)
		return
	}
	token, ok := h.sessions.sessionToken(r)
	if !ok {
		problem(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	values, ok := httpx.ReadObject(w, r, map[string]httpx.Field{
		"currentPassword": {Kind: httpx.KindString, Required: true},
		"newPassword":     {Kind: httpx.KindString, Required: true},
	}, passwordChangeBodyLimit)
	if !ok {
		return
	}
	session, err := h.service.ChangePassword(r.Context(), identity.ChangePasswordInput{
		Principal:       info.Principal,
		SignInMethod:    info.SignInMethod,
		SessionToken:    token,
		CurrentPassword: stringField(values, "currentPassword"),
		NewPassword:     stringField(values, "newPassword"),
	})
	if err != nil {
		h.writeChangeError(w, r, err)
		return
	}
	// The replacement deleted the caller's old session, so the response must
	// hand the browser the only copy of the fresh one.
	h.sessions.setCookie(w, session.Token, int(identity.SessionLifetime/time.Second))
	slog.Info("local password changed", "user_id", userID)
	writeJSON(w, http.StatusOK, sessionResponse(session.Principal, session.ExpiresAt, session.SignInMethod))
}

func (h *PasswordChangeHandler) writeChangeError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *identity.ValidationError
	switch {
	case errors.Is(err, security.ErrPasswordWorkLimit):
		rateLimited(w)
	case errors.Is(err, identity.ErrLocalSessionRequired):
		problem(w, http.StatusForbidden, "local_session_required")
	case errors.Is(err, identity.ErrUnauthenticated):
		// The caller's own session disappeared between authentication and the
		// transaction, so the cookie is stale and the browser must sign in again.
		h.sessions.clear(w)
		problem(w, http.StatusUnauthorized, "unauthenticated")
	case errors.Is(err, identity.ErrCurrentPasswordIncorrect):
		recordCredentialFault(r, err)
		httpx.ValidationProblemResponse(w, "currentPassword", "incorrect")
	case errors.As(err, &validation):
		httpx.ValidationProblemResponse(w, validation.Field, validation.Code)
	default:
		problem(w, http.StatusServiceUnavailable, "unavailable")
	}
}
